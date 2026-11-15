package provider

import (
	"os"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/filememo"
)

// TestStepFunSignedInReadOnce: a provider list asked each StepFun
// provider's sign-in, and each ask read stepfun-auth.json; a request
// reads it once, and a sign-in saved in it is seen at once.
func TestStepFunSignedInReadOnce(t *testing.T) {
	stepfunHome(t, stepfunOn("stepfun", "StepFun", "ai"), stepfunOn("stepfun-cn", "StepFun (China)", "com"))
	defer func(d time.Duration) { heldFor = d }(heldFor)
	heldFor = time.Hour // a loaded machine's pause isn't the bound
	if StepFunSignedIn("stepfun") {
		t.Fatal("signed in with no file")
	}
	acc, ref := stepfunTokens("4161", time.Now().Add(time.Hour), true)
	if err := saveStepFun(map[string]stepfunSession{"stepfun": {Access: acc, Refresh: ref, Account: "4161"}}); err != nil {
		t.Fatal(err)
	}
	// written a while ago, so a request keeps what it read without a look
	ago := time.Now().Add(-time.Hour)
	if err := os.Chtimes(stepfunPath(), ago, ago); err != nil {
		t.Fatal(err)
	}
	defer Hold()()
	defer filememo.Hold()() // as /api/ holds both
	parses := stepfunParses.Load()
	for range 20 {
		if !StepFunSignedIn("stepfun") || StepFunSignedIn("stepfun-cn") {
			t.Fatal("signed in: want stepfun only")
		}
	}
	if n := stepfunParses.Load() - parses; n != 1 {
		t.Fatalf("40 asks in one request parsed %d times, want 1", n)
	}
	if err := saveStepFun(map[string]stepfunSession{"stepfun-cn": {Access: acc, Refresh: ref, Account: "4161"}}); err != nil {
		t.Fatal(err)
	}
	if StepFunSignedIn("stepfun") || !StepFunSignedIn("stepfun-cn") {
		t.Fatal("a sign-in saved in the request wasn't seen")
	}
}
