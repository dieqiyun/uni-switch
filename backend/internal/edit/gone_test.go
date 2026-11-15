package edit

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/appdir"
)

// bm_rabbit on Discord: Pi's AgentDir moved and the old one deleted, yet
// setting Pi's model made the old folder again with only models.json and
// settings.json in it. A folder an agent's variable names is not made
// again; what wrote there hears why.
func TestWriteNeverMakesANamedFolderAgain(t *testing.T) {
	gone := filepath.Join(t.TempDir(), "piData", ".pi", "agent")
	t.Setenv("PI_CODING_AGENT_DIR", gone)
	err := SetJSON(filepath.Join(gone, "models.json"), KV{Path: "providers.uni-switch.baseUrl", Value: "http://127.0.0.1:3425/v1"})
	var g *appdir.GoneError
	if !errors.As(err, &g) || g.Var != "PI_CODING_AGENT_DIR" {
		t.Errorf("writing into the gone folder: %v, want a GoneError naming PI_CODING_AGENT_DIR", err)
	}
	if _, err := os.Stat(filepath.Dir(gone)); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("the folder was made again (%v)", err)
	}
	// with no variable it is Pi's default folder, made as before
	t.Setenv("PI_CODING_AGENT_DIR", "")
	if err := SetJSON(filepath.Join(gone, "models.json"), KV{Path: "a", Value: 1}); err != nil {
		t.Errorf("a folder no variable names: %v", err)
	}
}
