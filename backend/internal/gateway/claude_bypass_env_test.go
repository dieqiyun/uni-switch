package gateway

import (
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/claudetui"
)

// Claude Code records an accepted Bypass Permissions disclaimer in its
// config folder's settings.json for good (#5042). The shim may accept it
// only in an account folder of uni-switch's; in the account Claude Code is
// signed in to (configDir "", the user's own config folder) it never does,
// though uni-switch's folder is trusted for the run there.
func TestClaudeBypassOnlyInUniSwitchsAccountFolder(t *testing.T) {
	yes := claudetui.BypassEnv + "=1"
	if got := claudeBypassEnv(true, ""); slices.Contains(got, yes) {
		t.Fatalf("user's own account: got %v, want no %s", got, yes)
	}
	if got := claudeBypassEnv(false, "/x/accounts/a"); slices.Contains(got, yes) {
		t.Fatalf("-p run: got %v, want no %s", got, yes)
	}
	if got := claudeBypassEnv(true, "/x/accounts/a"); !slices.Contains(got, yes) {
		t.Fatalf("uni-switch's account folder: got %v, want %s", got, yes)
	}
}
