package agent

import (
	"net"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// fakeWSLNet makes cidr the WSL network Windows has an address on.
func fakeWSLNet(t *testing.T, cidr string) {
	t.Helper()
	ip, n, err := net.ParseCIDR(cidr)
	if err != nil {
		t.Fatal(err)
	}
	n.IP = ip
	old := gateway.WSLNets
	gateway.WSLNets = func() []*net.IPNet { return []*net.IPNet{n} }
	t.Cleanup(func() { gateway.WSLNets = old })
}

// zaidu259434 on X: Codex in WSL (NAT) failed to connect with uni-switch's
// sharing off. At Windows' address on the WSL network, where the gateway
// now listens itself, every agent is given the WSL key, sharing on or off,
// and Codex, signed in or not, has it as its provider's key. Elsewhere off
// loopback it is the LAN key as before, and on loopback the token.
func TestWSLAgentsGetTheWSLKey(t *testing.T) {
	home, read := codexHome(t, `{"OPENAI_API_KEY":"sk-x"}`, "model = \"gpt-5.5\"\n")
	fakeWSLNet(t, "172.20.0.1/20")
	nat := "http://172.20.0.1:" + gateway.Port()
	k := keyAt(nat)
	if k == gateway.Token || k != gateway.WSLKey() || !strings.HasPrefix(k, "wsl-") {
		t.Fatalf("key at the WSL network: %q", k)
	}
	if keyAt(gateway.URL()) != gateway.Token || keyAt("http://192.168.1.5:3425") != gateway.Token {
		t.Error("the WSL key given beyond the WSL network")
	}
	if !ourKey(k) || !gatewayKey(k) || !gatewayKey(agentKeyAt("qoder", nat)) || agentKeyAt("qoder", nat) != k+"-qoder" {
		t.Errorf("WSL keys not known as uni-switch's: %q", agentKeyAt("qoder", nat))
	}
	if oc := uniSwitchProviderJSONAt("opencode", "opencode", nat).(map[string]any)["options"].(map[string]any); oc["apiKey"] != k {
		t.Errorf("OpenCode in WSL: %v", oc["apiKey"])
	}

	a := wslCodex(fakeDistro(home, false))
	if err := a.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	cfg := read()
	if !strings.Contains(cfg, `experimental_bearer_token = "`+k+`"`) {
		t.Fatalf("Codex in WSL not given the WSL key:\n%s", cfg)
	}
	if c := a.Check(); c != "" {
		t.Fatalf("check: %s", c)
	}
}

// What answers at Windows' address on the WSL network is asked from inside
// the distro, never from Windows, where the gateway's own listener answers
// whatever the firewall lets in from WSL: a distro that can't connect is
// told so, with the firewall rule that lets it in; one that can, or can't
// tell (nothing to ask with), isn't. A stopped distro is never run.
func TestWSLReachAskedInsideTheDistro(t *testing.T) {
	home, _ := codexHome(t, `{"OPENAI_API_KEY":"sk-x"}`, "model = \"gpt-5.5\"\n")
	fakeWSLNet(t, "172.20.0.1/20")
	nat := "http://172.20.0.1:" + gateway.Port()
	// from Windows both answer: only the distro's answer can tell
	fakeReach(t, map[string]bool{gateway.URL(): true, nat: true})
	var mu sync.Mutex
	inside, runs := "no", 0
	was := wslRun
	wslRun = func(_ time.Duration, args ...string) ([]byte, error) {
		mu.Lock()
		defer mu.Unlock()
		if slices.Contains(args, wslReachScript) {
			runs++
			if args[1] != "Ubuntu-24.04" || !slices.Contains(args, nat) {
				t.Errorf("asked %v", args)
			}
			return []byte(inside + "\n"), nil
		}
		return nil, nil
	}
	t.Cleanup(func() { wslRun = was })
	d := fakeDistro(home, false)
	a := wslCodex(d)
	if err := a.Fields[0].Set("fake/m1"); err != nil {
		t.Fatal(err)
	}
	dr := driftSettled(t, a)
	if dr == nil || dr.Kind != "unreachable" || dr.Addr != nat || !strings.Contains(dr.Detail, "firewall") || !strings.Contains(dr.Detail, "New-NetFirewallRule") || !strings.Contains(dr.Detail, "-Profile Any") {
		t.Fatalf("blocked from WSL: %+v", dr)
	}
	for _, said := range []string{"ok", "?"} {
		mu.Lock()
		inside = said
		mu.Unlock()
		reachForget()
		if dr := driftSettled(t, a); dr != nil {
			t.Errorf("WSL says %s: %+v", said, dr)
		}
	}
	// stopped: never run to be asked
	mu.Lock()
	inside, runs = "no", 0
	mu.Unlock()
	reachForget()
	d.up = func() bool { return false }
	off := wslCodex(d)
	if off.reachFrom == nil {
		t.Fatal("no probe")
	}
	if _, known := off.reachFrom(nat); known {
		t.Error("a stopped distro's answer known")
	}
	time.Sleep(50 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if runs != 0 {
		t.Errorf("a stopped distro was run %d times", runs)
	}
}
