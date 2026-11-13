package claudebridge

import (
	"bufio"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// ownIPv4 is an IPv4 address of one of this computer's interfaces that
// isn't loopback, "" when it has none.
func ownIPv4() string {
	ifs, _ := net.Interfaces()
	for _, i := range ifs {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := i.Addrs()
		for _, a := range addrs {
			if n, ok := a.(*net.IPNet); ok && n.IP.To4() != nil && !n.IP.IsLoopback() && !n.IP.IsLinkLocalUnicast() {
				return n.IP.String()
			}
		}
	}
	return ""
}

// #2440 (ump45nose): an agent hands the helper the *_PROXY uni-switch gave it
// for its vendor (the system's proxy, by default), and a gateway on a LAN
// or Tailscale address isn't in that NO_PROXY. The helper's callback went
// to the proxy, which reset it, and the tool call never reached the run.
// Here the helper runs as its own process, as an agent starts it, every
// proxy variable on a proxy that resets each connection, and calls back
// to this computer's own non-loopback address.
func TestCallbackGoesPastTheAgentsProxy(t *testing.T) {
	ip := ownIPv4()
	if ip == "" {
		t.Skip("this computer has no address beyond loopback")
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(ip, "0"))
	if err != nil {
		t.Skip("can't listen on", ip, err)
	}
	cb := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"content": []map[string]any{{"type": "text", "text": "file body"}}})
	}))
	cb.Listener.Close()
	cb.Listener = ln
	cb.Start()
	t.Cleanup(cb.Close)

	proxy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { proxy.Close() })
	proxied := make(chan struct{}, 8)
	go func() {
		for {
			c, err := proxy.Accept()
			if err != nil {
				return
			}
			proxied <- struct{}{}
			c.(*net.TCPConn).SetLinger(0)
			c.Close()
		}
	}()

	dir := t.TempDir()
	tools := filepath.Join(dir, "tools.json")
	if err := os.WriteFile(tools, []byte(`[{"name":"read","inputSchema":{"type":"object"}}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	p := "http://" + proxy.Addr().String()
	cmd := exec.CommandContext(t.Context(), os.Args[0])
	cmd.Env = append(os.Environ(), helperEnv+"=1",
		"UNI_SWITCH_MCP_CALLBACK="+cb.URL+"/_uni-switch/claude-mcp/tok", "UNI_SWITCH_MCP_TOOLS="+tools,
		"HTTPS_PROXY="+p, "https_proxy="+p, "HTTP_PROXY="+p, "http_proxy="+p,
		"ALL_PROXY="+p, "all_proxy="+p, "NO_PROXY=localhost,127.0.0.1,::1", "no_proxy=localhost,127.0.0.1,::1")
	in, _ := cmd.StdinPipe()
	out, _ := cmd.StdoutPipe()
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { in.Close(); cmd.Wait() })
	in.Write([]byte(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"read","arguments":{},"_meta":{"claudecode/toolUseId":"toolu_1"}}}` + "\n"))
	line := make(chan string, 1)
	go func() {
		sc := bufio.NewScanner(out)
		sc.Buffer(make([]byte, 64<<10), 1<<20)
		if sc.Scan() {
			line <- sc.Text()
		}
		close(line)
	}()
	var got string
	select {
	case got = <-line:
	case <-time.After(20 * time.Second):
		t.Fatal("no answer from the helper")
	}
	if !strings.Contains(got, "file body") {
		t.Fatalf("the call didn't reach uni-switch: %s", got)
	}
	select {
	case <-proxied:
		t.Fatal("the callback went to the agent's proxy")
	default:
	}
}
