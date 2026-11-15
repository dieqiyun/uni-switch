package gateway

import (
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"io"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// writeCert writes a self-signed certificate for name, as certbot's
// fullchain.pem and privkey.pem would be, and returns its pool.
func writeCert(t *testing.T, certFile, keyFile, name string) *x509.CertPool {
	t.Helper()
	k, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(time.Now().UnixNano()),
		Subject:      pkix.Name{CommonName: name},
		DNSNames:     []string{name},
		IPAddresses:  []net.IP{net.ParseIP("127.0.0.1")},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		IsCA:         true, BasicConstraintsValid: true,
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &k.PublicKey, k)
	if err != nil {
		t.Fatal(err)
	}
	kb, err := x509.MarshalECPrivateKey(k)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(certFile, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "EC PRIVATE KEY", Bytes: kb}), 0o600); err != nil {
		t.Fatal(err)
	}
	c, _ := x509.ParseCertificate(der)
	pool := x509.NewCertPool()
	pool.AddCert(c)
	return pool
}

// serveTLSGateway serves the gateway as ListenAndServe does — lanGuard
// in front, on the listener Server.listen makes — with the TLS files the
// environment names, on loopback; seen from other computers says whether
// a connection counts as another computer's.
func serveTLSGateway(t *testing.T, other bool) (addr string) {
	t.Helper()
	was := fromThisComputer
	fromThisComputer = func(net.Conn) bool { return !other }
	t.Cleanup(func() { fromThisComputer = was })
	cfg, err := tlsConfig()
	if err != nil || cfg == nil {
		t.Fatalf("tlsConfig: %v, %v", cfg, err)
	}
	s := New()
	s.tls = cfg
	t.Setenv("UNI_SWITCH_ADDR", "127.0.0.1:0")
	ln, err := s.listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: localGuard(s.Handler()), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() { defer close(done); srv.Serve(ln) }()
	t.Cleanup(func() { srv.Close(); <-done })
	return ln.Addr().String()
}

func tlsFiles(t *testing.T) (cert, key string) {
	d := t.TempDir()
	cert, key = filepath.Join(d, "fullchain.pem"), filepath.Join(d, "privkey.pem")
	t.Setenv(TLSCertEnv, cert)
	t.Setenv(TLSKeyEnv, key)
	return cert, key
}

func get(t *testing.T, c *http.Client, url string) (int, string, error) {
	t.Helper()
	res, err := c.Get(url)
	if err != nil {
		return 0, "", err
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, string(b), nil
}

func tlsClient(pool *x509.CertPool) *http.Client {
	return &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{
		TLSClientConfig:   &tls.Config{RootCAs: pool, ServerName: "relay.example"},
		DisableKeepAlives: true,
	}}
}

var plain = &http.Client{Timeout: 5 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}

// Another computer reaches the gateway over TLS alone: plain HTTP is
// turned away, HTTPS answers /v1/models with the certificate's chain.
func TestTLSOtherComputersOnlyOverHTTPS(t *testing.T) {
	cert, key := tlsFiles(t)
	pool := writeCert(t, cert, key, "relay.example")
	addr := serveTLSGateway(t, true)

	code, body, err := get(t, plain, "http://"+addr+"/v1/models")
	if err == nil && code != http.StatusBadRequest {
		t.Fatalf("plain HTTP from another computer: %d %s, want it refused", code, body)
	}
	if err == nil && !strings.Contains(body, "HTTPS") {
		t.Errorf("plain HTTP refused with %q, which doesn't say to use HTTPS", body)
	}
	code, body, err = get(t, tlsClient(pool), "https://"+addr+"/v1/models")
	if err != nil || code != http.StatusOK || !strings.Contains(body, `"data"`) {
		t.Fatalf("HTTPS /v1/models: %d %s %v", code, body, err)
	}
}

// This computer's agents, MCP servers and status checks call plain
// http://127.0.0.1 with TLS on, and HTTPS works there too.
func TestTLSThisComputerTakesBoth(t *testing.T) {
	cert, key := tlsFiles(t)
	pool := writeCert(t, cert, key, "relay.example")
	addr := serveTLSGateway(t, false)

	if code, body, err := get(t, plain, "http://"+addr+"/v1/models"); err != nil || code != http.StatusOK {
		t.Fatalf("plain HTTP from this computer: %d %s %v", code, body, err)
	}
	if code, body, err := get(t, tlsClient(pool), "https://"+addr+"/v1/models"); err != nil || code != http.StatusOK {
		t.Fatalf("HTTPS from this computer: %d %s %v", code, body, err)
	}
}

