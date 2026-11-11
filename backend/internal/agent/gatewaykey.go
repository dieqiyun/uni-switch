package agent

import (
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
)

// keyAt uses the ordinary local token on loopback and the dedicated WSL
// token on a verified WSL adapter. No sharing credential is generated.
func keyAt(gw string) string {
	if onAnotherMachine(gw) {
		if gateway.OnWSLNet(gw) {
			if k := gateway.WSLKey(); k != "" {
				return k
			}
		}
	}
	return gateway.Token
}

// KeyAt is keyAt, for what else uni-switch gives an agent that reaches the
// gateway at gw (the library's MCP servers relayed through it).
func KeyAt(gw string) string { return keyAt(gw) }

// gwKey is keyAt for the agent at p.
func (p place) gwKey() string { return keyAt(p.gw()) }

// agentKeyAt is keyAt for an agent whose key on this machine names it
// (gateway.TokenFor), for requests that carry nothing else of it.
func agentKeyAt(agent, gw string) string {
	k := keyAt(gw)
	if wk := gateway.WSLKeyMade(); k == wk && wk != "" {
		return wk + "-" + agent // the gateway reads it as TokenFor(agent)
	}
	if k != gateway.Token {
		return k
	}
	return gateway.TokenFor(agent)
}

// hereKey is the ordinary token for the gateway's loopback address.
func hereKey() string { return keyAt(gateway.URL()) }

// agentKey is agentKeyAt for the gateway's own address, as hereKey.
func agentKey(agent string) string { return agentKeyAt(agent, gateway.URL()) }

// ourKey recognizes the ordinary local token and the dedicated WSL token.
func ourKey(k string) bool {
	if k == gateway.Token {
		return true
	}
	return k != "" && k == gateway.WSLKeyMade()
}

// gatewayTakes says the gateway would authenticate a request carrying k: a
// caller key of this uni-switch's own (one issued, or a value the user brought),
// so an agent holding it in its store of its own still goes through uni-switch
// (#1332). A key of another uni-switch's, which this gateway would refuse, is
// not.
func gatewayTakes(k string) bool {
	who, ok := access.Authenticate(k)
	// uni-switch Browser's key reaches no model, so an agent holding it
	// doesn't go through uni-switch
	return ok && !who.Browser
}

// gatewayKey recognizes local and WSL tokens, including per-agent suffixes.
func gatewayKey(want string) bool {
	if ourKey(want) || strings.HasPrefix(want, gateway.Token+"-") && !strings.ContainsAny(want, "/:") {
		return true
	}
	wk := gateway.WSLKeyMade()
	return wk != "" && strings.HasPrefix(want, wk+"-") && !strings.ContainsAny(want, "/:")
}

// keyHolds says the value v an agent's config has where uni-switch wrote want
// still reaches uni-switch: want itself, or, where want is uni-switch's key for its
// gateway, a key of the gateway's own the user put there instead (Gateway
// keys), which the gateway takes just the same (PaulT on Discord: Hermes
// with a key of his own was said not to go through uni-switch).
func keyHolds(v, want string) bool {
	return v == want || v != "" && gatewayKey(want) && gatewayTakes(v)
}
