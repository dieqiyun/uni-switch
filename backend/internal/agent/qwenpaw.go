package agent

// QwenPaw (agentscope-ai/QwenPaw, formerly CoPaw) is a personal assistant
// app with agents of its own. Everything is read from its working folder
// (constant.py): QWENPAW_WORKING_DIR, else ~/.copaw when a legacy one is
// there, else ~/.qwenpaw. Its secret folder is QWENPAW_SECRET_DIR, which
// <working folder>/.env may set too (loaded after the working folder is
// known), else "<working folder>.secret" (~/.qwenpaw.secret).
//
// <working folder>/config.json lists the agents, agents.profiles
// {id: {id, workspace_dir, enabled}} in agents.agent_order. "default" is
// always there, in <working folder>/workspaces/default. Each agent's
// <workspace_dir>/agent.json names its model:
//
//	"active_model": {"provider_id": "…", "model": "…"}
//
// beside fallback_models and subagent_model, which uni-switch leaves alone,
// and thinking_level, which is the agent's effort field. Only an agent
// whose backend is "qwenpaw" (the default) runs that model
// (app/routers/agents.py); one on an ACP backend runs another program.
// QwenPaw reads agent.json again when it changes
// (load_agent_config's fingerprint), and asks for the model on every turn
// (agents/model_factory.py create_model_and_formatter), so a model picked
// there is used from the next message.
//
// Providers are files under <secret folder>/providers/: builtin/<id>.json,
// custom/<id>.json, and active_model.json, the model of an agent with no
// active_model (provider_manager_persistence.py). A custom provider is the
// Provider model as QwenPaw saves it from its console's "Add provider":
// id, name, base_url, api_key, chat_model ("OpenAIChatModel" is OpenAI's
// Chat Completions), is_custom, and the models the user added in
// extra_models. uni-switch writes custom/uni-switch.json, OpenAI Chat at the
// gateway's /v1 with a key naming QwenPaw and one model of extra_models
// for each of uni-switch's, and sets an agent's active_model to
// {"provider_id": "uni-switch", "model": "<provider>/<model>"}. QwenPaw keeps
// api_key encrypted (security/secret_store.py, "ENC:…") and takes one
// written in plain text, encrypting it at its next start
// (_maybe_migrate_plaintext): that is how its own files hold a key, so the
// key goes nowhere QwenPaw doesn't keep it. What the user changes in
// uni-switch's provider in QwenPaw's console (a model's window, its generation
// settings, the provider's headers) stays.
//
// QwenPaw reads the provider files once, at start (ProviderManager), so the
// first connect needs QwenPaw restarted; until then a turn on uni-switch's
// model fails with "Provider 'uni-switch' not found".
//
// Each agent is a row: "qwenpaw" is the default agent, any other
// qwenpaw#<id>, as omp's named profiles are.

