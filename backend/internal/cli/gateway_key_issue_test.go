package cli

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// uni-switch gateway-key issue prints a short-lived key alone, for a script
// that hands it to a sandbox; --from holds it to a key let issue keys.
func TestGatewayKeyIssueCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	if err := settings.Save(settings.Settings{}); err != nil {
		t.Fatal(err)
	}
	call := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := gatewayKeysTo(&out, append([]string{"gateway-key"}, args...))
		return strings.TrimSpace(out.String()), err
	}
	secret, err := call("issue", "--ttl", "1h", "--name", "sandbox-1", "--models", "openai/gpt-5, openai/gpt-5-mini", "--tokens", "50000")
	if err != nil || !strings.HasPrefix(secret, access.Prefix) || strings.Contains(secret, "\n") {
		t.Fatal(secret, err)
	}
	who, ok := access.Authenticate(secret)
	if !ok || who.KeyName != "sandbox-1" || len(who.Models) != 2 || who.Limit == nil || who.Limit.Tokens != 50000 || time.Until(who.Expires) > time.Hour {
		t.Fatal(who, ok)
	}
	if got, _ := call("list"); !strings.Contains(got, "until ") {
		t.Fatal("list:", got)
	}
	for _, bad := range [][]string{{"issue"}, {"issue", "--ttl"}, {"issue", "--ttl", "soon"}, {"issue", "--ttl", "10s"}, {"issue", "--ttl", "1h", "--tokens", "-3"},
		{"issue", "--ttl", "1h", "--what"}, {"issue", "--help"}, {"issuer"}, {"issuer", "x", "maybe"}} {
		if _, err := call(bad...); err == nil {
			t.Error("accepted", bad)
		}
	}
	parent, _ := call("add", "Orchestrator")
	keys, _ := access.List()
	var pid string
	for _, k := range keys {
		if k.Name == "Orchestrator" {
			pid = k.ID
		}
	}
	if _, err := call("issue", "--ttl", "1h", "--from", pid); err == nil {
		t.Fatal("issued under a key not let issue")
	}
	if got, err := call("issuer", pid, "on"); err != nil || !strings.Contains(got, "may issue") {
		t.Fatal(got, err)
	}
	if who, _ := access.Authenticate(parent); !who.Issues {
		t.Fatal("not let issue:", who)
	}
	out, err := call("issue", "--ttl", "30m", "--from", pid, "--read-only", "--json")
	var k access.Issued
	if err != nil || json.Unmarshal([]byte(out), &k) != nil || k.Issuer != pid || !k.ReadOnly || k.Key == "" {
		t.Fatal(out, err)
	}
	if got, _ := call("list"); !strings.Contains(got, "issued by "+pid) || !strings.Contains(got, "issues keys") {
		t.Fatal("list:", got)
	}
	if got, err := call("issuer", pid, "off"); err != nil || !strings.Contains(got, "issues no keys") {
		t.Fatal(got, err)
	}
	if help := helpFor([]string{"gateway-key", "--help"}); !strings.Contains(help, "gateway-key issue --ttl") || !strings.Contains(help, "gateway-key issuer <id>") {
		t.Fatal("help:", help)
	}
}

// uni-switch gateway-key oidc adds, lists and removes the rules a CI job's
// token is traded by; a rule open to every repository is refused.
func TestGatewayKeyOIDCCLI(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	call := func(args ...string) (string, error) {
		var out bytes.Buffer
		err := gatewayKeysTo(&out, append([]string{"gateway-key"}, args...))
		return strings.TrimSpace(out.String()), err
	}
	call("add", "CI")
	keys, _ := access.List()
	id := keys[0].ID
	if _, err := call("oidc", "add", "--key", id, "--repo", "acme/app"); err == nil || !strings.Contains(err.Error(), "issuer "+id+" on") {
		t.Fatal("a key not let issue:", err)
	}
	call("issuer", id, "on")
	for _, bad := range [][]string{{"oidc", "add", "--key", id, "--ref", "refs/heads/main"}, {"oidc", "add", "--key", id, "--owner", "*"},
		{"oidc", "add", "--repo", "acme/app"}, {"oidc", "add", "--key", id, "--repo"}, {"oidc", "add", "--key", id, "--repo", "acme/app", "--ttl", "soon"},
		{"oidc", "add", "--key", id, "--repo", "acme/app", "--ttl", "100ms"}, {"oidc", "add", "--key", id, "--what", "x"}, {"oidc", "remove"}, {"oidc", "maybe"}} {
		if _, err := call(bad...); err == nil {
			t.Error("accepted", bad)
		}
	}
	got, err := call("oidc", "add", "--key", id, "--repo", "acme/app", "--environment", "prod", "--ttl", "30m")
	if err != nil || !strings.Contains(got, "audience uni-switch-") || !strings.Contains(got, "30m0s") {
		t.Fatal(got, err)
	}
	list, _ := call("oidc", "list")
	if !strings.Contains(list, "environment=prod repository=acme/app") || !strings.Contains(list, "token.actions.githubusercontent.com") {
		t.Fatal("list:", list)
	}
	ruleID := strings.Fields(strings.Split(list, "\n")[1])[0]
	if _, err := call("oidc", "remove", ruleID); err != nil {
		t.Fatal(err)
	}
	if list, _ := call("oidc"); !strings.Contains(list, "No OIDC rules") {
		t.Fatal("after remove:", list)
	}
	if help := helpFor([]string{"gateway-key", "--help"}); !strings.Contains(help, "gateway-key oidc add") {
		t.Fatal("help:", help)
	}
}
