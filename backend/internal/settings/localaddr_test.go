package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLocalAddr(t *testing.T) {
	for _, addr := range []string{"127.0.0.1:3430", "localhost:3430", "[::1]:3430", "127.0.0.2:0"} {
		if _, err := LocalAddr(addr); err != nil {
			t.Errorf("local address %q: %v", addr, err)
		}
	}
	for _, addr := range []string{"", ":3430", "0.0.0.0:3430", "[::]:3430", "192.168.1.2:3430", "10.0.0.1:3430", "203.0.113.1:3430", "host.example:3430", "[::1%zone]:3430", "127.0.0.1:http", "127.0.0.1:65536", "127.0.0.1:-1"} {
		if got, err := LocalAddr(addr); err == nil {
			t.Errorf("non-local/invalid address accepted: %q -> %q", addr, got)
		}
	}
	if got, _ := LocalAddr("localhost:3430"); got != "127.0.0.1:3430" {
		t.Fatalf("localhost must not use DNS: %q", got)
	}
}

func TestGatewayIgnoresLegacySharingAndNonlocalEnv(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := os.MkdirAll(filepath.Dir(Path()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(Path(), []byte(`{"port":18765,"lan":true,"allowIPs":["0.0.0.0/0"],"lanKey":"legacy-fixture"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, addr := range []string{"", "0.0.0.0:3430", "192.168.1.10:3430", "[::]:3430", "host.example:3430"} {
		t.Setenv("UNI_SWITCH_ADDR", addr)
		if got := GatewayAddr(); got != "127.0.0.1:18765" {
			t.Errorf("legacy sharing or unsafe env changed listener: %q", got)
		}
	}
	t.Setenv("UNI_SWITCH_ADDR", "localhost:18766")
	if got := GatewayAddr(); got != "127.0.0.1:18766" {
		t.Fatal(got)
	}
}
