package cli

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"math/big"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// pemPair writes a self-signed certificate and its key into dir.
func pemPair(t *testing.T, dir string) (cert, key string) {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1), Subject: pkix.Name{CommonName: "relay.example"},
		DNSNames:  []string{"relay.example"},
		NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, _ := x509.MarshalECPrivateKey(k)
	cert, key = filepath.Join(dir, "fullchain.pem"), filepath.Join(dir, "privkey.pem")
	os.WriteFile(cert, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600)
	os.WriteFile(key, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600)
	return cert, key
}

// --tls-cert and --tls-key, in either form, set the gateway's files as
// absolute paths (a service starts in another folder) and leave the other
// arguments; one alone, a flag with no file, or files that don't load
// refuse to start.
func TestTLSFlags(t *testing.T) {
	dir := t.TempDir()
	cert, key := pemPair(t, dir)
	t.Chdir(dir)
	for _, args := range [][]string{
		{"--web", "--tls-cert", "fullchain.pem", "--tls-key", "privkey.pem"},
		{"--tls-cert=fullchain.pem", "--web", "--tls-key=privkey.pem"},
	} {
		t.Setenv(gateway.TLSCertEnv, "")
		t.Setenv(gateway.TLSKeyEnv, "")
		rest, err := tlsFlags(args)
		if err != nil || len(rest) != 1 || rest[0] != "--web" {
			t.Fatalf("%v: %v %v", args, rest, err)
		}
		c, k := gateway.TLSFiles()
		want, _ := filepath.EvalSymlinks(cert)
		got, _ := filepath.EvalSymlinks(c)
		if !filepath.IsAbs(c) || !filepath.IsAbs(k) || got != want {
			t.Errorf("%v: files %q %q, want absolute %q", args, c, k, cert)
		}
		if !gateway.TLSOn() || tlsLine() == "" || !strings.Contains(tlsLine(), "relay.example") {
			t.Errorf("%v: TLS not on, or its line %q doesn't name the certificate", args, tlsLine())
		}
	}
	for _, args := range [][]string{
		{"--tls-cert", cert},
		{"--tls-key", key},
		{"--tls-cert", "--tls-key", key},
		{"--tls-cert"},
		{"--tls-cert", key, "--tls-key", key},
		{"--tls-cert", filepath.Join(dir, "gone.pem"), "--tls-key", key},
	} {
		t.Setenv(gateway.TLSCertEnv, "")
		t.Setenv(gateway.TLSKeyEnv, "")
		if _, err := tlsFlags(args); err == nil {
			t.Errorf("%v started", args)
		}
	}
	// neither: plain HTTP as before, and no TLS line
	t.Setenv(gateway.TLSCertEnv, "")
	t.Setenv(gateway.TLSKeyEnv, "")
	if rest, err := tlsFlags([]string{"--web"}); err != nil || len(rest) != 1 || gateway.TLSOn() || tlsLine() != "" {
		t.Errorf("no TLS flags: %v %v, on %v", rest, err, gateway.TLSOn())
	}
}

// serve, web and daemon install refuse half a pair before starting or
// writing anything.
func TestCommandsRefuseHalfAPair(t *testing.T) {
	cert, _ := pemPair(t, t.TempDir())
	for _, f := range []func() error{
		func() error { return serveCmd([]string{"--tls-cert", cert}) },
		func() error { return webCmd([]string{"--tls-cert", cert, "--no-open"}) },
		func() error { return daemonCmd([]string{"install", "--tls-cert", cert}) },
	} {
		t.Setenv(gateway.TLSCertEnv, "")
		t.Setenv(gateway.TLSKeyEnv, "")
		if err := f(); err == nil || !strings.Contains(err.Error(), "--tls-key") {
			t.Errorf("half a pair: %v", err)
		}
	}
}

// The service installed again from a shell that names no certificate
// (uni-switch daemon web on, a new PATH) keeps the one it was installed with;
// --no-tls takes it away.
func TestDaemonReinstallKeepsTLS(t *testing.T) {
	var file string
	switch runtime.GOOS {
	case "darwin":
		file = filepath.Join(os.Getenv("HOME"), "Library", "LaunchAgents", "io.uni-switch.desktop.serve.plist")
	case "linux":
		t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
		file = filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "systemd", "user", "uni-switch.service")
	default:
		t.Skip("no service on " + runtime.GOOS)
	}
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		t.Fatal(err)
	}
	prev, had := os.ReadFile(file)
	t.Cleanup(func() {
		if had == nil {
			os.WriteFile(file, prev, 0o600)
		} else {
			os.Remove(file)
		}
	})
	text := `<key>UNI_SWITCH_TLS_CERT</key><string>/etc/le/full&amp;chain.pem</string>
		<key>UNI_SWITCH_TLS_KEY</key><string>/etc/le/privkey.pem</string>`
	if runtime.GOOS == "linux" {
		text = "Environment=\"UNI_SWITCH_TLS_CERT=/etc/le/full&chain.pem\"\nEnvironment=\"UNI_SWITCH_TLS_KEY=/etc/le/privkey.pem\"\n"
	}
	if err := os.WriteFile(file, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv(gateway.TLSCertEnv, "")
	t.Setenv(gateway.TLSKeyEnv, "")
	keepTLS(false)
	if c, k := gateway.TLSFiles(); c != "/etc/le/full&chain.pem" || k != "/etc/le/privkey.pem" {
		t.Errorf("reinstalled with no certificate named: %q %q, want the service's", c, k)
	}
	t.Setenv(gateway.TLSCertEnv, "/new/cert.pem")
	t.Setenv(gateway.TLSKeyEnv, "/new/key.pem")
	keepTLS(false)
	if c, _ := gateway.TLSFiles(); c != "/new/cert.pem" {
		t.Errorf("the shell's certificate gave way to the service's: %q", c)
	}
	keepTLS(true)
	if gateway.TLSOn() {
		t.Error("--no-tls kept a certificate")
	}
}
