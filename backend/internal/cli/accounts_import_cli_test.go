package cli

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// importJWT is an unsigned JWT carrying claims, as ChatGPT's tokens are read.
func importJWT(claims map[string]any) string {
	b, _ := json.Marshal(claims)
	return "e30." + base64.RawURLEncoding.EncodeToString(b) + ".sig"
}

// uni-switch accounts import (#1453, wlj521: several auth.json files, the way
// Cockpit Tools takes them): it says first that the tool a file came from
// is signed out, imports nothing until that is agreed to, takes several
// files at once, names a file that held no account, and only reads them.
func TestAccountsImport(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude"))
	t.Setenv("CODEX_HOME", filepath.Join(home, ".codex"))
	asked := 0
	fake := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]string
		json.NewDecoder(r.Body).Decode(&body)
		asked++
		who := strings.TrimPrefix(body["refresh_token"], "r-")
		if who == body["refresh_token"] {
			w.WriteHeader(400)
			json.NewEncoder(w).Encode(map[string]any{"error": "invalid_grant"})
			return
		}
		json.NewEncoder(w).Encode(map[string]any{
			"id_token": importJWT(map[string]any{"email": who + "@example.com",
				"https://api.openai.com/auth": map[string]any{"chatgpt_plan_type": "pro", "chatgpt_account_id": "acct-" + who}}),
			"access_token":  importJWT(map[string]any{"exp": float64(time.Now().Add(time.Hour).Unix())}),
			"refresh_token": "n-" + who,
		})
	}))
	defer fake.Close()
	t.Cleanup(provider.CodexTokenURLForTest(fake.URL))
	// no answer on stdin: not agreed to
	null, err := os.Open(os.DevNull)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	oldIn := os.Stdin
	os.Stdin = null
	t.Cleanup(func() { os.Stdin = oldIn })

	dir := t.TempDir()
	write := func(name, s string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(s), 0o600); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a := write("a.json", `{"OPENAI_API_KEY":null,"tokens":{"id_token":"x","access_token":"y","refresh_token":"r-a","account_id":"acct-a"}}`)
	b := write("b.json", `{"OPENAI_API_KEY":null,"tokens":{"id_token":"x","access_token":"y","refresh_token":"r-b","account_id":"acct-b"}}`)
	bad := write("notes.txt", "{not json")
	before := map[string]string{}
	for _, p := range []string{a, b, bad} {
		x, _ := os.ReadFile(p)
		before[p] = string(x)
	}
	run := func(args ...string) (string, error) {
		t.Helper()
		return said(t, func() error { return accountsCmd(append([]string{"accounts", "import"}, args...)) })
	}

	// not agreed to: the takeover said, nothing asked of ChatGPT
	out, err := run("codex", a, b)
	if err == nil || !strings.Contains(err.Error(), "canceled") || asked != 0 || len(provider.Logins("codex")) != 0 {
		t.Fatalf("imported without --yes: %v %d\n%s", err, asked, out)
	}
	if !strings.Contains(out, "signed out of that account") || !strings.Contains(out, "only read") {
		t.Fatalf("the takeover wasn't said:\n%s", out)
	}

	out, err = run("codex", a, b, bad, "--yes")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	for _, want := range []string{"added a@example.com", "added b@example.com", bad + ":", "see them: uni-switch accounts codex"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if len(provider.Logins("codex")) != 2 {
		t.Errorf("logins %+v", provider.Logins("codex"))
	}
	for p, s := range before {
		if x, err := os.ReadFile(p); err != nil || string(x) != s {
			t.Errorf("%s changed: %q %v", p, x, err)
		}
	}

	// a file that can't be read stops the import; so does one holding none
	if _, err := run("codex", filepath.Join(dir, "gone.json"), "--yes"); err == nil {
		t.Error("a missing file was taken")
	}
	if _, err := run("codex", bad, "--yes"); err == nil || !strings.Contains(err.Error(), bad) {
		t.Errorf("a file of nothing: %v", err)
	}
	if _, err := run("gemini", a, "--yes"); err == nil {
		t.Error("imported into Gemini CLI")
	}
}
