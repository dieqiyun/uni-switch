package provider

import (
	"encoding/json"
	"errors"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
	"github.com/dieqiyun/uni-switch/backend/internal/edit"
)

// OpenCode's own sign-ins, as `opencode auth login` keeps them: auth.json in
// its data folder, $XDG_DATA_HOME/opencode, else ~/.local/share/opencode
// (xdg-basedir's, on Windows too). It maps a provider id (models.dev's) to
// {"type":"api","key":…}, {"type":"oauth",…} or {"type":"wellknown",…}
// (packages/opencode/src/auth/index.ts). It holds keys only, never where
// they go: an id is imported when uni-switch has its preset.

func openCodeAuthPath() string {
	if d := appdir.Getenv("XDG_DATA_HOME"); d != "" {
		return filepath.Join(d, "opencode", "auth.json")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "share", "opencode", "auth.json")
}

func readOpenCodeAuth(path string) ([]AppImport, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var all map[string]json.RawMessage
	if err := json.Unmarshal(edit.TrimBOM(raw), &all); err != nil {
		return nil, errors.New("OpenCode's auth.json: " + err.Error())
	}
	var out []AppImport
	for _, id := range slices.Sorted(maps.Keys(all)) {
		var e struct {
			Type string `json:"type"`
			Key  string `json:"key"`
		}
		if json.Unmarshal(all[id], &e) != nil {
			continue // OpenCode passes over what it can't read too
		}
		it := AppImport{Ref: id, From: "auth.json"}
		pr := Preset(id)
		name := id
		if pr != nil {
			name = pr.Name
		}
		skip := ""
		switch {
		case e.Type == "oauth" || e.Type == "wellknown":
			skip = "it is a sign-in, not a key; add the subscription in uni-switch"
		case e.Type != "api":
			continue
		case pr == nil:
			skip = "it has no base URL" // OpenCode keeps only its key here
		case strings.TrimSpace(e.Key) == "":
			skip = "it has no API key"
		default:
			it.Provider, skip = imported(name, strings.TrimSpace(e.Key), endpoints{pr.Chat, pr.Responses, pr.Anthropic, ""}, nil)
		}
		if skip != "" {
			it.Provider, it.Skip = Provider{Name: name}, skip
		}
		out = append(out, it)
	}
	return out, nil
}
