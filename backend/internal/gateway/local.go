package gateway

import (
	"crypto/subtle"
	"errors"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/budget"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
	"github.com/dieqiyun/uni-switch/backend/internal/usage"
)

// Port is the gateway's port.
func Port() string {
	_, p, err := net.SplitHostPort(Addr())
	if err != nil {
		return strconv.Itoa(settings.DefaultPort)
	}
	return p
}

// InContainer: this uni-switch runs in a container (Docker, Podman, a
// Kubernetes pod), whose image is what gets updated, not its binary.
func InContainer() bool { return inContainer("/") }

// inContainer: the system under root is a container's — Docker's or
// Podman's marker file, or a container runtime in PID 1's cgroup.
func inContainer(root string) bool {
	for _, f := range []string{".dockerenv", "run/.containerenv"} {
		if _, err := os.Stat(filepath.Join(root, f)); err == nil {
			return true
		}
	}
	b, _ := os.ReadFile(filepath.Join(root, "proc/1/cgroup"))
	for _, w := range []string{"docker", "containerd", "kubepods", "libpod", "lxc"} {
		if strings.Contains(string(b), w) {
			return true
		}
	}
	return false
}

// listen opens the local listener, optionally accepting TLS; under lnMu.
func (s *Server) listen(addr string) (net.Listener, error) {
	ln, err := Listen(addr)
	if err != nil || s.tls == nil {
		return ln, err
	}
	return newTLSListener(ln, s.tls), nil
}

// Relisten moves the local gateway to its configured port. In-flight requests finish.
func (s *Server) Relisten() error {
	s.lnMu.Lock()
	defer s.lnMu.Unlock()
	if s.ln == nil {
		return nil // not serving
	}
	to := Addr()
	if s.ln.Addr().String() == to {
		return nil
	}
	was := s.ln.Addr().String()
	// another port (Settings' port changed): the new one is taken up before
	// the old one lets go, so one the gateway can't have leaves it serving
	// where it was
	if _, p, _ := net.SplitHostPort(was); p != Port() {
		ln, err := s.listen(to)
		if err != nil {
			return err
		}
		old := s.ln
		s.ln = ln
		old.Close()
		return nil
	}
	// the port is the same, so the old one goes first
	s.ln.Close()
	ln, err := s.listen(to)
	if err != nil {
		if ln, _ = s.listen(was); ln == nil {
			return err
		}
		s.ln = ln
		return err
	}
	s.ln = ln
	return nil
}

// callbacks are the callback tokens of the Claude subscription runs now
// live (addCallback, dropCallback).
var callbacks = struct {
	sync.Mutex
	tokens map[string]bool
}{tokens: map[string]bool{}}

// addCallback makes token, a run's callback token (randomToken: 24 bytes
// from crypto/rand), one ownCallback takes, until dropCallback.
func addCallback(token string) {
	callbacks.Lock()
	callbacks.tokens[token] = true
	callbacks.Unlock()
}

func dropCallback(token string) {
	callbacks.Lock()
	delete(callbacks.tokens, token)
	callbacks.Unlock()
}

// ownCallback recognizes a live Claude subscription's local MCP helper.
// Its run token also follows an in-flight handover to the previous server.
// localGuard has already refused every non-local or forwarded request.
func ownCallback(r *http.Request) bool {
	if r.Method != http.MethodPost || r.URL.RawPath != "" {
		return false
	}
	token, ok := strings.CutPrefix(r.URL.Path, claudeMCPPath)
	if !ok || token == "" {
		return false
	}
	// while the uni-switch this one took the gateway over from finishes, a
	// token this one doesn't have may be one of that one's runs: it is
	// passed on there (forwardCallback), and that uni-switch judges it as it
	// would its own
	if forwarding() {
		return true
	}
	callbacks.Lock()
	defer callbacks.Unlock()
	found := false
	for t := range callbacks.tokens {
		if subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1 {
			found = true
		}
	}
	return found
}

