package sessions

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// CodeBuddy Code's direct calls reach the usage ledger (#1779). The two
// sessions in testdata/codebuddy are CodeBuddy Code 2.164.0's own files, from
// two `codebuddy -p` runs against a local OpenAI fake: one on a model the user
// added for another vendor (fake-m1), whose two replies (a Read tool call,
// then text) are calls of the agent's own; one on a model uni-switch wired
// (deepseek/pro, vendor "uni-switch" in models.json), whose replies the gateway
// logs itself and are not read here as well.
func TestCodeBuddyCalls(t *testing.T) {
	setupAgents(t)
	var got []Call
	for _, c := range Calls(time.Time{}) {
		if c.Agent == "codebuddy" {
			got = append(got, c)
		}
	}
	if len(got) != 2 {
		t.Fatalf("want CodeBuddy Code's 2 direct calls, got %d: %+v", len(got), got)
	}
	// each reply: 27017 in, of them 26496 read from the cache; 115 out
	want := Tokens{Input: 27017 - 26496, Output: 115, CacheRead: 26496}
	// newest first: the text reply, then the tool call before it
	for i, w := range []struct {
		msg string
		at  int64
	}{{"01a12fea4ecc74979fe1b9832df42ddb", 1791805247199}, {"01a12fea4e6378c680e219148e8547be", 1791805247141}} {
		c := got[i]
		if c.Msg != w.msg || !c.Time.Equal(ms(w.at)) || c.Model != "fake-m1" || c.Tokens != want || c.Session != "01a12fea-4cae-78ac-8b2b-a1c0f9da9b1c" {
			t.Fatalf("call %d: %+v", i, c)
		}
	}
}

// WorkBuddy's calls are still WorkBuddy's: the two agents share a reader,
// and each is read against its own models.json. deepseek/pro is uni-switch's in
// CodeBuddy's models.json and not in WorkBuddy's, so the same id there
// would be the user's own.
func TestCodeBuddyGatewayIsItsOwn(t *testing.T) {
	setupAgents(t)
	if !buddyGateway(CodeBuddyDir(), "custom-local:deepseek/pro") {
		t.Fatal("uni-switch's model in CodeBuddy's models.json is not read as the gateway's")
	}
	if buddyGateway(CodeBuddyDir(), "custom-local:fake-m1") {
		t.Fatal("the user's own model in CodeBuddy's models.json is read as the gateway's")
	}
	if workbuddyGateway("custom-local:deepseek/pro") {
		t.Fatal("CodeBuddy's models.json was read for WorkBuddy")
	}
}

// CodeBuddy Code told to keep its files in WorkBuddy's folder
// (CODEBUDDY_CONFIG_DIR=~/.workbuddy, TimWongUp's .zshrc in #2932) leaves
// one folder for both: its sessions are read once, as WorkBuddy's, on the
// Sessions page and in usage alike, and the folder is named once. Read as
// both, each was listed twice, its calls went to CodeBuddy Code, and a
// refresh took each path twice and closed a nil channel.
func TestCodeBuddyInWorkBuddysFolderIsReadOnce(t *testing.T) {
	// "both": both variables on one folder, as WorkBuddy's installer and
	// its IDE host set them on Windows (mxl2498, cunnak, Tsuki-hash)
	for _, how := range []string{"named", "linked", "both"} {
		t.Run(how, func(t *testing.T) {
			inZone(t, 0)
			home := setupAgents(t)
			dir := filepath.Join(home, ".workbuddy")
			switch how {
			case "linked":
				dir = filepath.Join(home, "buddy-link")
				if err := os.Symlink(filepath.Join(home, ".workbuddy"), dir); err != nil {
					t.Skip("no symlinks here:", err)
				}
			case "both":
				t.Setenv("WORKBUDDY_CONFIG_DIR", dir)
			}
			t.Setenv("CODEBUDDY_CONFIG_DIR", dir)
			// the start's warm-up, which quit uni-switch a few seconds in
			if st, _ := StatsAndNative(0); len(st.Sessions) == 0 {
				t.Fatal("the warm-up read no sessions")
			}
			ss := List(0)
			if w, c := count(ss, "workbuddy"), count(ss, "codebuddy"); w != 3 || c != 0 {
				t.Fatalf("listed %d WorkBuddy and %d CodeBuddy Code sessions, want 3 and 0", w, c)
			}
			n := map[string]int{}
			for _, c := range Calls(time.Time{}) {
				n[c.Agent]++
			}
			// TestWorkBuddyCalls' six, each once
			if n["workbuddy"] != 6 || n["codebuddy"] != 0 {
				t.Fatalf("calls %v, want WorkBuddy's 6 and no CodeBuddy Code's", n)
			}
			for _, s := range StatsFor(0).Sessions {
				if s.Agent == "codebuddy" {
					t.Fatalf("stats counted a WorkBuddy session as CodeBuddy Code's too: %s", s.Key)
				}
			}
			named := 0
			for _, d := range Dirs() {
				if sameFolder(d, filepath.Join(home, ".workbuddy")) {
					named++
				}
			}
			if named != 1 {
				t.Fatalf("WorkBuddy's folder named %d times in %v", named, Dirs())
			}
		})
	}
}
