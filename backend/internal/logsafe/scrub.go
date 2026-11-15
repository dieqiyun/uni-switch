// Package logsafe removes credentials from local diagnostics and saved records.
// It never changes a request sent to a provider or a reply sent to a client.
package logsafe

import (
	"crypto/sha256"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
)

// Diagnostic copies discard credentials permanently. No values are held
// for restoration, and no configurable masking settings are consulted.

// Scrubbed is what a field or header named for a secret holds once scrubbed.
const Scrubbed = "[REDACTED]"

// secretName is a field's or header's name that says its value is a secret.
// A name that ends in pass (a db_pass) is one too (#2946); a name that only
// ends in key is not — a sort key, a cache key, a public key hold no secret.
var secretName = regexp.MustCompile(`(?i)` + `password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential` + `|authori[sz]ation|cookie|^key$|^pass$|_pass$`)

// SecretName says a field or header called name holds a secret: a token,
// key, password or cookie — not a count of tokens (max_tokens).
func SecretName(name string) bool {
	n := strings.ToLower(name)
	return secretName.MatchString(n) && !strings.HasSuffix(n, "tokens") && !strings.HasSuffix(n, "token_count")
}

// Scrub removes credential-shaped values from diagnostic text.
func Scrub(s string) string {
	if len(s) < scrubMemoMin {
		return scrub(s)
	}
	gen := uint64(0)
	k := sha256.Sum256([]byte(s))
	if out, ok := scrubMemo.get(k, gen, s); ok {
		return out
	}
	out := scrub(s)
	scrubMemo.put(k, gen, s, out)
	return out
}

func scrub(s string) string {
	if len(s) >= 256 {
		scrubRuns.Add(1)
	}
	for _, rule := range credentials {
		matches := rule.re.FindAllStringSubmatchIndex(s, -1)
		for i := len(matches) - 1; i >= 0; i-- {
			m := matches[i]
			a, b := m[2*rule.group], m[2*rule.group+1]
			if a >= 0 {
				s = s[:a] + "[REDACTED:" + rule.kind + "]" + s[b:]
			}
		}
	}
	return s
}

// scrubRuns counts the strings of 256 bytes or more the rules have run
// over (a test reads it).
var scrubRuns atomic.Uint64

// Scrub remembers what it made of a long string. An agent sends its whole
// history on every turn (Codex: every item of a session, each turn), and
// what is kept of each request (the request archive, a gateway
// conversation) is scrubbed whole, so without it every turn ran each rule
// over the whole history again: a core's worth while Codex ran
// (yetone/uni-switch-releases#24). A short string costs less to scrub than to
// look up. Only a hash of a string is kept, and what it became when that
// is not the string itself.
const (
	scrubMemoMin     = 256
	scrubMemoEntries = 1 << 15
	scrubMemoBytes   = 32 << 20
)

type scrubEntry struct {
	gen  uint64
	same bool   // s came back as it was
	out  string // else what it became
}

// scrubMemo holds two generations of results: when the newer is full, it
// becomes the older and the older goes, so what is in use stays and at
// most twice the bounds are held.
var scrubMemo memo

type memo struct {
	sync.Mutex
	cur, old map[[32]byte]scrubEntry
	bytes    int
}

func (m *memo) get(k [32]byte, gen uint64, s string) (string, bool) {
	m.Lock()
	defer m.Unlock()
	e, ok := m.cur[k]
	if !ok {
		if e, ok = m.old[k]; ok && e.gen == gen {
			m.add(k, e)
		}
	}
	if !ok || e.gen != gen {
		return "", false
	}
	if e.same {
		return s, true
	}
	return e.out, true
}

func (m *memo) put(k [32]byte, gen uint64, s, out string) {
	e := scrubEntry{gen: gen, same: out == s}
	if !e.same {
		e.out = strings.Clone(out)
	}
	m.Lock()
	defer m.Unlock()
	m.add(k, e)
}

// reset forgets everything (a test starts from nothing).
func (m *memo) reset() {
	m.Lock()
	defer m.Unlock()
	m.cur, m.old, m.bytes = nil, nil, 0
}

// add is called with m held.
func (m *memo) add(k [32]byte, e scrubEntry) {
	if m.cur == nil || len(m.cur) >= scrubMemoEntries || m.bytes+len(e.out) > scrubMemoBytes {
		m.old, m.cur, m.bytes = m.cur, make(map[[32]byte]scrubEntry), 0
	}
	m.bytes += len(e.out) - len(m.cur[k].out)
	m.cur[k] = e
}

// ScrubHeader is a header's value with its secrets taken out: all of it
// for a header named for one or a credential (Bearer, Basic), else what
// Scrub finds in it.
func ScrubHeader(name, value string) string {
	v := strings.ToLower(strings.TrimSpace(value))
	if SecretHeader(name) || strings.HasPrefix(v, "bearer ") || strings.HasPrefix(v, "basic ") {
		return Scrubbed
	}
	return Scrub(value)
}

