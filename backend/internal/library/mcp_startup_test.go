package library

import (
	"path/filepath"
	"strings"
	"testing"

	toml "github.com/pelletier/go-toml/v2"
)

// codexEntry is the server's entry in a Codex config.toml.
func codexEntry(t *testing.T, path, name string) map[string]any {
	t.Helper()
	var doc map[string]any
	if err := toml.Unmarshal([]byte(read(t, path)), &doc); err != nil {
		t.Fatal(err)
	}
	m, _ := doc["mcp_servers"].(map[string]any)
	e, _ := m[name].(map[string]any)
	return e
}

// A server's startup timeout reaches Codex as startup_timeout_sec, follows
// the library when it changes, and goes when it is taken off; no other
// agent is given it (#3168).
func TestCodexStartupTimeout(t *testing.T) {
	h := sandbox(t)
	cfg := filepath.Join(h, ".codex/config.toml")
	srv := Server{Name: "slow", Transport: "stdio", Command: "npx", Args: []string{"-y", "slow-mcp"}, Agents: []string{"claude", "codex"}}

	ok(t)(SaveServer("", srv))
	if e := codexEntry(t, cfg, "slow"); e == nil || e["startup_timeout_sec"] != nil {
		t.Fatalf("no timeout set, yet Codex has one:\n%s", read(t, cfg))
	}

	for _, n := range []int{120, 45} {
		srv.StartupTimeout = n
		ok(t)(SaveServer("slow", srv))
		if e := codexEntry(t, cfg, "slow"); !isNumber(e["startup_timeout_sec"], float64(n)) {
			t.Fatalf("startup_timeout_sec = %v, want %d:\n%s", e["startup_timeout_sec"], n, read(t, cfg))
		}
	}
	if s := read(t, filepath.Join(h, ".claude.json")); strings.Contains(s, "startup") || strings.Contains(s, "45") {
		t.Errorf("Claude Code was given the startup timeout:\n%s", s)
	}

	srv.StartupTimeout = 0
	ok(t)(SaveServer("slow", srv))
	if e := codexEntry(t, cfg, "slow"); e == nil || e["startup_timeout_sec"] != nil {
		t.Errorf("the timeout taken off stays in Codex:\n%s", read(t, cfg))
	}

	srv.StartupTimeout = -1
	if _, err := SaveServer("slow", srv); err == nil {
		t.Error("a negative startup timeout was saved")
	}
	srv.StartupTimeout = maxStartupTimeout + 1
	if _, err := SaveServer("slow", srv); err == nil {
		t.Error("a startup timeout over a day was saved")
	}
}

// A startup timeout the user wrote in Codex's config.toml by hand is never
// written over: one there before uni-switch had any, one changed by hand after
// uni-switch wrote its own, and Codex's older startup_timeout_ms. Once the
// user's is taken out, uni-switch's goes in.
func TestCodexStartupTimeoutKeepsUsers(t *testing.T) {
	h := sandbox(t)
	cfg := filepath.Join(h, ".codex/config.toml")
	srv := Server{Name: "slow", Transport: "stdio", Command: "npx", Agents: []string{"codex"}}
	ok(t)(SaveServer("", srv))
	hand := func(from, to string) {
		t.Helper()
		s := read(t, cfg)
		if !strings.Contains(s, from) {
			t.Fatalf("no %q in:\n%s", from, s)
		}
		write(t, cfg, strings.Replace(s, from, to, 1))
	}
	want := func(key string, n float64) {
		t.Helper()
		e := codexEntry(t, cfg, "slow")
		if !isNumber(e[key], n) {
			t.Fatalf("%s = %v, want %v:\n%s", key, e[key], n, read(t, cfg))
		}
	}

	// the user's before uni-switch had one
	hand("command = ", "startup_timeout_sec = 90\ncommand = ")
	srv.StartupTimeout = 20
	ok(t)(SaveServer("slow", srv))
	want("startup_timeout_sec", 90)
	if n := strings.Count(read(t, cfg), "startup_timeout_sec"); n != 1 {
		t.Fatalf("%d startup_timeout_sec:\n%s", n, read(t, cfg))
	}

	// taken out by hand: uni-switch's goes in, and follows the library
	hand("startup_timeout_sec = 90\n", "")
	ok(t)(Sync())
	want("startup_timeout_sec", 20)
	srv.StartupTimeout = 25
	ok(t)(SaveServer("slow", srv))
	want("startup_timeout_sec", 25)

	// uni-switch's changed by hand is the user's from then on
	hand("startup_timeout_sec = 25", "startup_timeout_sec = 75")
	srv.StartupTimeout = 30
	ok(t)(SaveServer("slow", srv))
	want("startup_timeout_sec", 75)
	srv.StartupTimeout = 0
	ok(t)(SaveServer("slow", srv))
	want("startup_timeout_sec", 75)

	// Codex reads startup_timeout_ms only without _sec: uni-switch writes no
	// _sec beside the user's _ms
	hand("startup_timeout_sec = 75", "startup_timeout_ms = 40000")
	srv.StartupTimeout = 20
	ok(t)(SaveServer("slow", srv))
	e := codexEntry(t, cfg, "slow")
	if e["startup_timeout_sec"] != nil || !isNumber(e["startup_timeout_ms"], 40000) {
		t.Errorf("the user's startup_timeout_ms isn't what Codex reads:\n%s", read(t, cfg))
	}
}

// A project's .codex/config.toml is given the startup timeout too, and
// follows it when it changes.
func TestCodexProjectStartupTimeout(t *testing.T) {
	sandbox(t)
	proj := filepath.Join(t.TempDir(), "app")
	write(t, filepath.Join(proj, "README"), "app\n")
	ok(t)(AddProject(proj))
	srv := Server{Name: "slow", Transport: "stdio", Command: "npx", StartupTimeout: 60}
	ok(t)(SaveServer("", srv))
	ok(t)(ProjectServer(proj, "slow", []string{"codex"}))
	cfg := filepath.Join(proj, ".codex/config.toml")
	if e := codexEntry(t, cfg, "slow"); !isNumber(e["startup_timeout_sec"], 60) {
		t.Fatalf("project:\n%s", read(t, cfg))
	}
	srv.StartupTimeout = 15
	ok(t)(SaveServer("slow", srv))
	if e := codexEntry(t, cfg, "slow"); !isNumber(e["startup_timeout_sec"], 15) {
		t.Fatalf("project didn't follow the library:\n%s", read(t, cfg))
	}
	write(t, cfg, strings.Replace(read(t, cfg), "startup_timeout_sec = 15", "startup_timeout_sec = 300", 1))
	srv.StartupTimeout = 10
	ok(t)(SaveServer("slow", srv))
	if e := codexEntry(t, cfg, "slow"); !isNumber(e["startup_timeout_sec"], 300) {
		t.Fatalf("project's hand-written timeout was written over:\n%s", read(t, cfg))
	}
}
