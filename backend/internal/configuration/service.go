// Package configuration owns client application, confirmation and recovery.
// It follows Magpie's agent.Applied/edit transaction lifecycle while retaining
// uni-switch's SQLite and journal formats for installed users.
package configuration

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/agent"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
	"github.com/dieqiyun/uni-switch/backend/internal/state"
)

type Object = map[string]any
type Service struct {
	db               *sql.DB
	Directory, Cache string
	Route            *Route
}
type Route struct {
	Port  uint16 `json:"port"`
	Token string `json:"token"`
}
type Input struct {
	Profile        string          `json:"profile,omitempty"`
	Directory      string          `json:"directory"`
	Cache          string          `json:"cacheDirectory"`
	Target         string          `json:"target"`
	Client         string          `json:"client"`
	ProviderID     string          `json:"providerId"`
	Provider       *agent.Provider `json:"provider"`
	ProviderUpdate *agent.Provider `json:"providerUpdate"`
	Route          *Route          `json:"route"`
	Preserve       bool            `json:"preserve"`
	Source         *string         `json:"source"`
	Token          string          `json:"token"`
	Pending        *Pending        `json:"pending"`
	ConfigPending  *ConfigPending  `json:"configPending"`
	FileID         string          `json:"fileId"`
	Document       *Document       `json:"document"`
	Protocol       string          `json:"protocol"`
	Revision       *string         `json:"revision"`
	Path           string          `json:"path"`
	Expected       string          `json:"expected"`
	Automatic      bool            `json:"automatic"`
	Enabled        bool            `json:"enabled"`
}

func fail(code, msg string) error { return &edit.Error{Code: code, Message: msg} }
func obj(v any) Object            { m, _ := v.(map[string]any); return m }
func arr(v any) []any             { a, _ := v.([]any); return a }
func str(v any) string            { s, _ := v.(string); return s }
func int64of(v any) int64 {
	switch n := v.(type) {
	case json.Number:
		i, _ := n.Int64()
		return i
	case float64:
		return int64(n)
	case int64:
		return n
	case int:
		return int64(n)
	}
	return 0
}
func ptr(s string) *string { return &s }
func encode(v any) string  { return agent.Canonical(v) }
func parse(s string, v any) error {
	d := json.NewDecoder(strings.NewReader(s))
	d.UseNumber()
	if d.Decode(v) != nil {
		return fail("invalid_state", "本地配置状态损坏，请保留数据目录并修复")
	}
	return nil
}
func primary(c string) bool { return c == "codex" || c == "claude_cli" || c == "claude_desktop" }
func valid(c string) bool   { _, ok := agent.LookupClient(c); return primary(c) || ok }
func Open(in Input) (*Service, error) {
	if !filepath.IsAbs(in.Directory) {
		return nil, fail("invalid_path", "数据目录必须是绝对路径")
	}
	db, e := state.Open(filepath.Join(in.Directory, "uni-switch.db"))
	if e != nil {
		return nil, e
	}
	cache := in.Cache
	if cache == "" {
		cache = filepath.Join(in.Directory, "model-metadata")
	}
	return &Service{db, in.Directory, cache, in.Route}, nil
}

type Target struct {
	Directory string
	Active    *string
	Baseline  []agent.ManagedFile
	Revision  int64
	Summary   *string
	Source    *string
	Manual    bool
	Selected  bool
}