// SecretHeader says a header called name carries a credential, by the
// words of its name: Authorization, Cookie and Set-Cookie, x-api-key and
// api-key, X-Goog-Api-Key, an X-Client-Key, any *-Token (Copilot's
// Copilot-Session-Token, X-Amz-Security-Token), *-Secret, *-Auth, a
// request's *-Signature. A count of tokens is no secret: Anthropic's
// anthropic-ratelimit-tokens-remaining and OpenAI's
// x-ratelimit-remaining-tokens are what a rate limit is debugged by. Nor
// is a key that names no credential: Idempotency-Key.
func SecretHeader(name string) bool {
	n := strings.ToLower(name)
	words := strings.FieldsFunc(n, func(r rune) bool { return r == '-' || r == '_' || r == '.' })
	kept := words[:0:0]
	for i, w := range words {
		switch w {
		case "authorization", "authorisation", "auth", "cookie", "token", "secret", "password", "passwd",
			"pass", "credential", "credentials", "signature", "apikey", "csrf", "xsrf":
			return true
		case "key":
			if i == 0 || !headerKeyNotSecret[words[i-1]] {
				return true
			}
		case "tokens":
			continue // a count of them
		}
		for _, end := range []string{"token", "secret", "cookie", "apikey", "password"} {
			if strings.HasSuffix(w, end) {
				return true // accesstoken, clientsecret, setcookie
			}
		}
		kept = append(kept, w)
	}
	// what a field's name says (x-access-key, x-private-key-id), its count
	// of tokens left out
	return SecretName(strings.Join(kept, "-"))
}

// headerKeyNotSecret are the words before "key" in a header that names no
// credential.
var headerKeyNotSecret = map[string]bool{"idempotency": true, "cache": true, "public": true, "sort": true, "partition": true}

// ScrubJSON is a body with its secrets taken out: a JSON one string by
// string, a field named for a secret wholly; a stream's events each as
// JSON, line by line; anything else as text. A picture's data is left as
// it is.
func ScrubJSON(body []byte) []byte {
	if out, ok := scrubJSON(body); ok {
		return out
	}
	lines := strings.SplitAfter(string(body), "\n")
	for i, l := range lines {
		if rest, ok := strings.CutPrefix(l, "data:"); ok {
			trimmed := strings.TrimSpace(rest)
			if out, ok := scrubJSON([]byte(trimmed)); ok && trimmed != "" {
				lines[i] = "data: " + string(out) + l[len(strings.TrimRight(l, "\r\n")):]
				continue
			}
		}
		lines[i] = Scrub(l)
	}
	return []byte(strings.Join(lines, ""))
}

func scrubJSON(b []byte) ([]byte, bool) {
	return walk(b, func(_, key, s string) string {
		switch {
		case strings.HasPrefix(s, "data:"):
			return s
		case key != "" && SecretName(key):
			return Scrubbed
		}
		return Scrub(s)
	})
}

type credentialRule struct {
	kind  string
	re    *regexp.Regexp
	group int
}

// Fixed credential patterns are used only when recording diagnostics. They
// intentionally do not identify names, emails, phone numbers or user words.
var credentials = []credentialRule{
	{"PRIVATE_KEY", regexp.MustCompile(`-----BEGIN (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----[\s\S]+?-----END (?:[A-Z0-9]+ )*PRIVATE KEY(?: BLOCK)?-----`), 0},
	{"API_KEY", regexp.MustCompile(`sk-(?:ant-|proj-|or-|svcacct-|admin-)?[A-Za-z0-9_-]{20,}`), 0},
	{"API_KEY", regexp.MustCompile(`[a-z]{2,4}_sk_[A-Za-z0-9_-]{20,}|ark-[A-Za-z0-9][A-Za-z0-9_-]{15,}`), 0},
	{"API_KEY", regexp.MustCompile(`gh[pousr]_[A-Za-z0-9]{30,}|github_pat_[A-Za-z0-9_]{40,}|glpat-[A-Za-z0-9_-]{20,}|AIza[0-9A-Za-z_-]{35}`), 0},
	{"API_KEY", regexp.MustCompile(`xox[abposr]-[0-9A-Za-z-]{10,}|(?:sk|rk)_live_[0-9A-Za-z]{20,}|(?:AKIA|ASIA)[0-9A-Z]{16}`), 0},
	{"API_KEY", regexp.MustCompile(`hf_[A-Za-z0-9]{30,}|gsk_[A-Za-z0-9]{40,}|xai-[A-Za-z0-9]{40,}|npm_[A-Za-z0-9]{36}|pypi-[A-Za-z0-9_-]{50,}|dop_v1_[a-f0-9]{64}|SG\.[A-Za-z0-9_-]{22}\.[A-Za-z0-9_-]{43}`), 0},
	{"TOKEN", regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`), 0},
	{"PASSWORD", regexp.MustCompile(`[A-Za-z][A-Za-z0-9+.-]*://[^\s:@/?#"'<>]*:([^\s@/?#"'<>]+)@`), 1},
	{"SECRET", regexp.MustCompile(`(?i)[A-Za-z0-9_.-]*(?:password|passwd|secret|token|api[_-]?key|access[_-]?key|private[_-]?key|credential|_key|_pass)[A-Za-z0-9_.-]*(?:\\?["'])?[ \t]*[:=][ \t]*(?:\\?["'])?([A-Za-z0-9_\-./+=~!@#%^&*]{4,})`), 1},
	{"SECRET", regexp.MustCompile(`(?im)^[ \t]*(?:authorization|proxy-authorization|cookie|set-cookie)[ \t]*:[ \t]*(\S[^\r\n]*)`), 1},
	{"TOKEN", regexp.MustCompile(`(?i)\b(?:Bearer|Basic)[ \t]+([A-Za-z0-9._~+/=-]+)`), 1},
}
