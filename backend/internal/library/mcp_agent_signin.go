package library

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// An agent can sign in to a remote MCP server itself: Claude Code's /mcp
// does, and keeps the tokens in its own .credentials.json; `codex mcp
// login` does, keeping them in its keyring or, when its config's
// mcp_oauth_credentials_store is file or the keyring is missing, in a
// .credentials.json of its own. uni-switch's check connects without them, so
// such a server answers it 401 and its row said "needs sign-in" while the
// agent used it fine (CyberCaius on Discord: the server signed in to in
// Claude Code in WSL). The check says which agents hold their own sign-in
// instead, from the files alone: the keyrings are never read.
//
// Every target is read where it keeps its files, so an agent in a running
// WSL distro is read under the distro's home (wslTargetOf), as its
// settings are; a stopped distro has no target and is never started for
// this. On a Mac Claude Code keeps the tokens in the Keychain, which uni-switch
// never reads: there only the file, when Claude Code fell back to it.

// ownSignIns are the agents signed in to a server themselves, by
// signInKey of the server's name and URL.
type ownSignIns map[string][]string

func signInKey(name, url string) string {
	return name + "\n" + strings.TrimRight(url, "/")
}

// of are the agents signed in to s on their own; none for a command.
func (o ownSignIns) of(s *Server) []string {
	if s.Transport == "stdio" {
		return nil
	}
	return o[signInKey(s.Name, s.URL)]
}

// claudeCredentials is the part of Claude Code's .credentials.json that
// holds its MCP sign-ins: mcpOAuth, keyed "<server name>|<16 hex of the
// server's type, url and headers>", each entry naming its server and URL
// (Claude Code 2.1.310's td and its token save). An entry with neither
// token is a sign-in started and not finished, which Claude Code itself
// doesn't count as one.
type claudeCredentials struct {
	MCPOAuth map[string]struct {
		ServerName   string `json:"serverName"`
		ServerURL    string `json:"serverUrl"`
		AccessToken  string `json:"accessToken"`
		RefreshToken string `json:"refreshToken"`
	} `json:"mcpOAuth"`
}

// claudeCredentialsOf is Claude Code's .credentials.json for t, beside its
// settings.json (in its config folder, a WSL distro's for one there); ""
// for any other agent.
func claudeCredentialsOf(t *Target) string {
	if id, _, _ := strings.Cut(t.Agent.ID, "@"); id != "claude" || t.Settings == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(t.Settings), ".credentials.json")
}

// codexCredentials is the part of Codex's .credentials.json that holds its
// MCP sign-ins: an entry per server, under the key codex-rs's
// compute_store_key makes of its name and a 16 hex of the hash of its
// config, each entry naming its server and URL with the tokens flattened
// into it (codex-rs's rmcp-client/src/oauth.rs, its FallbackTokenEntry —
// the file holds those, not the StoredOAuthTokens of the login's memory).
// An entry with neither token is a sign-in started and not finished, and
// one of the executor's (executor_owned) is no `codex mcp login`: codex
// itself answers no host's lookup with either.
type codexCredentials map[string]struct {
	ServerName    string `json:"server_name"`
	ServerURL     string `json:"server_url"`
	AccessToken   string `json:"access_token"`
	RefreshToken  string `json:"refresh_token"`
	ExecutorOwned bool   `json:"executor_owned"`
}

// codexCredentialsOf is Codex's .credentials.json for t, beside the
// config.toml the target already has (in CODEX_HOME, a WSL distro's for
// one there), where codex-rs's own read of the file is; "" for any other
// agent.
func codexCredentialsOf(t *Target) string {
	if id, _, _ := strings.Cut(t.Agent.ID, "@"); id != "codex" || t.MCP == nil || t.MCP.Path == "" {
		return ""
	}
	return filepath.Join(filepath.Dir(t.MCP.Path), ".credentials.json")
}

// readOwnSignIns reads each target's own MCP sign-ins. A file that can't
// be read or parsed says nothing of the agent: it is left out, and the
// check's own answer stands.
func readOwnSignIns() ownSignIns {
	out := ownSignIns{}
	for _, t := range Targets() {
		if p := claudeCredentialsOf(t); p != "" {
			out.readClaude(t.Agent.ID, p)
		}
		if p := codexCredentialsOf(t); p != "" {
			out.readCodex(t.Agent.ID, p)
		}
	}
	for _, ids := range out {
		slices.Sort(ids)
	}
	return out
}

// add names agent as signed in to a server called name at url.
func (o ownSignIns) add(agent, name, url string) {
	key := signInKey(name, url)
	if !slices.Contains(o[key], agent) {
		o[key] = append(o[key], agent)
	}
}

// readClaude adds the sign-ins of the Claude Code whose credentials are at
// p: its mcpOAuth entries, the key's prefix naming the server when the
// entry itself doesn't.
func (o ownSignIns) readClaude(agent, p string) {
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var c claudeCredentials
	if json.Unmarshal(b, &c) != nil {
		return
	}
	for k, e := range c.MCPOAuth {
		if e.AccessToken == "" && e.RefreshToken == "" || e.ServerURL == "" {
			continue
		}
		name := e.ServerName
		if name == "" {
			name, _, _ = strings.Cut(k, "|")
		}
		o.add(agent, name, e.ServerURL)
	}
}

// readCodex adds the sign-ins of the Codex whose credentials are at p: its
// file fallback's entries, each naming its server itself; the executor's
// say nothing of a `codex mcp login`.
func (o ownSignIns) readCodex(agent, p string) {
	b, err := os.ReadFile(p)
	if err != nil {
		return
	}
	var c codexCredentials
	if json.Unmarshal(b, &c) != nil {
		return
	}
	for _, e := range c {
		if e.AccessToken == "" && e.RefreshToken == "" || e.ServerURL == "" || e.ExecutorOwned {
			continue
		}
		o.add(agent, e.ServerName, e.ServerURL)
	}
}
