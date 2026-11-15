package agent

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// #3896: an agent made for a stopped distro opens none of its files, as
// opening one starts the distro again; the user stopped it, and uni-switch
// started it every few seconds while its window was up. Every kind is made
// at a cold place in a child run, whose -test.testlogfile lists each file
// the os package was asked to stat or open.
func TestWSLColdPlaceOpensNothing(t *testing.T) {
	if root := os.Getenv("UNI_SWITCH_TEST_COLD_ROOT"); root != "" {
		for _, k := range wslKinds {
			d := distro{Name: "Stopped", Root: root, Home: "/home/me", Has: map[string]bool{"dir:." + k.id: true}}
			k.in(d.place(k.id + "@wsl:Stopped"))
			// and what the window asks of its row
			a := wslAgent(k, d)
			a.Values()
			a.Notice()
			a.Drift()
			a.Wired()
			for _, f := range a.Fields {
				if f.Options != nil {
					f.Options(a.Values())
				}
			}
		}
		return
	}
	root := t.TempDir()
	home := filepath.Join(root, "home", "me")
	// what each kind would read were it looking: Hermes's sticky profile
	// and dsh's profiles among them
	for _, f := range []string{".hermes/active_profile", ".hermes/profiles/work/config.yaml",
		".dsh/profiles/web/cordis.patch.yml", ".claude/settings.json", ".codex/config.toml",
		".config/opencode/opencode.json", ".kimi-code/config.toml", ".omp/agent/config.yml"} {
		p := filepath.Join(home, filepath.FromSlash(f))
		os.MkdirAll(filepath.Dir(p), 0o755)
		body := "{}"
		if strings.HasSuffix(f, "active_profile") {
			body = "work\n"
		}
		os.WriteFile(p, []byte(body), 0o644)
	}
	log := filepath.Join(t.TempDir(), "testlog")
	cmd := exec.Command(os.Args[0], "-test.run=^TestWSLColdPlaceOpensNothing$", "-test.testlogfile="+log)
	cmd.Env = append(os.Environ(), "UNI_SWITCH_TEST_COLD_ROOT="+root)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	b, err := os.ReadFile(log)
	if err != nil {
		t.Fatal(err)
	}
	var opened []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.Contains(line, root) {
			opened = append(opened, line)
		}
	}
	if len(opened) > 0 {
		t.Fatalf("a stopped distro's files were looked at:\n%s", strings.Join(opened, "\n"))
	}
	if !strings.Contains(string(b), "\nopen ") && !strings.Contains(string(b), "\nstat ") {
		t.Fatalf("the log records nothing, so it shows nothing:\n%.500s", b)
	}
}