// rebound: a browser sent this request to loopback under a hostname that
// isn't this computer's — a web site whose DNS answered 127.0.0.1 after its
// page loaded (DNS rebinding). The page is then same-origin with the
// gateway: it sends no Origin on a GET, or one equal to the Host, so
// corsGuard lets it by, and on loopback a call needs no key, so the page
// could read the models and quotas and spend the user's subscriptions.
// Agents call uni-switch as localhost or an IP, and send none of a browser's
// marks; a hostname of this computer's own (its loopback listener,
// its own name, or a container runtime's name for the host)
// isn't a web site's. An enabled gateway key, which such a page can't
// know, still lets a call in.
func rebound(r *http.Request) bool {
	if !fromBrowser(r) || ownHost(r.Host) {
		return false
	}
	return !slices.ContainsFunc(callerKeys(r), func(k string) bool { _, ok := access.Authenticate(k); return ok })
}

// fromBrowser: the request carries what a browser puts on every request
// and a page's script can't take off.
func fromBrowser(r *http.Request) bool {
	return r.Header.Get("Origin") != "" || r.Header.Get("Sec-Fetch-Site") != "" ||
		r.Header.Get("Sec-Fetch-Mode") != "" || strings.HasPrefix(r.UserAgent(), "Mozilla/")
}

// containerHostNames are the names container runtimes give the host.
var containerHostNames = []string{"host.docker.internal", "gateway.docker.internal", "host.containers.internal", "host.orb.internal", "docker.for.mac.localhost"}

// ownHost: host (a Host header) names this computer — an IP, localhost,
// or one of the names above. A subdomain of localhost doesn't (#2256):
// Safari and other WebKit views ask the DNS for it, so a page served at
// one from anywhere can have it turned to 127.0.0.1 after it loads.
func ownHost(host string) bool {
	if h, _, err := net.SplitHostPort(host); err == nil {
		host = h
	}
	host = strings.TrimSuffix(strings.ToLower(strings.Trim(host, "[]")), ".")
	if host == "" || net.ParseIP(host) != nil || host == "localhost" || slices.Contains(containerHostNames, host) {
		return true
	}
	if h, _, err := net.SplitHostPort(Addr()); err == nil && strings.EqualFold(host, h) {
		return true
	}
	if name, err := os.Hostname(); err == nil {
		name = strings.TrimSuffix(strings.ToLower(name), ".local")
		if name != "" && (host == name || host == name+".local") {
			return true
		}
	}
	return false
}

