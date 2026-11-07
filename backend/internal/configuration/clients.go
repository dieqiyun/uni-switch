package configuration

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/localgateway"
	"github.com/google/uuid"
)

type ExtraState struct {
	Profile         string            `json:"profile,omitempty"`
	Provider        *agent.Provider   `json:"provider,omitempty"`
	Active          *string           `json:"active"`
	Baseline        []edit.Change     `json:"baseline"`
	Revision        int64             `json:"revision"`
	Manual          bool              `json:"manual"`
	Source          *string           `json:"source"`
	WorkbuddyRoutes *edit.Change      `json:"workbuddy_routes"`
	Formats         map[string]string `json:"formats,omitempty"`
}
type ConfigPending struct {
	Formats  map[string]string `json:"formats,omitempty"`
	Client   string            `json:"client"`
	Changes  []edit.Change     `json:"changes"`
	State    *ExtraState       `json:"state"`
	Revision int64             `json:"revision"`
}
type Document struct {
	Client   string `json:"client"`
	FileID   string `json:"fileId"`
	Path     string `json:"path"`
	Format   string `json:"format"`
	Content  string `json:"content"`
	Revision string `json:"revision"`
	Exists   bool   `json:"exists"`
}

func (s *Service) extra(client string) (ExtraState, error) {
	var data string
	var st ExtraState
	if e := s.db.QueryRow("SELECT state FROM client_configs WHERE id=?", client).Scan(&data); e != nil {
		return st, e
	}
	e := parseExtra(data, &st)
	return st, e
}
func parseExtra(data string, st *ExtraState) error { return parse(data, st) }
func (s *Service) binding(client string) (string, bool, error) {
	if primary(client) {
		t, e := s.target(client)
		return t.Directory, false, e
	}
	var dir string
	var selected bool
	e := s.db.QueryRow("SELECT directory,selected FROM client_configs WHERE id=?", client).Scan(&dir, &selected)
	if e == nil && dir == "" && !selected {
		dir, e = agent.DefaultDirectory(client)
	}
	return dir, !selected, e
}
func (s *Service) specs(client string) ([]agent.Spec, error) {
	if !primary(client) {
		st, e := s.extra(client)
		if e != nil {
			return nil, e
		}
		if st.Baseline != nil {
			specs := []agent.Spec{}
			for _, c := range st.Baseline {
				format := st.Formats[c.Path]
				if format == "" {
					format = agent.FileFormat(c.Path)
				}
				if client == "dsh" {
					format = "yaml"
				}
				specs = append(specs, agent.Spec{Path: c.Path, Format: format})
			}
			return specs, nil
		}
	}
	dir, binding, e := s.binding(client)
	if e != nil {
		return nil, e
	}
	if !primary(client) {
		st, e := s.extra(client)
		if e != nil {
			return nil, e
		}
		return agent.ProfileSpecifications(client, dir, st.Profile, binding)
	}
	return agent.Specifications(client, dir, binding, true)
}
func (s *Service) file(client, id string) (agent.Spec, error) {
	specs, e := s.specs(client)
	if e != nil {
		return agent.Spec{}, e
	}
	for _, f := range specs {
		if agent.Revision(f.Path, nil) == id {
			return f, nil
		}
	}
	return agent.Spec{}, fail("config_file_changed", "文件不在当前客户端配置列表中，请重新打开编辑器")
}
func (s *Service) snapshot(client string) (string, error) {
	specs, e := s.specs(client)
	if e != nil {
		return "", e
	}
	revs := []string{}
	for _, f := range specs {
		if !externalFormat(f.Format) {
			if e = agent.CheckPath(f.Path); e != nil {
				return "", e
			}
		}
		raw, e := readClientResource(f.Path, f.Format)
		if e != nil {
			return "", e
		}
		revs = append(revs, agent.Revision(f.Path, raw))
	}
	dir, _, e := s.binding(client)
	if e != nil {
		return "", e
	}
	return agent.Revision(dir, ptr(strings.Join(revs, "\n"))), nil
}
func (s *Service) ReadDocument(client, id string) (Document, error) {
	f, e := s.file(client, id)
	if e != nil {
		return Document{}, e
	}
	if externalFormat(f.Format) {
		return Document{}, fail("external_resource", "此客户端通过数据库、API 或系统凭据库配置，请使用一键配置和恢复")
	}
	if e = agent.CheckPath(f.Path); e != nil {
		return Document{}, e
	}
	raw, e := edit.Read(f.Path)
	if e != nil {
		return Document{}, e
	}
	content := ""
	if f.Format == "json" {
		content = "{}\n"
	}
	if raw != nil {
		content = *raw
	}
	if len(content) > 4<<20 {
		return Document{}, fail("config_too_large", "配置文件超过 4 MiB，无法在应用内编辑")
	}
	return Document{client, id, f.Path, f.Format, content, agent.Revision(f.Path, raw), raw != nil}, nil
}
func writeResult(changed bool, backup *string, revision int64) Object {
	return Object{"changed": changed, "backupPath": backup, "configurationRevision": revision}
}
func (s *Service) configRevision(client string) (int64, error) {
	if primary(client) {
		t, e := s.target(client)
		return t.Revision, e
	}
	st, e := s.extra(client)
	return st.Revision, e
}
func (s *Service) Save(doc Document) (Object, error) {
	if !valid(doc.Client) {
		return nil, fail("invalid_client", "未知客户端")
	}
	if e := s.Recover(); e != nil {
		return nil, e
	}
	f, e := s.file(doc.Client, doc.FileID)
	if e != nil {
		return nil, e
	}
	if e = agent.CheckPath(f.Path); e != nil {
		return nil, e
	}
	before, e := edit.Read(f.Path)
	if e != nil {
		return nil, e
	}
	if doc.Path != f.Path || doc.Revision != agent.Revision(f.Path, before) {
		return nil, fail("config_conflict", "配置文件或目录在编辑期间发生变化。本次未保存，请保留编辑内容并重新读取文件")
	}
	if e = agent.Validate(f.Format, doc.Content); e != nil {
		return nil, e
	}
	if agent.Same(before, &doc.Content) {
		rev, e := s.configRevision(doc.Client)
		return writeResult(false, nil, rev), e
	}
	var st *ExtraState
	if !primary(doc.Client) {
		v, e := s.extra(doc.Client)
		if e != nil {
			return nil, e
		}
		if doc.Client == "workbuddy" && v.WorkbuddyRoutes == nil && !v.Manual && len(v.Baseline) > 0 {
			c := v.Baseline[0]
			v.WorkbuddyRoutes = &c
		}
		v.Manual = v.Baseline != nil
		for i, c := range v.Baseline {
			if c.Path == f.Path {
				v.Baseline[i].After = &doc.Content
			}
		}
		st = &v
	}
	return s.ExecuteConfig(ConfigPending{Client: doc.Client, Changes: []edit.Change{{Path: f.Path, Before: before, After: &doc.Content}}, State: st})
}
func (s *Service) ApplyClient(client, id, protocol string, expected *string) (Object, error) {
	if primary(client) {
		return nil, fail("invalid_client", "请使用原有客户端供应商入口")
	}
	if e := s.Recover(); e != nil {
		return nil, e
	}
	st, e := s.extra(client)
	if e != nil {
		return nil, e
	}
	if expected != nil {
		rev, e := s.snapshot(client)
		if e != nil {
			return nil, e
		}
		if *expected != rev {
			return nil, fail("config_conflict", "配置在确认期间发生变化，请重新查看后确认覆盖")
		}
	} else {
		if st.Manual {
			return nil, fail("config_conflict", "配置已手动或外部修改，请查看文件后确认覆盖")
		}
		for _, c := range st.Baseline {
			raw, e := readClientResource(c.Path, st.Formats[c.Path])
			if e != nil {
				return nil, e
			}
			if !sameClientResource(raw, c.After, st.Formats[c.Path]) {
				return nil, fail("config_conflict", "配置已手动或外部修改，请查看文件后确认覆盖")
			}
		}
	}
	p, e := s.provider(id)
	if e != nil {
		return nil, e
	}
	models, e := p.Models(s.Cache, true)
	if e != nil {
		return nil, e
	}
	files, e := s.specs(client)
	if e != nil {
		return nil, e
	}
	input := agent.Input{Client: client, Name: str(p.Summary["name"]), Model: p.Model(), Models: models, Protocol: protocol, Upstream: p.Upstream(), BaseURL: str(p.Summary["baseUrl"]), APIKey: p.APIKey, AuthMode: str(p.Summary["authMode"]), Previous: st.WorkbuddyRoutes}
	if client == "kirocrew" {
		for _, h := range []string{"opencode", "pi", "goose"} {
			st, e := s.extra(h)
			if e == nil && st.Active != nil && *st.Active == id {
				input.Harness = h
				break
			}
		}
		if input.Harness == "" {
			for _, h := range []string{"codex", "claude_cli"} {
				t, e := s.target(h)
				if e == nil && t.Active != nil && *t.Active == id {
					input.Harness = h
					if h == "claude_cli" {
						input.Harness = "claude"
					}
					break
				}
			}
		}
	}
	if client != "zcode" && client != "dsh" && client != "workbuddy" {
		if s.Route == nil || s.Route.Port == 0 {
			return nil, fail("bridge_unavailable", "本地 Go 网关尚未启动")
		}
		input.Routed = true
		input.APIKey = s.Route.Token
		input.BaseURL = fmt.Sprintf("http://127.0.0.1:%d/clients/%s/%s", s.Route.Port, client, p.ID())
		if localgateway.NeedsScoped(client) {
			route, e := localgateway.StartScoped(s.Directory, client)
			if e != nil {
				return nil, e
			}
			input.BaseURL = fmt.Sprintf("http://127.0.0.1:%d", route.Port)
			if client == "zed" {
				input.APIKey = route.Token
			}
		}
		if protocol != "messages" && protocol != "gemini" {
			input.BaseURL += "/v1"
		}
		input.AuthMode = "bearer"
		st.Provider = &p
	}
	if client == "zed" {
		found := false
		for _, f := range files {
			found = found || f.Format == "zed_credential"
		}
		if !found {
			files = append(files, agent.Spec{Path: input.BaseURL, Format: "zed_credential"})
		}
	}
	if input.Previous == nil && !st.Manual && len(st.Baseline) > 0 {
		c := st.Baseline[0]
		input.Previous = &c
	}
	for _, f := range files {
		if !externalFormat(f.Format) {
			if e = agent.CheckPath(f.Path); e != nil {
				return nil, e
			}
		}
		raw, e := readClientResource(f.Path, f.Format)
		if e != nil {
			return nil, e
		}
		input.Files = append(input.Files, agent.File{Path: f.Path, Format: f.Format, Content: raw})
	}
	plan, e := agent.Build(input)
	if e != nil {
		return nil, e
	}
	baseline := []edit.Change{}
	for _, c := range plan.Changes {
		original := c.Before
		for _, old := range st.Baseline {
			if c.Path == old.Path {
				original = old.Before
			}
		}
		baseline = append(baseline, edit.Change{Path: c.Path, Before: original, After: c.After})
	}
	st.Active = ptr(id)
	st.Source = ptr(p.Signature())
	st.Baseline = baseline
	st.Formats = map[string]string{}
	for _, f := range files {
		st.Formats[f.Path] = f.Format
	}
	st.Manual = false
	st.WorkbuddyRoutes = plan.WorkbuddyRoutes
	result, e := s.ExecuteConfig(ConfigPending{Client: client, Changes: plan.Changes, State: &st, Formats: st.Formats})
	if e == nil && plan.ImportLink != "" {
		result["importLink"] = plan.ImportLink
	}
	return result, e
}
func (s *Service) RestoreClient(client string) (Object, error) {
	if e := s.Recover(); e != nil {
		return nil, e
	}
	if primary(client) {
		before, e := s.configRevision(client)
		if e != nil {
			return nil, e
		}
		if e = s.Restore(client, nil); e != nil {
			return nil, e
		}
		after, e := s.configRevision(client)
		return writeResult(before != after, nil, after), e
	}
	st, e := s.extra(client)
	if e != nil {
		return nil, e
	}
	if c, ok := agent.LookupClient(client); ok && c.Mode == "import" {
		return s.ExecuteConfig(ConfigPending{Client: client, State: &ExtraState{}})
	}
	if st.Baseline == nil {
		return nil, fail("nothing_to_restore", "此客户端尚未接管")
	}
	changes := []edit.Change{}
	for _, c := range st.Baseline {
		raw, e := readClientResource(c.Path, st.Formats[c.Path])
		if e != nil {
			return nil, e
		}
		if !sameClientResource(raw, c.After, st.Formats[c.Path]) {
			return nil, fail("config_conflict", "文件已被手动或外部修改，自动恢复已停止；请使用编辑器和操作备份手动保留或恢复内容")
		}
		after := c.Before
		if st.Formats[c.Path] == "hanako_chat" {
			after, e = hanakoRestoreChat(c.Path, after)
			if e != nil {
				return nil, e
			}
		}
		changes = append(changes, edit.Change{Path: c.Path, Before: raw, After: after})
	}
	// A running OpenHanako validates its current model when a provider is
	// removed. Move the agent back to its own model before removing ours.
	if client == "hanako" {
		for i, j := 0, len(changes)-1; i < j; i, j = i+1, j-1 {
			changes[i], changes[j] = changes[j], changes[i]
		}
	}
	return s.ExecuteConfig(ConfigPending{Client: client, Changes: changes, State: &ExtraState{Profile: st.Profile}, Formats: st.Formats})
}
func (s *Service) ExecuteConfig(p ConfigPending) (Object, error) {
	changes := []edit.Change{}
	for _, c := range p.Changes {
		if !sameClientResource(c.Before, c.After, p.Formats[c.Path]) {
			changes = append(changes, c)
		}
	}
	p.Changes = changes
	previous, e := s.configRevision(p.Client)
	if e != nil {
		return nil, e
	}
	if len(changes) == 0 {
		if p.State != nil {
			p.State.Revision = previous
			if _, e = s.db.Exec("UPDATE client_configs SET state=? WHERE id=?", encode(p.State), p.Client); e != nil {
				return nil, e
			}
		}
		return writeResult(false, nil, previous), nil
	}
	for _, c := range changes {
		raw, e := readClientResource(c.Path, p.Formats[c.Path])
		if e != nil {
			return nil, e
		}
		if !sameClientResource(raw, c.Before, p.Formats[c.Path]) {
			return nil, fail("config_conflict", "写入前发现外部修改，本次未保存")
		}
	}
	p.Revision = revision(previous)
	if p.State != nil {
		p.State.Revision = p.Revision
	}
	id := uuid.NewString()
	backups := filepath.Join(s.Directory, "backups")
	if e = edit.PrivateDirectory(backups); e != nil {
		return nil, e
	}
	backup := filepath.Join(backups, "client-config-"+id+".json")
	if e = edit.WriteAtomic(backup, ptr(encode(p))); e != nil {
		return nil, e
	}
	if _, e = s.db.Exec("INSERT INTO config_operations(id,status,data) VALUES(?,'pending',?)", id, encode(p)); e != nil {
		return nil, e
	}
	e = applyClientChanges(changes, p.Formats)
	if e == nil {
		e = s.finishConfig(id, p)
	}
	if e != nil {
		if back := rollbackClientChanges(changes, p.Formats); back != nil {
			return nil, back
		}
		_, err := s.db.Exec("UPDATE config_operations SET status='rolled_back',data='{}' WHERE id=?", id)
		if err != nil {
			return nil, err
		}
		return nil, e
	}
	return writeResult(true, &backup, p.Revision), nil
}
func (s *Service) finishConfig(id string, p ConfigPending) error {
	tx, e := s.db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if primary(p.Client) {
		_, e = tx.Exec("UPDATE targets SET applied_at_ms=?,configuration_manual=1 WHERE id=?", p.Revision, p.Client)
	} else if p.State != nil {
		_, e = tx.Exec("UPDATE client_configs SET state=? WHERE id=?", encode(p.State), p.Client)
	}
	if e != nil {
		return e
	}
	if _, e = tx.Exec("UPDATE config_operations SET status='complete',data='{}' WHERE id=?", id); e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Service) recoverConfig() error {
	rows, e := s.db.Query("SELECT id,data FROM config_operations WHERE status='pending' ORDER BY rowid")
	if e != nil {
		return e
	}
	pending := [][2]string{}
	for rows.Next() {
		var r [2]string
		if e = rows.Scan(&r[0], &r[1]); e != nil {
			rows.Close()
			return e
		}
		pending = append(pending, r)
	}
	rows.Close()
	for _, r := range pending {
		var p ConfigPending
		if e = parse(r[1], &p); e != nil {
			return e
		}
		all := true
		for _, c := range p.Changes {
			raw, e := readClientResource(c.Path, p.Formats[c.Path])
			all = all && e == nil && sameClientResource(raw, c.After, p.Formats[c.Path])
		}
		if all {
			e = s.finishConfig(r[0], p)
		} else {
			e = rollbackClientChanges(p.Changes, p.Formats)
			if e == nil {
				_, e = s.db.Exec("UPDATE config_operations SET status='rolled_back',data='{}' WHERE id=?", r[0])
			}
		}
		if e != nil {
			return e
		}
	}
	return nil
}
func (s *Service) ClientStatus(client string) (Object, error) {
	dir, _, e := s.binding(client)
	if e != nil {
		return nil, e
	}
	specs, e := s.specs(client)
	if e != nil {
		return nil, e
	}
	files := []any{}
	for _, f := range specs {
		fi, e := os.Stat(f.Path)
		files = append(files, Object{"id": agent.Revision(f.Path, nil), "path": f.Path, "format": f.Format, "exists": e == nil && fi.Mode().IsRegular()})
	}
	var active *string
	restore := false
	state, message := "unmanaged", "选择已保存的供应商，写入此客户端的原生配置。"
	if primary(client) {
		status, e := s.Status(client)
		if e != nil {
			return nil, e
		}
		active, _ = status["activeProviderId"].(*string)
		restore, _ = status["canRestore"].(bool)
		state = str(status["state"])
		message = str(status["message"])
	} else {
		st, e := s.extra(client)
		if e != nil {
			return nil, e
		}
		active = st.Active
		restore = st.Baseline != nil
		changed := false
		for _, c := range st.Baseline {
			raw, e := readClientResource(c.Path, st.Formats[c.Path])
			changed = changed || e != nil || !sameClientResource(raw, c.After, st.Formats[c.Path])
		}
		if st.Manual {
			state = "manual_changes"
			message = "配置已手动保存。重新应用供应商前需确认覆盖。"
		} else if changed {
			state = "external_change"
			message = "配置被外部修改，请查看文件后确认是否重新应用。"
		} else if active != nil {
			p, e := s.provider(*active)
			if e != nil || st.Source == nil || p.Signature() != *st.Source {
				state = "saved_changes"
				message = "供应商已编辑，请重新一键配置以更新此客户端。"
			} else {
				state = "applied"
				message = "配置已写入，请重启客户端并新开对话。"
			}
		}
	}
	if c, ok := agent.LookupClient(client); ok && active != nil && state == "applied" {
		if c.Mode == "import" {
			state = "awaiting_import"
			message = "导入链接已生成。请在客户端确认导入；此处无法确认客户端是否已导入。"
			restore = true
		}
		if c.Mode == "launch" || client == "openchamber" {
			state = "launch_ready"
			message = "启动脚本已生成，请运行 uni-switch-launch 脚本启动客户端后使用。"
		}
	}
	rev, e := s.snapshot(client)
	if e != nil {
		return nil, e
	}
	profile := ""
	if !primary(client) {
		st, _ := s.extra(client)
		profile = st.Profile
	}
	return Object{"client": client, "directory": dir, "files": files, "activeProviderId": active, "canRestore": restore, "state": state, "message": message, "revision": rev, "profiles": agent.Profiles(client, dir), "profile": profile}, nil
}

func (s *Service) SetProfile(client, profile string) error {
	if primary(client) {
		return fail("invalid_client", "此入口没有配置档案选择")
	}
	if e := s.Recover(); e != nil {
		return e
	}
	st, e := s.extra(client)
	if e != nil {
		return e
	}
	if st.Baseline != nil || st.Active != nil {
		return fail("managed_directory", "请先恢复当前配置，再切换档案")
	}
	dir, _, e := s.binding(client)
	if e != nil {
		return e
	}
	found := profile == ""
	for _, p := range agent.Profiles(client, dir) {
		found = found || p.ID == profile
	}
	if !found {
		return fail("profile_missing", "此配置档案已移除，请刷新列表")
	}
	st.Profile = profile
	st.Revision = revision(st.Revision)
	_, e = s.db.Exec("UPDATE client_configs SET state=? WHERE id=?", encode(st), client)
	return e
}