import (
	"bufio"
	"bytes"
	"cmp"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"sort"
	"strings"

	"github.com/tidwall/gjson"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// qwenpawDefault is the agent QwenPaw always has, which can't be removed.
const qwenpawDefault = "default"

// qwenpawEncrypted is what QwenPaw's encrypted values start with.
const qwenpawEncrypted = "ENC:"

// qwenpawWorking is QwenPaw's working folder: QWENPAW_WORKING_DIR, else a
// legacy ~/.copaw, else ~/.qwenpaw. QwenPaw resolves it (Path.resolve), so
// a link is followed: the secret folder is named after where it leads.
func qwenpawWorking(at place) string {
	d := strings.TrimSpace(at.getenv("QWENPAW_WORKING_DIR"))
	switch {
	case d == "~":
		d = at.home
	case strings.HasPrefix(d, "~/"), strings.HasPrefix(d, `~\`):
		d = filepath.Join(at.home, d[2:])
	case d == "":
		d = at.join(".qwenpaw")
		if legacy := at.join(".copaw"); at.exists(legacy) {
			d = legacy
		}
	}
	if r, err := filepath.EvalSymlinks(d); err == nil {
		return r
	}
	return d
}

// qwenpawSecret is QwenPaw's secret folder: QWENPAW_SECRET_DIR, from the
// environment or from <working folder>/.env, else "<working folder>.secret".
func qwenpawSecret(at place, work string) string {
	d := strings.TrimSpace(at.getenv("QWENPAW_SECRET_DIR"))
	if d == "" {
		d = dotenvValue(filepath.Join(work, ".env"), "QWENPAW_SECRET_DIR")
	}
	switch {
	case d == "":
		return work + ".secret"
	case d == "~":
		return at.home
	case strings.HasPrefix(d, "~/"), strings.HasPrefix(d, `~\`):
		return filepath.Join(at.home, d[2:])
	}
	return d
}

// dotenvValue is k's value in a .env file as python-dotenv reads one:
// KEY=value lines, an optional "export ", quotes around the value, # for a
// comment. "" when it isn't there.
func dotenvValue(path, k string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	sc := bufio.NewScanner(bytes.NewReader(b))
	v := ""
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		l = strings.TrimSpace(strings.TrimPrefix(l, "export "))
		name, val, ok := strings.Cut(l, "=")
		if !ok || strings.TrimSpace(name) != k {
			continue
		}
		val = strings.TrimSpace(val)
		if len(val) >= 2 && (val[0] == '"' || val[0] == '\'') {
			if end := strings.IndexByte(val[1:], val[0]); end >= 0 {
				val = val[1 : end+1]
			}
		} else if i := strings.Index(val, " #"); i >= 0 {
			val = strings.TrimSpace(val[:i])
		}
		v = val // the last one wins, as dotenv's
	}
	return v
}

// qwenpawProfile is one of QwenPaw's agents: its id and its workspace.
type qwenpawProfile struct{ id, dir string }

// qwenpawProfiles are the agents config.json lists, in agent_order, the
// default first; those switched off are left out. With no config.json yet
// it is the default alone, in its default workspace.
func qwenpawProfiles(at place, work string) []qwenpawProfile {
	def := qwenpawProfile{qwenpawDefault, filepath.Join(work, "workspaces", qwenpawDefault)}
	raw, err := os.ReadFile(filepath.Join(work, "config.json"))
	if err != nil || !json.Valid(raw) {
		return []qwenpawProfile{def}
	}
	profiles := gjson.GetBytes(raw, "agents.profiles")
	dirOf := func(id string) string {
		d := strings.TrimSpace(profiles.Get(gjson.Escape(id) + ".workspace_dir").String())
		switch {
		case d == "":
			return filepath.Join(work, "workspaces", id)
		case strings.HasPrefix(d, "~/"), strings.HasPrefix(d, `~\`):
			return filepath.Join(at.home, d[2:])
		}
		return d
	}
	var ids []string
	for _, id := range gjson.GetBytes(raw, "agents.agent_order").Array() {
		ids = append(ids, id.String())
	}
	var rest []string
	profiles.ForEach(func(k, _ gjson.Result) bool {
		if !slices.Contains(ids, k.String()) {
			rest = append(rest, k.String())
		}
		return true
	})
	sort.Strings(rest)
	ids = append(ids, rest...)
	out := []qwenpawProfile{{qwenpawDefault, dirOf(qwenpawDefault)}}
	for _, id := range ids {
		if id == qwenpawDefault || !qwenpawIDOK(id) || slices.ContainsFunc(out, func(p qwenpawProfile) bool { return p.id == id }) {
			continue
		}
		if e := profiles.Get(gjson.Escape(id) + ".enabled"); e.Exists() && !e.Bool() {
			continue
		}
		out = append(out, qwenpawProfile{id, dirOf(id)})
	}
	return out
}

// qwenpawIDOK is an agent id as QwenPaw takes one (agents.py's
// _AGENT_ID_RE): letters, digits, _ and -, 2 to 64 of them, neither end a
// _ or -.
func qwenpawIDOK(id string) bool {
	if len(id) < 2 || len(id) > 64 {
		return false
	}
	for i, r := range id {
		alnum := r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9'
		if !alnum && (i == 0 || i == len(id)-1 || r != '_' && r != '-') {
			return false
		}
	}
	return true
}

func qwenpaw(home string) *Agent {
	at := here(home)
	work := qwenpawWorking(at)
	return qwenpawAt(at, work, qwenpawProfiles(at, work)[0])
}

// qwenpawAgents are the rows of QwenPaw's agents other than the default.
func qwenpawAgents(home string) []*Agent {
	at := here(home)
	work := qwenpawWorking(at)
	if !isDir(work) {
		return nil
	}
	var out []*Agent
	for _, p := range qwenpawProfiles(at, work)[1:] {
		agentJSON := filepath.Join(p.dir, "agent.json")
		if !isFile(agentJSON) {
			continue
		}
		if b := qwenpawBackend(agentJSON); b != "qwenpaw" {
			continue
		}
		a := qwenpawAt(at, work, p)
		name, _ := edit.GetJSON(agentJSON, "name")
		a.ID, a.Name = "qwenpaw#"+p.id, "QwenPaw · "+cmp.Or(strings.TrimSpace(name), p.id)
		a.Aliases, a.UA, a.Bin = nil, nil, ""
		a.detect = func() bool { return isFile(agentJSON) }
		out = append(out, a)
	}
	return out
}

// qwenpawBackend is the backend an agent.json names, "qwenpaw" when none.
func qwenpawBackend(path string) string {
	b, _ := edit.GetJSON(path, "backend")
	return cmp.Or(strings.TrimSpace(b), "qwenpaw")
}

func qwenpawAt(at place, work string, p qwenpawProfile) *Agent {
	secret := qwenpawSecret(at, work)
	providers := filepath.Join(secret, "providers")
	prov := filepath.Join(providers, "custom", uniSwitchID+".json")
	path := filepath.Join(p.dir, "agent.json")
	// keyWas is the active_model this agent had before uni-switch's, as JSON
	// ("{}": none); keyWrote the key uni-switch last wrote in plain text, which
	// QwenPaw has encrypted since
	keyWas := at.key("qwenpaw.was." + p.id)
	keyWrote := at.key("qwenpaw.key")
	key := func() string { return agentKeyAt("qwenpaw", at.gw()) }
	get := func(k string) gjson.Result {
		raw, err := os.ReadFile(path)
		if err != nil || !json.Valid(raw) {
			return gjson.Result{}
		}
		return gjson.GetBytes(raw, k)
	}
	pick := func() string {
		m := get("active_model")
		pid, model := m.Get("provider_id").String(), m.Get("model").String()
		if pid == "" || model == "" {
			return ""
		}
		return pid + "/" + model
	}
	ours := func() bool { return prefixed(pick()) }
	// setRaw writes active_model as raw, a JSON object (what was there
	// before), or takes it out ("" or "{}"); setPick writes it v's
	// provider and model
	setRaw := func(raw string) error {
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		var out []byte
		if raw == "" || raw == "{}" {
			out, err = edit.PatchJSON(b, nil, []string{"active_model"})
		} else {
			if !json.Valid([]byte(raw)) {
				return fmt.Errorf("QwenPaw's model %q isn't JSON", raw)
			}
			// as it was, in its own order of keys
			out, err = edit.PatchJSON(b, []edit.KV{{Path: "active_model", Value: json.RawMessage(raw)}}, nil)
		}
		if err != nil {
			return err
		}
		if bytes.Equal(out, b) {
			return nil
		}
		return edit.WriteAtomic(path, out)
	}
	setPick := func(v string) error {
		pid, model, ok := strings.Cut(v, "/")
		if !ok || pid == "" || model == "" {
			return fmt.Errorf("QwenPaw names a model as <provider>/<model>, not %q", v)
		}
		cur := get("active_model")
		slot := map[string]any{}
		if cur.IsObject() {
			json.Unmarshal([]byte(cur.Raw), &slot)
		}
		slot["provider_id"], slot["model"] = pid, model
		b, _ := json.Marshal(slot)
		return setRaw(string(b))
	}
	// provider writes uni-switch's provider file as it is now, or takes it out
	// when no agent of QwenPaw's names it any more
	provider := func(on bool) error {
		if !on {
			if qwenpawNamesUniSwitch(at, work) || !isFile(prov) {
				return nil
			}
			return os.Remove(prov)
		}
		raw, err := os.ReadFile(prov)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		k := key()
		// an encrypted key is kept while it is the one uni-switch wrote: it
		// can't be read back
		was := gjson.GetBytes(raw, "api_key").String()
		keep := strings.HasPrefix(was, qwenpawEncrypted) && unstashPeek(keyWrote) == k
		out, err := qwenpawProviderJSON(raw, at.v1(), k, keep, uniSwitchModels("qwenpaw"))
		if err != nil {
			return fmt.Errorf("QwenPaw's %s: %w", prov, err)
		}
		if out == nil {
			return nil
		}
		if err := os.MkdirAll(filepath.Dir(prov), 0o700); err != nil {
			return err
		}
		if err := edit.WriteAtomic(prov, out); err != nil {
			return err
		}
		if !keep {
			stash(map[string]string{keyWrote: k})
		}
		// QwenPaw keeps its provider files to its user alone (0600)
		return at.ownerOnly(prov)
	}
	unwire := func() error {
		if !isFile(path) {
			forget(keyWas)
			return provider(false)
		}
		if ours() {
			if err := setRaw(unstash(keyWas)); err != nil {
				return err
			}
		} else {
			forget(keyWas)
		}
		return provider(false)
	}
	put := func(v string) error {
		raw, err := os.ReadFile(path)
		if errors.Is(err, os.ErrNotExist) {
			return fmt.Errorf("QwenPaw has no %s yet: start QwenPaw once (qwenpaw app) so it writes its agents' settings, then connect it", path)
		}
		if err != nil {
			return err
		}
		if !json.Valid(raw) {
			return fmt.Errorf("QwenPaw's %s isn't JSON uni-switch can read; it was left as it is", path)
		}
		if b := qwenpawBackend(path); b != "qwenpaw" {
			return fmt.Errorf("this QwenPaw agent runs on the %s backend, which doesn't use QwenPaw's models; switch it to QwenPaw's own backend first", b)
		}
		if !ours() {
			was := "{}"
			// as it was: an object, or the null QwenPaw may write
			if m := get("active_model"); m.Exists() {
				was = m.Raw
			}
			stash(map[string]string{keyWas: was})
		}
		if err := provider(true); err != nil {
			return err
		}
		return setPick(v)
	}
	return atomic(&Agent{
		ID: "qwenpaw", Name: "QwenPaw", Icon: "qwenpaw", Spelled: prefixed, NoNames: true,
		Bin: "qwenpaw", Dir: work, Path: path,
		Notice: func() string { return noticeQwenpaw.String() },
		Unwire: unwire,
		Check: func() string {
			if !ours() {
				return ""
			}
			if b := qwenpawBackend(path); b != "qwenpaw" {
				return "this QwenPaw agent's backend (agent.json) is " + b + ", which doesn't use its model, so it no longer asks uni-switch"
			}
			raw, err := os.ReadFile(prov)
			if errors.Is(err, os.ErrNotExist) {
				return "uni-switch's provider is gone from QwenPaw's " + prov + " — connect it again"
			}
			if err != nil || !json.Valid(raw) {
				return ""
			}
			kvs := []string{"base_url", at.v1()}
			// an encrypted key can't be read back: only a plain one is
			// checked
			if k := gjson.GetBytes(raw, "api_key").String(); !strings.HasPrefix(k, qwenpawEncrypted) {
				kvs = append(kvs, "api_key", key())
			}
			if e := gjson.GetBytes(raw, "enabled"); e.Exists() && !e.Bool() {
				return "uni-switch's provider is switched off in QwenPaw (" + filepath.Base(prov) + "), so it no longer asks uni-switch"
			}
			return wiringOff("QwenPaw", prov, func(k string) (string, bool) {
				r := gjson.GetBytes(raw, k)
				return r.String(), r.Exists()
			}, kvs...)
		},
		Sync: func() error {
			if !isFile(prov) {
				return nil
			}
			return provider(true)
		},
		Fields: []Field{{
			// the agent's active_model as <provider_id>/<model>: uni-switch's
			// is uni-switch/<provider>/<model>
			Key: "model", Label: "model",
			Get: pick,
			Set: func(v string) error {
				if v == "" {
					if ours() {
						return unwire()
					}
					return setRaw("")
				}
				if ref, ok := strings.CutPrefix(v, uniSwitchID+"/"); ok && isUniSwitch(ref) {
					return put(v)
				}
				// one of the user's own: uni-switch's provider stays in
				// QwenPaw's list until Disconnect
				if ours() {
					forget(keyWas)
				}
				return setPick(v)
			},
			Options: func(cur map[string]string) []Option {
				return append(qwenpawOwn(providers, cur["model"]), viaUniSwitch("qwenpaw", uniSwitchID+"/")...)
			},
		}, {
			// the agent's thinking_level (config.py's AgentProfileConfig),
			// held by QwenPaw to the levels its model's card has; "" is
			// its inherit, the model's own
			Key: "effort", Label: "effort",
			Get: func() string {
				l := get("thinking_level").String()
				if l == "inherit" {
					return ""
				}
				return l
			},
			Set: func(v string) error {
				if !isFile(path) {
					return fmt.Errorf("QwenPaw has no %s yet: start QwenPaw once (qwenpaw app) so it writes its agents' settings", path)
				}
				if l := get("thinking_level").String(); v == l && (v == "budget" || v == "off") {
					return nil
				}
				if v != "" && !slices.Contains(qwenpawLevels, v) {
					return fmt.Errorf("QwenPaw has no thinking level %q", v)
				}
				b, err := os.ReadFile(path)
				if err != nil {
					return err
				}
				// a budget goes with the budget level alone (config.py's
				// validate_thinking_budget)
				out, err := edit.PatchJSON(b, []edit.KV{{Path: "thinking_level", Value: cmp.Or(v, "inherit")}}, []string{"thinking_budget"})
				if err != nil || bytes.Equal(out, b) {
					return err
				}
				return edit.WriteAtomic(path, out)
			},
			Options: func(cur map[string]string) []Option {
				// no off: uni-switch's models declare none (qwenpawModelJSON);
				// a level set in QwenPaw is listed as it is
				o := static(qwenpawLevels...)
				if l := cur["effort"]; l == "off" || l == "budget" {
					o = append(o, Option{Value: l})
				}
				return o
			},
		}},
	}, path, prov)
}

// unstashPeek is a stashed value, left in the stash.
func unstashPeek(k string) string {
	stashMu.Lock()
	defer stashMu.Unlock()
	return stashShared()[k]
}

// qwenpawNamesuni-switch says an agent of QwenPaw's still names uni-switch's
// provider: its active_model, a fallback or its subagent model.
func qwenpawNamesUniSwitch(at place, work string) bool {
	for _, p := range qwenpawProfiles(at, work) {
		raw, err := os.ReadFile(filepath.Join(p.dir, "agent.json"))
		if err != nil || !json.Valid(raw) {
			continue
		}
		if gjson.GetBytes(raw, "active_model.provider_id").String() == uniSwitchID ||
			gjson.GetBytes(raw, "subagent_model.provider_id").String() == uniSwitchID {
			return true
		}
		for _, f := range gjson.GetBytes(raw, "fallback_models").Array() {
			if f.Get("provider_id").String() == uniSwitchID {
				return true
			}
		}
	}
	return false
}

// qwenpawOwn is the models of the user's own providers in QwenPaw, as
// <provider_id>/<model>, for the picker: every custom provider's, and a
// built-in one's when it has a key. The one the agent is on is listed
// whatever it is.
func qwenpawOwn(providers, cur string) []Option {
	var out []Option
	seen := map[string]bool{}
	for _, kind := range []string{"custom", "builtin"} {
		files, _ := filepath.Glob(filepath.Join(providers, kind, "*.json"))
		sort.Strings(files)
		for _, f := range files {
			raw, err := os.ReadFile(f)
			if err != nil || !json.Valid(raw) {
				continue
			}
			p := gjson.ParseBytes(raw)
			id := p.Get("id").String()
			if id == "" || id == uniSwitchID {
				continue
			}
			if e := p.Get("enabled"); e.Exists() && !e.Bool() {
				continue
			}
			if kind == "builtin" && p.Get("api_key").String() == "" {
				continue
			}
			removed := map[string]bool{}
			for _, r := range p.Get("removed_model_ids").Array() {
				removed[r.String()] = true
			}
			name := cmp.Or(p.Get("name").String(), id)
			for _, list := range []string{"models", "extra_models"} {
				for _, m := range p.Get(list).Array() {
					mid := m.Get("id").String()
					v := id + "/" + mid
					if mid == "" || removed[mid] || seen[v] {
						continue
					}
					seen[v] = true
					out = append(out, Option{Value: v, Label: cmp.Or(m.Get("name").String(), mid), Note: "QwenPaw's own", Group: name})
				}
			}
		}
	}
	if cur != "" && !prefixed(cur) && !seen[cur] {
		out = append(out, Option{Value: cur, Label: cur, Note: "QwenPaw's own"})
	}
	return out
}

// qwenpawOwned are the keys of uni-switch's provider file that are uni-switch's to
// say; the rest are the user's to change in QwenPaw's console.
var qwenpawOwned = []string{"id", "name", "base_url", "api_key", "chat_model", "is_custom", "extra_models"}

// qwenpawModelOwned are the keys of one of uni-switch's models that are
// uni-switch's to say, unless the user changed one in QwenPaw (the model's
// config_overrides names it).
var qwenpawModelOwned = []string{"id", "name", "source", "supports_image", "supports_multimodal", "supports_tool_calling", "max_input_length", "max_input_length_configured", "max_output_length", "thinking_param_style", "reasoning_effort_options"}

// qwenpawProviderJSON is uni-switch's provider file as it should be, from raw,
// the file as it is (nil: none yet): OpenAI Chat at gw with key (the one
// there kept when keep), and models. What isn't uni-switch's to say is kept.
// nil when the file already says that.
func qwenpawProviderJSON(raw []byte, gw, key string, keep bool, models []catalog.Model) ([]byte, error) {
	have := map[string]json.RawMessage{}
	if len(bytes.TrimSpace(raw)) > 0 {
		if err := json.Unmarshal(raw, &have); err != nil {
			return nil, errors.New("not JSON uni-switch can read; it was left as it is")
		}
	}
	// the models as they are, by id
	was := map[string]map[string]json.RawMessage{}
	var list []map[string]json.RawMessage
	json.Unmarshal(have["extra_models"], &list)
	for _, m := range list {
		var id string
		json.Unmarshal(m["id"], &id)
		was[id] = m
	}
	out := map[string]any{}
	for k, v := range have {
		out[k] = v
	}
	defaults := map[string]any{
		"enabled": true, "support_model_discovery": false, "support_connection_check": false,
		"models": []any{}, "generate_kwargs": map[string]any{}, "custom_headers": map[string]any{},
		"snapshot_schema_version": 2,
	}
	for k, v := range defaults {
		if _, ok := out[k]; !ok {
			out[k] = v
		}
	}
	out["id"], out["name"], out["base_url"] = uniSwitchID, "uni-switch", gw
	out["chat_model"], out["is_custom"] = "OpenAIChatModel", true
	if !keep {
		out["api_key"] = key
	}
	extra := []map[string]any{}
	for _, m := range models {
		e := qwenpawModelJSON(m)
		if h := was[m.ID]; h != nil {
			var overrides []string
			json.Unmarshal(h["config_overrides"], &overrides)
			for hk, hv := range h {
				// what QwenPaw filled in itself where uni-switch says
				// nothing (a window it detected, its own defaults)
				// stays too, so a sync after QwenPaw's start writes
				// nothing
				_, said := e[hk]
				if !slices.Contains(qwenpawModelOwned, hk) || slices.Contains(overrides, hk) || !said {
					e[hk] = hv
				}
			}
		}
		// a null of uni-switch's takes back what it said before, and is
		// written nowhere it said nothing
		for k, v := range e {
			if _, had := was[m.ID][k]; v == nil && !had {
				delete(e, k)
			}
		}
		extra = append(extra, e)
	}
	out["extra_models"] = extra
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	// QwenPaw writes with ensure_ascii off and an indent of 2
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(out); err != nil {
		return nil, err
	}
	// the same settings in another order of keys are no change
	var a, c any
	if len(raw) > 0 && json.Unmarshal(raw, &a) == nil && json.Unmarshal(b.Bytes(), &c) == nil && reflect.DeepEqual(a, c) {
		return nil, nil
	}
	return b.Bytes(), nil
}

// qwenpawModelJSON is one of uni-switch's models as a model of QwenPaw's
// (providers/model_info.py's ModelInfo), added by the user. Every one is
// said to take tools, as the gateway hands a tool call on whatever the
// model; images are what uni-switch knows it sees, or describes to it. A
// window under QwenPaw's least (1000) or not known is left to QwenPaw's
// default.
func qwenpawModelJSON(m catalog.Model) map[string]any {
	e := map[string]any{
		"id": m.ID, "name": cmp.Or(m.Name, m.ID), "source": "user",
		"supports_image": m.Images, "supports_multimodal": m.Images,
		"supports_tool_calling": true,
	}
	if m.Context >= 1000 {
		e["max_input_length"], e["max_input_length_configured"] = m.Context, true
	}
	if n := maxTokens(m); n > 0 {
		e["max_output_length"] = n
	}
	// QwenPaw applies an agent's thinking level (agent.json thinking_level)
	// only to a model whose card declares how it thinks: with neither
	// thinking_param_style nor thinking_control, resolve_thinking drops
	// the level (providers/provider.py thinking_control, Pkh on Discord).
	// "effort" makes it send the level as Chat's reasoning_effort, held to
	// the levels the model has; none is left out, as QwenPaw turns an off
	// into Qwen's and DeepSeek's switches (extra_body enable_thinking,
	// thinking), not the reasoning_effort uni-switch hands every vendor.
	e["thinking_param_style"], e["reasoning_effort_options"] = nil, nil
	if levels := qwenpawEfforts(m.Efforts); len(levels) > 0 {
		e["thinking_param_style"], e["reasoning_effort_options"] = "effort", levels
	}
	return e
}

// qwenpawLevels are the thinking levels QwenPaw names (thinking.py's
// ThinkingControl.efforts), weakest first.
var qwenpawLevels = []string{"minimal", "low", "medium", "high", "xhigh", "max"}

// qwenpawEfforts are a model's reasoning levels that QwenPaw names, in
// its order.
func qwenpawEfforts(efforts []string) []string {
	var out []string
	for _, l := range qwenpawLevels {
		if slices.ContainsFunc(efforts, func(e string) bool { return catalog.EffortLevel(e) == l }) {
			out = append(out, l)
		}
	}
	return out
}

// what qwenpaw says after a change (notice.go)
var noticeQwenpaw = newNotice("QwenPaw reads its providers at start-up — restart QwenPaw once after connecting it, and after uni-switch's models change; a model picked for an agent after that is used from its next message.")