func (s *Service) target(id string) (Target, error) {
	var t Target
	var baseline *string
	e := s.db.QueryRow("SELECT directory,active,baseline,applied_at_ms,applied_summary,accepted_source,configuration_manual,directory_selected FROM targets WHERE id=?", id).Scan(&t.Directory, &t.Active, &baseline, &t.Revision, &t.Summary, &t.Source, &t.Manual, &t.Selected)
	if e != nil {
		return t, fail("invalid_state", "客户端状态不存在")
	}
	if baseline != nil {
		if e = parse(*baseline, &t.Baseline); e != nil {
			return t, e
		}
		if t.Baseline == nil {
			t.Baseline = []agent.ManagedFile{}
		}
	}
	return t, nil
}
func (s *Service) provider(id string) (agent.Provider, error) {
	var data string
	var p agent.Provider
	if s.db.QueryRow("SELECT data FROM providers WHERE id=?", id).Scan(&data) != nil {
		return p, fail("not_found", "供应商不存在")
	}
	if e := parse(data, &p); e != nil {
		return p, e
	}
	return p, nil
}
func BridgePath(dir, target string) string {
	name := "bridge-active.json"
	if target == "claude_cli" {
		name = "bridge-claude-cli-active.json"
	}
	if target == "claude_desktop" {
		name = "bridge-claude-desktop-active.json"
	}
	return filepath.Join(dir, name)
}
func (s *Service) bridge(target string) (*agent.Provider, error) {
	raw, e := edit.Read(BridgePath(s.Directory, target))
	if e != nil || raw == nil {
		return nil, e
	}
	var p agent.Provider
	e = parse(*raw, &p)
	return &p, e
}
func (s *Service) Initialize() error {
	_, e := s.db.Exec("CREATE TABLE IF NOT EXISTS client_configs (id TEXT PRIMARY KEY,directory TEXT NOT NULL,selected INTEGER NOT NULL DEFAULT 0,state TEXT NOT NULL DEFAULT '{}'); CREATE TABLE IF NOT EXISTS config_operations (id TEXT PRIMARY KEY,status TEXT NOT NULL,data TEXT NOT NULL);")
	if e != nil {
		return e
	}
	rows, e := s.db.Query("PRAGMA table_info(targets)")
	if e != nil {
		return e
	}
	has := false
	for rows.Next() {
		var cid, notnull, pk int
		var name, typ string
		var def any
		if rows.Scan(&cid, &name, &typ, &notnull, &def, &pk) != nil {
			rows.Close()
			return fail("invalid_state", "客户端表结构无效")
		}
		has = has || name == "configuration_manual"
	}
	rows.Close()
	if !has {
		if _, e = s.db.Exec("ALTER TABLE targets ADD COLUMN configuration_manual INTEGER NOT NULL DEFAULT 0"); e != nil {
			return e
		}
	}
	for _, client := range agent.ExtraClients {
		c := client.ID
		dir, e := agent.DefaultDirectory(c)
		if e != nil {
			// An unavailable client's discovery file must not prevent every
			// other client from starting. Resolve again when this client is
			// opened, or let the user select its directory explicitly.
			dir = ""
		}
		if _, e = s.db.Exec("INSERT OR IGNORE INTO client_configs(id,directory) VALUES(?,?)", c, dir); e != nil {
			return e
		}
	}
	return s.Recover()
}
func Dispatch(ctx context.Context, method string, data []byte) (any, error) {
	var in Input
	d := json.NewDecoder(bytes.NewReader(data))
	d.UseNumber()
	d.DisallowUnknownFields()
	if d.Decode(&in) != nil {
		return nil, fail("core_request", "配置服务参数无效")
	}
	s, e := Open(in)
	if e != nil {
		return nil, e
	}
	defer s.db.Close()
	if in.Target != "" && !primary(in.Target) || in.Client != "" && !valid(in.Client) {
		return nil, fail("invalid_client", "未知客户端")
	}
	switch method {
	case "configuration.initialize":
		e = s.Initialize()
	case "configuration.recover":
		e = s.Recover()
	case "configuration.execute":
		if in.Pending == nil {
			return nil, fail("core_request", "缺少事务")
		}
		e = s.Execute(*in.Pending)
	case "configuration.executeConfig":
		if in.ConfigPending == nil {
			return nil, fail("core_request", "缺少事务")
		}
		return s.ExecuteConfig(*in.ConfigPending)
	case "configuration.plan":
		if in.Provider == nil {
			return nil, fail("core_request", "缺少供应商")
		}
		t, e := s.target(in.Target)
		if e != nil {
			return nil, e
		}
		return s.Plan(in.Target, t, *in.Provider)
	case "configuration.apply", "configuration.applyStored":
		if e = s.Recover(); e != nil {
			return nil, e
		}
		p := in.Provider
		if p == nil {
			v, e := s.provider(in.ProviderID)
			if e != nil {
				return nil, e
			}
			p = &v
		}
		e = s.Apply(in.Target, *p, in.ProviderUpdate, in.Preserve, in.Source, nil)
	case "configuration.prepare":
		return s.Prepare(in.Target, in.ProviderID)
	case "configuration.confirm":
		e = s.Confirm(in.Target, in.ProviderID, in.Token)
	case "configuration.restore":
		e = s.Restore(in.Target, in.ProviderUpdate)
	case "configuration.status":
		return s.Status(in.Target)
	case "configuration.fast":
		return s.Fast(in.ProviderID, in.Enabled)
	case "configuration.repair":
		return s.Repair(in.ProviderID)
	case "configuration.sync":
		e = s.Sync(in.Target, in.ProviderID)
	case "configuration.migrate":
		return s.Migrate()
	case "configuration.read":
		return s.ReadDocument(in.Client, in.FileID)
	case "configuration.save":
		if in.Document == nil {
			return nil, fail("core_request", "缺少文档")
		}
		return s.Save(*in.Document)
	case "configuration.applyClient":
		return s.ApplyClient(in.Client, in.ProviderID, in.Protocol, in.Revision)
	case "configuration.restoreClient":
		return s.RestoreClient(in.Client)
	case "configuration.clientStatus":
		return s.ClientStatus(in.Client)
	case "configuration.specifications":
		return s.specs(in.Client)
	case "configuration.extraState":
		return s.extra(in.Client)
	case "configuration.bridgeRecord":
		return s.bridge(in.Target)
	case "configuration.bridgePath":
		return BridgePath(s.Directory, in.Target), nil
	case "configuration.setDirectory":
		e = s.SetDirectory(in.Client, in.Path, in.Expected, in.Automatic)
	case "configuration.setProfile":
		e = s.SetProfile(in.Client, in.Profile)
	default:
		return nil, fail("core_method", "未知配置操作")
	}
	if e != nil {
		return nil, e
	}
	return true, nil
}
func revision(previous int64) int64 {
	n := time.Now().UnixMilli()
	if n <= previous {
		n = previous + 1
	}
	return n
}
func (s *Service) SetDirectory(client, path, expected string, automatic bool) error {
	if e := s.Recover(); e != nil {
		return e
	}
	path = strings.TrimSpace(path)
	if !filepath.IsAbs(path) {
		return fail("invalid_path", "请选择完整的绝对目录路径")
	}
	if fi, e := os.Stat(path); e == nil && !fi.IsDir() {
		return fail("invalid_path", "请输入目录路径，不是文件路径")
	}
	current, _, e := s.binding(client)
	if e != nil && !primary(client) && !automatic && expected == "" {
		// Manual selection is also the recovery path for a damaged external
		// location record. Baseline checks below still prevent moving a
		// managed configuration.
		e = s.db.QueryRow("SELECT directory FROM client_configs WHERE id=?", client).Scan(&current)
	}
	if e != nil {
		return e
	}
	same := func(a, b string) bool {
		a, b = filepath.Clean(a), filepath.Clean(b)
		return a == b || strings.EqualFold(a, b) && filepath.VolumeName(a) != ""
	}
	if expected != "" && !same(current, expected) {
		return fail("directory_changed", "当前目录已变化，请重新查找")
	}
	if primary(client) {
		t, e := s.target(client)
		if e != nil {
			return e
		}
		if automatic && (t.Selected || t.Baseline != nil) {
			return fail("directory_selected", "当前目录已手动选择或接管，不自动更换")
		}
		selected := 1
		if automatic {
			selected = 0
		}
		if same(current, path) {
			_, e = s.db.Exec("UPDATE targets SET directory_selected=? WHERE id=?", selected, client)
			return e
		}
		if t.Baseline != nil {
			return fail("managed_directory", "请先恢复当前目录的原配置，再修改目录")
		}
		_, e = s.db.Exec("UPDATE targets SET directory=?,active=NULL,directory_selected=?,applied_at_ms=0,applied_summary=NULL,accepted_source=NULL WHERE id=?", path, selected, client)
		return e
	}
	st, e := s.extra(client)
	if e != nil {
		return e
	}
	if !same(current, path) && st.Baseline != nil {
		return fail("managed_directory", "请先恢复原配置，再更换目录")
	}
	if !same(current, path) {
		st.Profile = ""
	}
	_, e = s.db.Exec("UPDATE client_configs SET directory=?,selected=1,state=? WHERE id=?", path, encode(st), client)
	return e
}
