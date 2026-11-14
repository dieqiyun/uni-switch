package cli

import (
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch model suffix off has the agents' lists name models alone, on
// names them with their providers again (#335), own all but the names the
// user gave (#92); anything else is refused.
func TestModelSuffixCmd(t *testing.T) {
	groupsHome(t)
	catalog.Changed = nil // no agent's files are rewritten here
	if err := modelCmd([]string{"suffix"}); err != nil {
		t.Fatal(err)
	}
	if err := modelCmd([]string{"suffix", "off"}); err != nil || !settings.Load().PlainNames {
		t.Fatal(err, settings.Load().PlainNames)
	}
	if err := modelCmd([]string{"suffix"}); err != nil {
		t.Fatal(err)
	}
	if err := modelCmd([]string{"suffix", "on"}); err != nil || settings.Load().PlainNames {
		t.Fatal(err, settings.Load().PlainNames)
	}
	// own: a name the user gave just as they wrote it, the others with
	// their provider's (#92)
	if err := modelCmd([]string{"suffix", "own"}); err != nil || provider.SuffixMode() != provider.SuffixOwn {
		t.Fatal(err, provider.SuffixMode())
	}
	if err := modelCmd([]string{"suffix"}); err != nil {
		t.Fatal(err)
	}
	if err := modelCmd([]string{"suffix", "on"}); err != nil || provider.SuffixMode() != provider.SuffixOn {
		t.Fatal(err, provider.SuffixMode())
	}
	if err := modelCmd([]string{"suffix", "maybe"}); err == nil {
		t.Fatal("took maybe")
	}
}
