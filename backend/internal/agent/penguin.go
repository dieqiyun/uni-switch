package agent

// PenguinHarness (Prism-Shadow/penguin-harness, the penguin command; npm
// @prismshadow/penguin-cli) keeps each Project's models in one TOML file
// under its data root: $PENGUIN_HOME, else ~/.penguin/data on every system
// (core's resolveRoot, os.homedir()), the CLI's and the desktop app's
// alike. The CLI works in the Project $PENGUIN_PROJECT_ID, else
// default_project (client.ts), the one its server makes first, so the file
// is <root>/<project>/.project_config.toml (projectConfigPath). Its
// shape (core state/project-config.ts, penguin 0.2.17):
//
//	default_model = { provider = "uni-switch", model_id = "<model>" }
//	[providers.uni-switch]
//	base_url = "http://127.0.0.1:3425/v1"
//	client_type = "openai-responses"
//	api_key = "uni-switch-penguin"
//	[[models]]
//	provider = "uni-switch"
//	model_id = "<model>"
//	display_name = "…"
//	context_window = …
//	vision = true
//	max_tokens = …
//	[models.pricing]
//	unit = "usd_per_mtok"
//	cache_read = …
//	cache_write = …
//	output = …
//
// A model is the pair (provider, model_id); one without a base_url, client
// type or key of its own takes its group's, [providers.<provider>]. Its
// requests go through MMSP's client of that type: openai-responses posts
// base_url + /responses, with its thinking level as reasoning.effort,
// which openai-chat leaves out. The server reads the file again whenever
// its mtime moves (ProjectConfigService), so a change needs no restart;
// its own Models page and `penguin config model …` write the whole file
// back (smol-toml), every key uni-switch writes being one it knows. A
// default_model naming no entry is refused, so the entries go in before
// the default and out after it.

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pelletier/go-toml/v2"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// penguinClient is how Penguin speaks to the gateway: OpenAI Responses,
// the one of its OpenAI clients that carries a thinking level.
const penguinClient = "openai-responses"

// penguinConfig is what uni-switch reads of a Project's .project_config.toml.
type penguinConfig struct {
	DefaultModel *penguinRef `toml:"default_model"`
	VisionModel  *penguinRef `toml:"vision_model"`
	Models       []struct {
		Provider    string `toml:"provider"`
		ModelID     string `toml:"model_id"`
		DisplayName string `toml:"display_name"`
	} `toml:"models"`
}

type penguinRef struct {
	Provider string `toml:"provider"`
	ModelID  string `toml:"model_id"`
}

// value is the pair as the model field spells it: provider/model_id.
func (r *penguinRef) value() string {
	if r == nil || r.ModelID == "" {
		return ""
	}
	return r.Provider + "/" + r.ModelID
}

// readPenguin is the config at path; a missing file is an empty one, and
// one that can't be read or parsed is an error.
func readPenguin(path string) (penguinConfig, error) {
	var c penguinConfig
	raw, err := edit.Read(path)
	if err != nil || len(raw) == 0 {
		return c, err
	}
	err = toml.Unmarshal(raw, &c)
	return c, err
}

// PenguinRoot is PenguinHarness's data root under home: $PENGUIN_HOME,
// else ~/.penguin/data.
func PenguinRoot(home string) string { return penguinRoot(here(home)) }

// penguinRoot is the data root at a place; a WSL distro's variables
// aren't read, so it is ~/.penguin/data there.
func penguinRoot(at place) string {
	if d := at.getenv("PENGUIN_HOME"); d != "" {
		return d
	}
	return at.join(".penguin", "data")
}

