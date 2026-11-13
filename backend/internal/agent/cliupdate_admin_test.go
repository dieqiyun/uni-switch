package agent

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/update"
)

// On Windows npm's global folder may be Node.js's own, C:\Program
// Files\nodejs, which only an administrator can change (新v学员 on
// Discord): uni-switch runs as the user, so npm install -g there failed with
// EPERM. The update is still offered, says it asks for an administrator,
// and runs through Windows' prompt (runAdmin), never as the user; a prompt
// declined or not to be had says why, with what to do. Elsewhere a global
// folder the user can't write in (/usr/lib) still offers nothing: npm
// would want sudo.
func TestNPMUpdateNeedsAdmin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("a shell script as the CLI")
	}
	tmp, _ := filepath.EvalSymlinks(t.TempDir())
	prefix := filepath.Join(tmp, "node")
	verFile := filepath.Join(tmp, "version")
	os.WriteFile(verFile, []byte("0.60.0\n"), 0o644)
	file(t, filepath.Join(prefix, "lib/node_modules/@google/gemini-cli/bundle/gemini.js"), "#!/bin/sh\n/bin/cat "+verFile+"\n")
	file(t, filepath.Join(prefix, "bin/npm"), "#!/bin/sh\nexit 1\n")
	link(t, "../lib/node_modules/@google/gemini-cli/bundle/gemini.js", filepath.Join(prefix, "bin/gemini"))
	t.Setenv("PATH", filepath.Join(prefix, "bin"))
	reg := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"name":"@google/gemini-cli","version":"0.61.0"}`))
	}))
	defer reg.Close()

	oldReg, oldRun, oldAdmin, oldWrite, oldNPM := npmRegistry, runUpdate, runAdmin, canWrite, adminNPM
	t.Cleanup(func() {
		npmRegistry, runUpdate, runAdmin, canWrite, adminNPM = oldReg, oldRun, oldAdmin, oldWrite, oldNPM
		versions, latests = memo{}, memo{}
	})
	npmRegistry = reg.URL + "/"
	versions, latests = memo{}, memo{}
	modules := filepath.Join(prefix, "lib", "node_modules")
	canWrite = func(dir string) bool { return dir != modules }
	runUpdate = func(context.Context, *updater) ([]byte, error) {
		t.Error("the update ran as the user, in a folder only an administrator can change")
		return nil, errors.New("EPERM")
	}
	var asAdmin [][]string
	answer := func(u *updater) ([]byte, error) {
		os.WriteFile(verFile, []byte("0.61.0\n"), 0o644)
		return []byte("changed 1 package"), nil
	}
	runAdmin = func(_ context.Context, u *updater) ([]byte, error) {
		asAdmin = append(asAdmin, u.cmd)
		return answer(u)
	}
	a := &Agent{ID: "gemini", Name: "Gemini CLI", Bin: "gemini"}

	// not on Windows: nothing offered, as before
	adminNPM = false
	if c, _ := a.CLI(); c.Update || c.Via != "" {
		t.Fatalf("a folder needing sudo offered an update: %+v", c)
	}

	adminNPM = true
	c, _ := a.CLI()
	if !c.Update || c.Via != "npm" || !c.Admin {
		t.Fatalf("Program Files' npm: %+v, want an update as administrator", c)
	}

	// the prompt declined: npm never ran, and the reason names the folder
	answer = func(*updater) ([]byte, error) { return nil, update.ErrCanceled }
	_, err := a.UpdateCLI()
	var ae *AdminError
	if !errors.As(err, &ae) || !ae.Canceled || ae.Prefix != prefix || ae.Agent != "Gemini CLI" || !strings.Contains(err.Error(), prefix) || !strings.Contains(err.Error(), "npm config set prefix") {
		t.Fatalf("declined: %v", err)
	}
	// no prompt to be had
	answer = func(*updater) ([]byte, error) {
		return nil, fmt.Errorf("%w: the service isn't running", update.ErrNoAdmin)
	}
	if _, err = a.UpdateCLI(); !errors.As(err, &ae) || ae.Canceled || !strings.Contains(err.Error(), "the service isn't running") {
		t.Fatalf("no prompt: %v", err)
	}
	// npm itself failing as administrator says npm's own words
	answer = func(*updater) ([]byte, error) { return []byte("npm error code ETARGET"), errors.New("exit status 1") }
	if _, err = a.UpdateCLI(); err == nil || errors.As(err, &ae) || !strings.Contains(err.Error(), "ETARGET") {
		t.Fatalf("npm failing: %v", err)
	}

	// allowed: updated
	answer = func(*updater) ([]byte, error) {
		os.WriteFile(verFile, []byte("0.61.0\n"), 0o644)
		return []byte("changed 1 package"), nil
	}
	c, err = a.UpdateCLI()
	if err != nil || c.Version != "0.61.0" || c.Update {
		t.Fatalf("allowed: %+v, %v", c, err)
	}
	want := "install -g --prefix " + prefix + " @google/gemini-cli@latest"
	if len(asAdmin) != 4 || !strings.HasSuffix(strings.Join(asAdmin[3], " "), want) {
		t.Errorf("ran as administrator: %q", asAdmin)
	}
}

// Install another agent's npm commands say when npm's global folder is
// one only an administrator can change, and only then.
func TestInstallNPMNeedsAdmin(t *testing.T) {
	oldWrite := canWrite
	t.Cleanup(func() { canWrite = oldWrite })
	canWrite = func(dir string) bool { return dir != `C:\Program Files\nodejs` }
	mk := func() []Install {
		return []Install{{ID: "gemini", Commands: []InstallCmd{{Via: "brew", Command: "brew install gemini-cli"}, {Via: "npm", Command: "npm install -g @google/gemini-cli"}}}}
	}
	out := mk()
	npmNeedsAdmin(out, `C:\Program Files\nodejs`)
	if out[0].Commands[1].Admin != `C:\Program Files\nodejs` || out[0].Commands[0].Admin != "" {
		t.Errorf("Program Files: %+v", out[0].Commands)
	}
	for _, p := range []string{`C:\Users\me\AppData\Roaming\npm`, ""} {
		out = mk()
		npmNeedsAdmin(out, p)
		if out[0].Commands[1].Admin != "" {
			t.Errorf("%q: %+v", p, out[0].Commands)
		}
	}
}
