package agent

// Codex reads a model_context_window or model_auto_compact_token_limit in
// its config.toml before the window and threshold uni-switch's catalog gives
// each model (codexcat: context_window, auto_compact_token_limit, from
// Long conversations): the first becomes every model's window, and Codex
// compacts at 90% of it; the second is where it compacts, whatever the
// catalog says (codex-rs models-manager with_config_overrides). A guide
// for a third-party model that puts model_context_window = 128000 there
// kept every Codex conversation compacting near 128K while Long
// conversations said 544K (zgjfjfl on Discord, 10-31). The value is the
// user's, so uni-switch never takes it out on its own: Long conversations
// names it, and takes it out when the user asks (DropCodexCompactHolds).

import (
	"cmp"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// codexCompactKeys are the config keys Codex reads before the catalog's
// window and threshold.
var codexCompactKeys = []string{"model_context_window", "model_auto_compact_token_limit"}

// CodexCompactHold is one such key in a Codex config: at the top level, or
// in the profile the config picks ([profiles.<Profile>]).
type CodexCompactHold struct {
	Path    string `json:"path"`
	Key     string `json:"key"`
	Profile string `json:"profile,omitempty"`
	Value   string `json:"value"`
}

// CodexCompactHeld is the keys this machine's Codex config (home "" is the
// user's) has that come before Long conversations.
func CodexCompactHeld(home string) []CodexCompactHold {
	path := filepath.Join(here(tuneHome(home)).codexHome(), "config.toml")
	var out []CodexCompactHold
	for _, k := range codexCompactKeys {
		if v, ok := edit.GetTOMLTop(path, k); ok {
			out = append(out, CodexCompactHold{Path: path, Key: k, Value: compactValue(v)})
		}
	}
	if p, _ := edit.GetTOMLTop(path, "profile"); p != "" {
		t, _ := edit.GetTOMLTable(path, "profiles."+tomlQuoted(p))
		for _, k := range codexCompactKeys {
			if v, ok := t[k]; ok {
				out = append(out, CodexCompactHold{Path: path, Key: k, Profile: p, Value: compactValue(v)})
			}
		}
	}
	return out
}

// DropCodexCompactHolds takes those keys out of the config, so Codex
// compacts where Long conversations says.
func DropCodexCompactHolds(home string) error {
	for _, h := range CodexCompactHeld(home) {
		var err error
		if h.Profile == "" {
			err = edit.DelTOMLTop(h.Path, h.Key)
		} else {
			err = edit.DelTOMLKey(h.Path, "profiles."+tomlQuoted(h.Profile), h.Key)
		}
		if err != nil {
			return err
		}
	}
	return nil
}

// CodexModel is the uni-switch model this machine's Codex config runs (home ""
// is the user's): the profile's the config picks, else the top-level one,
// as Codex reads them; "" when Codex runs a model of its own provider.
// Long conversations can't compact past that model's own window: Codex
// compacts at 90% of it (zgjfjfl on Discord, 10-31: a relay's gpt-6-A).
func CodexModel(home string) string {
	path := filepath.Join(here(tuneHome(home)).codexHome(), "config.toml")
	m, _ := edit.GetTOMLTop(path, "model")
	p, _ := edit.GetTOMLTop(path, "model_provider")
	if name, _ := edit.GetTOMLTop(path, "profile"); name != "" {
		t, _ := edit.GetTOMLTable(path, "profiles."+tomlQuoted(name))
		m, p = cmp.Or(t["model"], m), cmp.Or(t["model_provider"], p)
	}
	if p != uniSwitchID {
		return ""
	}
	return m
}

// compactValue is a size as written (128_000 too), or the text as it is.
func compactValue(v string) string {
	v = strings.TrimSpace(v)
	if n, err := strconv.Atoi(strings.ReplaceAll(v, "_", "")); err == nil {
		return strconv.Itoa(n)
	}
	return v
}

// tomlQuoted is a table name part, quoted when it isn't a bare key.
func tomlQuoted(s string) string {
	for _, r := range s {
		if !(r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9' || r == '_' || r == '-') {
			return strconv.Quote(s)
		}
	}
	return s
}
