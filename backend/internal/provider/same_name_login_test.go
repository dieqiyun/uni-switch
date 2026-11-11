package provider

import (
	"encoding/json"
	"testing"
)

// The built-in path: an agent's own account hidden in uni-switch and one of
// uni-switch's of the same name, the latter listed. Removing it removes it,
// not the hidden one.
func TestForgetSideLoginTakesTheListedOne(t *testing.T) {
	claudeHome(t)
	writeFile(t, loginsPath(), []savedLogin{
		{Agent: "side", User: "a@x", Hidden: hiddenNoMark},
		{Agent: "side", User: "a@x", Auth: json.RawMessage(`{"t":1}`), On: true},
	})
	usable := func(savedLogin) bool { return true }
	ls := sideLogins("side", "a@x", usable)
	if len(ls) != 1 || ls[0].saved.own() {
		t.Fatalf("listed %+v, want uni-switch's", ls)
	}
	var gone savedLogin
	if err := forgetSideLogin("side", "a@x", ls, func(l savedLogin) { gone = l }); err != nil {
		t.Fatal(err)
	}
	if len(gone.Auth) == 0 {
		t.Errorf("told of %+v going, not uni-switch's", gone)
	}
	var left []savedLogin
	for _, l := range readLogins() {
		if l.Agent == "side" {
			left = append(left, l)
		}
	}
	if len(left) != 1 || !left[0].own() || left[0].Hidden == "" {
		t.Errorf("left %+v, want the agent's own, hidden", left)
	}
}
