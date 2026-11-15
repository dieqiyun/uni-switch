package cli

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// `uni-switch agy run`, what the function agy in the user's shells runs
// (bestony on Discord): the agy on PATH, given uni-switch's gateway, key and
// --model while settings.json's model is uni-switch's, every argument agy's;
// as it is off uni-switch.
func TestAgyRun(t *testing.T) {
	home := helpHome(t)
	settings := filepath.Join(home, ".gemini", "antigravity-cli", "settings.json")
	os.MkdirAll(filepath.Dir(settings), 0o755)
	t.Setenv("GEMINI_API_KEY", "users-own")
	t.Setenv("GOOGLE_GEMINI_BASE_URL", "")
	var bin string
	var args, env []string
	oldExec, oldLook := agyExec, agyLook
	t.Cleanup(func() { agyExec, agyLook = oldExec, oldLook })
	agyExec = func(b string, a, e []string) error {
		bin, args, env = b, a, e
		return nil
	}
	real := filepath.Join(home, "bin", "agy")
	agyLook = func() (string, error) { return real, nil }
	envOf := func(k string) []string {
		var out []string
		for _, kv := range env {
			if strings.HasPrefix(kv, k+"=") {
				out = append(out, strings.TrimPrefix(kv, k+"="))
			}
		}
		return out
	}

	os.WriteFile(settings, []byte(`{"model": "uni-switch/deepseek/pro", "modelProvider": "gemini"}`), 0o644)
	for _, c := range []struct{ in, want []string }{
		{nil, []string{"--model", "uni-switch/deepseek/pro"}},
		{[]string{"--help"}, []string{"--model", "uni-switch/deepseek/pro", "--help"}},
		{[]string{"-p", "hi", "--model", "mine"}, []string{"-p", "hi", "--model", "mine"}},
		{[]string{"--model=mine"}, []string{"--model=mine"}},
		{[]string{"-model", "mine"}, []string{"-model", "mine"}},
	} {
		if _, err := runOut(t, append([]string{"agy", "run"}, c.in...)...); err != nil {
			t.Fatal(err)
		}
		if bin != real || !slices.Equal(args, c.want) {
			t.Fatalf("agy run %v: %s %v, want %v", c.in, bin, args, c.want)
		}
		if k, u := envOf("GEMINI_API_KEY"), envOf("GOOGLE_GEMINI_BASE_URL"); !slices.Equal(k, []string{gateway.TokenFor("agy")}) || !slices.Equal(u, []string{gateway.URL()}) {
			t.Fatalf("env: key %v, url %v", k, u)
		}
	}
	out, err := runOut(t, "agy", "env")
	if want := "GEMINI_API_KEY=" + gateway.TokenFor("agy") + "\nGOOGLE_GEMINI_BASE_URL=" + gateway.URL() + "\nmodel=uni-switch/deepseek/pro\n"; err != nil || out != want {
		t.Fatalf("agy env: %q %v", out, err)
	}

	// off uni-switch: as it is
	os.WriteFile(settings, []byte(`{"model": "mine"}`), 0o644)
	if _, err := runOut(t, "agy", "run", "-p", "hi"); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(args, []string{"-p", "hi"}) || !slices.Equal(envOf("GEMINI_API_KEY"), []string{"users-own"}) {
		t.Fatalf("off uni-switch: %v %v", args, envOf("GEMINI_API_KEY"))
	}
	if out, err := runOut(t, "agy", "env"); err != nil || out != "" {
		t.Fatalf("agy env off uni-switch: %q %v", out, err)
	}

	// what --help and a stray value say
	if out, _ := runOut(t, "agy", "--help"); !strings.Contains(out, "uni-switch agy run [args…]") {
		t.Fatalf("agy --help:\n%s", out)
	}
	if _, err := runOut(t, "agy", "env", "x"); err == nil {
		t.Fatal("agy env x: no error")
	}
	agyLook = func() (string, error) { return "", errors.New("not found") }
	if _, err := runOut(t, "agy", "run"); err == nil || !strings.Contains(err.Error(), "isn't installed") {
		t.Fatalf("no agy: %v", err)
	}
}