// callerGuard also covers embedded handlers used by the web app and tests.
func callerGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if access.Caller(r.Context()).KeyID == "" && managedKey(r) {
			var ok bool
			r, ok = identifyCaller(w, r)
			if !ok {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func identifyCaller(w http.ResponseWriter, r *http.Request) (*http.Request, bool) {
	var who access.Identity
	ok := false
	var refused *access.Refused
	for _, k := range callerKeys(r) {
		var err error
		if who, err = access.Check(k); err == nil {
			ok = true
			break
		}
		if refused == nil {
			errors.As(err, &refused)
		}
	}
	if !ok && refused != nil {
		// an expired key, or one its issuer no longer lets in, is told so
		// on every route, from this computer too: a sandbox's agent on
		// loopback holding a key that has run out is not let on as one
		// with no key, which would hand it everything its key held it from
		log.Printf("refused %s %s from %s: %s", r.Method, r.URL.Path, r.RemoteAddr, refused.Why)
		writeError(w, provider.Chat, http.StatusUnauthorized, refused.Why)
		return r, false
	}
	if ok {
		r = r.WithContext(access.WithIdentity(r.Context(), who))
	}
	r.Header = r.Header.Clone()
	r.Header.Set("Authorization", "Bearer "+Token)
	for _, h := range []string{"x-api-key", "x-goog-api-key"} {
		if r.Header.Get(h) != "" {
			r.Header.Set(h, Token)
		}
	}
	if q := r.URL.Query(); q.Get("key") != "" {
		q.Set("key", Token)
		u := *r.URL
		u.RawQuery = q.Encode()
		r.URL = &u
	}
	return r, true
}

// accountOf is the subscription account p answers as, named as the Routing
// trace names it (Account.User: an email, a login), never by a token; ""
// for a key or a provider without an account (#557).
func accountOf(p provider.Provider) string {
	if p.Account == nil {
		return ""
	}
	return p.Account.User
}

func appendUsage(r *http.Request, rec usage.Record) {
	if rec.Session == "" {
		rec.Session = sessionOf(r.Header)
	}
	if rec.Subagent == "" {
		rec.Subagent, rec.ParentAgent = subagentOf(r.Header)
	}
	who := access.Caller(r.Context())
	rec.CallerKeyID, rec.CallerKeyName, rec.CallerIssuerID = who.KeyID, who.KeyName, who.IssuerID()
	if !rec.IsProbe() {
		// a request an MCP tool call made: its row names the model (mcpUse)
		mcpUseOf(r.Context()).used(rec.Provider, rec.Model)
	}
	budget.Append(rec)
}

// callerKey is the API key a request carries, however its client sends one:
// the first of callerKeys, "" for none.
func callerKey(r *http.Request) string {
	if ks := callerKeys(r); len(ks) > 0 {
		return ks[0]
	}
	return ""
}

// callerKeys reads non-empty credentials in order: Authorization's bearer
// token, x-api-key, x-goog-api-key, then ?key=. Named local keys preserve
// attribution even when a client also sends a stale placeholder token.
func callerKeys(r *http.Request) []string {
	var out []string
	add := func(v string) {
		if v = strings.TrimSpace(v); v != "" && !slices.Contains(out, v) {
			out = append(out, v)
		}
	}
	a := strings.TrimSpace(r.Header.Get("Authorization"))
	if len(a) >= 7 && strings.EqualFold(a[:7], "Bearer ") {
		a = a[7:]
	} else if strings.EqualFold(a, "Bearer") {
		a = ""
	}
	add(a)
	for _, h := range []string{"x-api-key", "x-goog-api-key"} {
		add(r.Header.Get(h))
	}
	add(r.URL.Query().Get("key"))
	return out
}

// managedKey: the request carries a named gateway key's form (sk-uni-switch-…),
// or a key's own value a user gave it, in any of the places a key is read.
func managedKey(r *http.Request) bool {
	return slices.ContainsFunc(callerKeys(r), access.Named)
}

// local is a request from this computer: one that reached the gateway on
// loopback, wasn't passed on by a proxy or tunnel running here, and didn't
// come through `uni-switch web`'s page port (viaPagePort).
func local(r *http.Request) bool {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback() && !proxied(r) && !viaPagePort(r)
}

// forwardHeaders are what a proxy or tunnel adds to a request it passes
// on: cloudflared (Cf-Connecting-IP, and X-Forwarded-For beside it),
// ngrok, Tailscale serve and funnel, frp's and Caddy's and nginx's HTTP
// proxies. An agent calling uni-switch sends none of them.
var forwardHeaders = []string{"Forwarded", "X-Forwarded-For", "X-Real-IP", "Cf-Connecting-IP", "True-Client-IP", "Tailscale-User-Login"}

// proxied recognizes forwarding headers. Such requests are refused even
// when the socket peer is loopback; legacy proxy-trust settings are ignored.
func proxied(r *http.Request) bool {
	return slices.ContainsFunc(forwardHeaders, func(h string) bool { return r.Header.Get(h) != "" })
}

// LocalOnly rejects non-local peers and forwarded requests before page
// authentication or API handling. No key or legacy setting enables sharing.
func LocalOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !local(r) {
			http.Error(w, "uni-switch accepts requests from this computer only", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// localGuard serves local clients, including authenticated WSL adapters.
// Named keys retain their local budgets and model restrictions.
func localGuard(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r = takePagePort(r)
		if fromWSL(r) {
			r = asFromLoopback(r)
		}
		if !local(r) {
			writeError(w, provider.Chat, http.StatusForbidden, "uni-switch accepts requests from this computer only")
			return
		}
		if ownCallback(r) {
			next.ServeHTTP(w, r)
			return
		}
		if rebound(r) {
			writeError(w, provider.Chat, http.StatusForbidden, "uni-switch doesn't answer a browser under this hostname; use localhost or 127.0.0.1")
			return
		}
		if managedKey(r) {
			var ok bool
			r, ok = identifyCaller(w, r)
			if !ok {
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}