// penguinProject is the Project the CLI works in: $PENGUIN_PROJECT_ID,
// where it names a folder of the root, else default_project.
func penguinProject(at place) string {
	if p := strings.TrimSpace(at.getenv("PENGUIN_PROJECT_ID")); p != "" && p != "." && p != ".." && !strings.ContainsAny(p, `/\`) {
		return p
	}
	return "default_project"
}

func penguin(home string) *Agent { return penguinIn(here(home)) }

// penguinIn is PenguinHarness at a place: this machine's home, or a WSL
// distro's (see wsl.go), its provider naming the gateway as it reaches it
// from there.
func penguinIn(at place) *Agent {
	root := penguinRoot(at)
	path := filepath.Join(root, penguinProject(at), ".project_config.toml")
	providerTable := "providers." + uniSwitchID
	stashKey := at.key("penguin.default")
	absentKey := at.key("penguin.before.absent")
	key := func() string { return agentKeyAt("penguin", at.gw()) }
	get := func() string { c, _ := readPenguin(path); return c.DefaultModel.value() }
	writeUniSwitch := func() error {
		// Penguin keeps its config private (it holds API keys), so a file
		// uni-switch starts is made the same way.
		if _, err := os.Stat(path); os.IsNotExist(err) {
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				return err
			}
			if err := os.WriteFile(path, nil, 0o600); err != nil {
				return err
			}
		}
		kvs := []edit.KV{
			{Path: "base_url", Value: at.v1()},
			{Path: "client_type", Value: penguinClient},
			{Path: "api_key", Value: key()},
		}
		// A sync with nothing new leaves the file, and its time, alone.
		t, _ := edit.GetTOMLTable(path, providerTable)
		same := t != nil
		for _, kv := range kvs {
			same = same && t[kv.Path] == kv.Value
		}
		if !same {
			if err := kimiProvider(path, providerTable, kvs...); err != nil {
				return err
			}
		}
		return edit.SetTOMLArrayTables(path, "models", "provider", uniSwitchID, penguinBlocks(uniSwitchModels("penguin")))
	}
	dropUniSwitch := func() error {
		if err := edit.SetTOMLArrayTables(path, "models", "provider", uniSwitchID, nil); err != nil {
			return err
		}
		return edit.DelTOMLTable(path, providerTable)
	}
	// setRef points key (default_model, vision_model) at provider/model_id,
	// or takes it out for ""
	setRef := func(k, v string) error {
		if v == "" {
			return edit.DelTOMLTop(path, k)
		}
		p, m, _ := strings.Cut(v, "/")
		// spelled as Penguin's own writer spells it
		return edit.SetTOMLTop(path, edit.KV{Path: k, Value: edit.Raw(
			"{ provider = " + penguinString(p) + ", model_id = " + penguinString(m) + " }")})
	}
	// own reports whether Penguin has the entry v of the user's
	own := func(v string) bool {
		c, err := readPenguin(path)
		if err != nil {
			return false
		}
		for _, m := range c.Models {
			if m.Provider != uniSwitchID && m.Provider+"/"+m.ModelID == v {
				return true
			}
		}
		return false
	}
	return atomic(&Agent{
		ID: "penguin", Name: "PenguinHarness", Icon: "penguin-color", Bin: "penguin",
		Aliases: []string{"penguin-harness", "penguinharness"}, Spelled: prefixed,
		Dir: filepath.Dir(root), Path: path,
		// the desktop app, on this machine (a distro has none): installed
		// and not opened yet, it has made no ~/.penguin
		sign:     func() bool { return at.spell == nil && (&Agent{ID: "penguin"}).HasApp() },
		ListOnly: true,
		Sync: func() error {
			t, err := edit.GetTOMLTable(path, providerTable)
			if err != nil || t == nil {
				return err
			}
			return writeUniSwitch()
		},
		Check: func() string {
			c, err := readPenguin(path)
			if err != nil {
				return err.Error()
			}
			if c.DefaultModel == nil || c.DefaultModel.Provider != uniSwitchID {
				return ""
			}
			found := false
			for _, m := range c.Models {
				found = found || m.Provider == uniSwitchID && m.ModelID == c.DefaultModel.ModelID
			}
			if !found {
				return "PenguinHarness's model " + c.DefaultModel.ModelID + " (.project_config.toml) is gone, so it no longer reaches uni-switch"
			}
			t, err := edit.GetTOMLTable(path, providerTable)
			if err != nil {
				return err.Error()
			}
			if t == nil {
				return "PenguinHarness's [" + providerTable + "] (.project_config.toml) is gone, so it no longer reaches uni-switch"
			}
			return wiringOff("PenguinHarness", path, func(k string) (string, bool) { v, ok := t[k]; return v, ok },
				"base_url", at.v1(), "api_key", key())
		},
		// a file uni-switch made is taken away again once nothing is left in it
		Restore: func() error {
			if unstash(absentKey) != "1" {
				return nil
			}
			raw, err := os.ReadFile(path)
			if err != nil {
				return nil
			}
			var now map[string]any
			if toml.Unmarshal(raw, &now) != nil || len(now) > 0 {
				return nil
			}
			if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
				return err
			}
			return nil
		},
		Fields: []Field{{
			// default_model, as provider/model_id
			Key: "model", Label: "model",
			Get: get,
			Set: func(v string) error {
				cur := get()
				if ref, ok := strings.CutPrefix(v, uniSwitchID+"/"); ok && isUniSwitch(ref) {
					if !usesUniSwitch(cur) {
						kept := map[string]string{stashKey: cur}
						if t, _ := edit.GetTOMLTable(path, providerTable); t == nil {
							if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
								kept[absentKey] = "1"
							} else {
								kept[absentKey] = ""
							}
						}
						stash(kept)
					}
					if err := writeUniSwitch(); err != nil {
						return err
					}
					return setRef("default_model", v)
				}
				if v != "" && !strings.Contains(strings.Trim(v, "/"), "/") {
					return errors.New("expected provider/model_id, got " + strconv.Quote(v))
				}
				if v == "" && usesUniSwitch(cur) {
					// back to the default the user had, if Penguin still has it
					if was := unstash(stashKey); was != "" && !usesUniSwitch(was) && own(was) {
						v = was
					}
				}
				if err := setRef("default_model", v); err != nil {
					return err
				}
				// a vision model picked among uni-switch's in Penguin goes with them
				if c, err := readPenguin(path); err == nil && c.VisionModel != nil && c.VisionModel.Provider == uniSwitchID {
					if err := setRef("vision_model", ""); err != nil {
						return err
					}
				}
				return dropUniSwitch()
			},
			Options: func(cur map[string]string) []Option {
				return withVia(penguinOwnOptions(path, cur["model"]), "penguin", uniSwitchID+"/")
			},
		}},
	}, path)
}

// penguinOwnOptions are the models of the user's own in the Project's
// config, as provider/model_id, and the current pick when it is none of
// them.
func penguinOwnOptions(path, cur string) []Option {
	c, _ := readPenguin(path)
	seen := map[string]bool{}
	var out []Option
	for _, m := range c.Models {
		v := m.Provider + "/" + m.ModelID
		if m.Provider == "" || m.ModelID == "" || m.Provider == uniSwitchID || seen[v] {
			continue
		}
		seen[v] = true
		o := Option{Value: v, Icon: modelIcon(m.Provider, m.ModelID)}
		if m.DisplayName != "" && m.DisplayName != m.ModelID {
			o.Note = m.DisplayName
		}
		out = append(out, o)
	}
	if cur != "" && !seen[cur] && !usesUniSwitch(cur) {
		out = append([]Option{{Value: cur, Icon: modelIcon("", cur)}}, out...)
	}
	return group("PenguinHarness", out)
}

// penguinBlocks are uni-switch's models as entries of Penguin's [[models]],
// each with what its own Models page records for one: the name, context
// window, cap on a reply, whether it takes images and its list price.
func penguinBlocks(ms []catalog.Model) []string {
	out := make([]string, 0, len(ms))
	for _, m := range ms {
		var b strings.Builder
		line := func(k, v string) { b.WriteString(k + " = " + v + "\n") }
		b.WriteString("[[models]]\n")
		line("provider", penguinString(uniSwitchID))
		line("model_id", penguinString(m.ID))
		if m.Name != "" && m.Name != m.ID {
			line("display_name", penguinString(m.Name))
		}
		if m.Context > 0 {
			line("context_window", strconv.Itoa(m.Context))
		}
		// Penguin takes a model for one that reads images unless told not
		line("vision", strconv.FormatBool(m.Images))
		if n := maxTokens(m); n > 0 && m.Output > 0 {
			line("max_tokens", strconv.Itoa(n))
		}
		// its three buckets: cache_write is input not read from a cache
		if p := m.Price; p != nil && (p.Input > 0 || p.Output > 0 || p.CacheRead > 0) {
			b.WriteString("\n[models.pricing]\n")
			line("unit", penguinString("usd_per_mtok"))
			line("cache_read", penguinFloat(p.CacheRead))
			line("cache_write", penguinFloat(p.Input))
			line("output", penguinFloat(p.Output))
		}
		out = append(out, b.String())
	}
	return out
}

// penguinString is s as a TOML basic string: JSON's escapes are all TOML's.
func penguinString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// penguinFloat is f as a TOML float.
func penguinFloat(f float64) string {
	s := strconv.FormatFloat(f, 'f', -1, 64)
	if !strings.ContainsAny(s, ".eE") {
		s += ".0"
	}
	return s
}