// A client here that connects and says nothing (a browser's preconnect)
// doesn't hold up the connections after it while its first byte, which
// says TLS or not, is waited for.
func TestTLSSilentLocalClientHoldsNoOther(t *testing.T) {
	cert, key := tlsFiles(t)
	writeCert(t, cert, key, "relay.example")
	addr := serveTLSGateway(t, false)

	silent, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer silent.Close()
	time.Sleep(50 * time.Millisecond) // it is the one accepted first
	c := &http.Client{Timeout: sniffWait / 2, Transport: &http.Transport{DisableKeepAlives: true}}
	if code, body, err := get(t, c, "http://"+addr+"/v1/models"); err != nil || code != http.StatusOK {
		t.Fatalf("a request behind a silent connection: %d %s %v", code, body, err)
	}
}

// A renewal (certbot writing new files) is served on the next connection
// without a restart; files half written keep the certificate before.
func TestTLSServesRenewedCertificate(t *testing.T) {
	cert, key := tlsFiles(t)
	poolA := writeCert(t, cert, key, "relay.example")
	addr := serveTLSGateway(t, true)

	served := func(pool *x509.CertPool) error {
		_, _, err := get(t, tlsClient(pool), "https://"+addr+"/v1/models")
		return err
	}
	if err := served(poolA); err != nil {
		t.Fatalf("first certificate: %v", err)
	}
	poolB := writeCert(t, cert, key, "relay.example")
	later := time.Now().Add(time.Minute)
	os.Chtimes(cert, later, later)
	os.Chtimes(key, later, later)
	if err := served(poolB); err != nil {
		t.Fatalf("renewed certificate not served: %v", err)
	}
	if served(poolA) == nil {
		t.Fatal("the certificate before the renewal is still served")
	}
	// a renewal half written: the key changed, the certificate not yet
	os.WriteFile(key, []byte("not a key"), 0o600)
	if err := served(poolB); err != nil {
		t.Fatalf("a pair that doesn't load took the served certificate away: %v", err)
	}
}

// Only one of the two files, or files that aren't a certificate and its
// key, refuse to start, saying which.
func TestCheckTLS(t *testing.T) {
	cert, key := tlsFiles(t)
	writeCert(t, cert, key, "relay.example")
	if err := CheckTLS(); err != nil || !TLSOn() {
		t.Fatalf("a good pair: %v, on %v", err, TLSOn())
	}
	t.Setenv(TLSKeyEnv, "")
	if err := CheckTLS(); err == nil || !strings.Contains(err.Error(), "--tls-key") {
		t.Errorf("certificate alone: %v", err)
	}
	t.Setenv(TLSKeyEnv, key)
	t.Setenv(TLSCertEnv, "")
	if err := CheckTLS(); err == nil || !strings.Contains(err.Error(), "--tls-cert") {
		t.Errorf("key alone: %v", err)
	}
	t.Setenv(TLSCertEnv, key) // a key where the certificate goes
	if err := CheckTLS(); err == nil {
		t.Error("a key as the certificate loaded")
	}
	if _, err := tlsConfig(); err == nil {
		t.Error("tlsConfig served a pair that doesn't load")
	}
	t.Setenv(TLSCertEnv, "")
	t.Setenv(TLSKeyEnv, "")
	if err := CheckTLS(); err != nil || TLSOn() {
		t.Errorf("neither: %v, on %v", err, TLSOn())
	}
	if cfg, err := tlsConfig(); cfg != nil || err != nil {
		t.Errorf("neither: %v %v", cfg, err)
	}
}

// Moving the gateway (sharing turned on, another port) keeps TLS.
func TestTLSRelistenKeepsTLS(t *testing.T) {
	cert, key := tlsFiles(t)
	pool := writeCert(t, cert, key, "relay.example")
	cfg, err := tlsConfig()
	if err != nil {
		t.Fatal(err)
	}
	was := fromThisComputer
	fromThisComputer = func(net.Conn) bool { return false }
	t.Cleanup(func() { fromThisComputer = was })
	s := New()
	s.tls = cfg
	first, err := s.listen("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.ln = first
	free, _ := net.Listen("tcp", "127.0.0.1:0")
	to := free.Addr().String()
	free.Close()
	t.Setenv("UNI_SWITCH_ADDR", to)
	if err := s.Relisten(); err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: localGuard(s.Handler()), ReadHeaderTimeout: 5 * time.Second}
	done := make(chan struct{})
	go func() { defer close(done); srv.Serve(s.ln) }()
	defer func() { srv.Close(); <-done }()
	if code, _, err := get(t, plain, "http://"+to+"/v1/models"); err == nil && code != http.StatusBadRequest {
		t.Errorf("plain HTTP after moving: %d", code)
	}
	if code, body, err := get(t, tlsClient(pool), "https://"+to+"/v1/models"); err != nil || code != http.StatusOK {
		t.Errorf("HTTPS after moving: %d %s %v", code, body, err)
	}
}
