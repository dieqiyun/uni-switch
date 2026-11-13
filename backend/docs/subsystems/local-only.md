# Local-only networking

uni-switch no longer shares its gateway or management page over a LAN.
This contract supersedes the imported backend's network-hosting examples.

The remote gateway provider is removed in both directions: no addable preset,
neighbour-address scanning, remote model following, quota/reset management,
forwarded caller attribution or automatic MCP router bridge. Legacy
`remote-magpie` and `remote-uni-switch` provider records stay on disk but are
excluded from active providers, and saves/imports cannot reactivate them.
Ordinary explicit outbound API endpoints remain supported.

## Boundaries and source of truth

- [`settings.LocalAddr`](../../internal/settings/localaddr.go) accepts only
  literal loopback IPs or `localhost`, normalized without DNS. Wildcards,
  private/public addresses, arbitrary names and zone IDs are rejected.
- `GatewayAddr` ignores an unsafe `UNI_SWITCH_ADDR` and uses the saved
  local port. Explicit `web` and daemon addresses fail validation before
  binding or writing service files.
- `gateway.Listen` validates before inherited listeners, reuse or a new
  TCP bind. TLS never expands access.
- [`localGuard`](../../internal/gateway/local.go) refuses non-local,
  forwarded and management-page-port inference with 403 before callbacks,
  named-key attribution and inference handling. A valid key cannot bypass
  this boundary. Forwarding headers do not become trusted client addresses.
- `LocalOnly` guards the browser server before its run-key/cookie
  authentication. Its API still has same-origin protection. Browser access
  offers Off or This computer only.

The dedicated Windows WSL adapter listener and its key remain for local WSL
clients. Its address/network/key checks are distinct from general LAN sharing;
they do not authorize other interfaces or ordinary remote requests. Local
clients may still call external providers, including private-address servers.
That outbound capability is unrelated to publishing this gateway.

## State and compatibility

LAN toggles, IP allow lists, shared/public address discovery, proxy trust,
their management APIs and CLI switches are removed. Gateway and Settings JSON
no longer advertise those fields. Environment variables cannot re-enable
sharing, public-address discovery or forwarded-client trust.

Old files are not deleted. A legacy `web-page.json` with `mode: "lan"`
starts no page and is left intact until the user makes a valid new choice.
Legacy default-key identity/value fields are read for named-key and client
recovery compatibility only; no key creates or opens a listener. Local named
keys remain available for attribution, model/account restrictions and limits.
The ordinary loopback token retains the existing local trust model.

Local named browser keys keep their identity even when the extension connects
to a literal loopback host, so rotation, revocation and browser-session
isolation continue to work. Downloaded browser extensions use the local
inference gateway URL; a local management-page origin is never used as the
extension's WebSocket endpoint.

The management port is not an inference endpoint. Existing client-path
dispatch marks a request before passing it to the gateway, where it is
refused. Agents and SDKs use the separate local gateway URL shown in Connect.

## Verification

Run from the repository root:

```sh
corepack pnpm test
node scripts/qa-source-local-only.mjs
node scripts/test-go.mjs --log .qa/local-only.log -tags=nogui,uniswitchqa -run 'Test(Local|GatewayAddr|Legacy|KeyPage|NetworkGatewayGuardsEveryRoute|WSLAgentAddressNotAnswering|ReadOnlyKey)' ./internal/settings ./internal/gateway ./internal/gui ./internal/agent ./internal/access
```

The compiled UI check uses synthetic credentials and an isolated home. It
covers Chromium/WebKit and English, Simplified/Traditional Chinese, Japanese
and German. `local_only_test.go`, `localaddr_test.go` and
`uni_switch_local_only_test.go` cover remote peers, old environment/configuration,
forwarded calls, listener validation and local behavior. These checks do not
constitute Linux/macOS/WSL native runtime acceptance or a complete protocol suite.

Gateway status, local address, key management and manual connection examples are presented in Settings > Gateway. Port and bare-model-ID configuration are inside its Advanced configuration fold. Browser access and CORS remain under Network.
