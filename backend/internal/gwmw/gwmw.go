// Package gwmw defines the native gateway request middleware interfaces.
package gwmw

import (
	"net/http"
	"slices"
	"sync"
	"sync/atomic"
	"time"
)

// Info is what an agent middleware is told of a request.
type Info struct {
	Protocol string `json:"protocol"` // the agent's API: anthropic, chat, responses, gemini
	Model    string `json:"model"`
	Stream   bool   `json:"stream"`
	Path     string `json:"path"`
	Agent    string `json:"agent,omitempty"`
}

// Reject is a request a middleware turned away: the agent gets Status with
// Message in its API's error shape.
type Reject struct {
	Status  int
	Message string
}

// Agent is a middleware on agents' requests.
type Agent interface {
	// Begin is asked as a request starts; nil leaves it alone
	Begin(Info) Run
}

// Run is an agent middleware on one request.
type Run interface {
	// Request is the body the gateway, or the next middleware, gets; a
	// Reject ends the request there
	Request(body []byte) ([]byte, *Reject)
	// Wrap is the writer the reply goes through; done writes what it holds
	Wrap(w http.ResponseWriter) (http.ResponseWriter, func())
	// End is the request's end, after done
	End()
}

// To is the provider a request is tried on, as a middleware is told it.
type To struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Account string `json:"account,omitempty"` // the subscription's kind (claude, antigravity, …); empty for a key
	Model   string `json:"model"`             // the model asked of it, by its id there
}

// Tried is a Run that also changes the body each provider the request is
// tried on is sent: Tried is that try's body, in the agent's API, and
// changes no other try's.
type Tried interface {
	Tried(body []byte, to To) []byte
}

// Account is the subscription account a request is about to be tried on,
// as the gateway knows it. Agent, User and Plan are who it is
// (provider.Account) and never reach a middleware: a middleware is told a
// ref made from them (internal/middleware's accountRef). A zero Agent is
// a try on a key, no account.
type Account struct {
	Agent, User, Plan string
	ID, Name, Model   string // the provider, and the model asked of it there
}

// Accounts is a Run asked, before each try, whether the account it goes
// to may be used for this request: skip says it may not, and the gateway
// goes on to the next as it does past an account that can't be used,
// resting nothing. by names the middleware that said so.
type Accounts interface {
	Account(a Account) (skip bool, by string)
}

var (
	mu      sync.Mutex
	agents  []*Agent
	refresh func()
)

func add[T any](l *[]*T, v T) (remove func()) {
	e := &v
	mu.Lock()
	*l = append(*l, e)
	mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			defer mu.Unlock()
			*l = slices.DeleteFunc(slices.Clone(*l), func(x *T) bool { return x == e })
		})
	}
}

// Use registers an agent middleware until remove; those registered run in
// that order on the request, and the reply goes back through them the
// other way.
func Use(a Agent) (remove func()) { return add(&agents, a) }

// Agents are the agent middlewares registered now, in their order.
func Agents() []Agent {
	mu.Lock()
	defer mu.Unlock()
	out := make([]Agent, len(agents))
	for i, a := range agents {
		out[i] = *a
	}
	return out
}

// Refresh registers the refresh callback for native agent middleware.
func Refresh(f func()) (stop func()) {
	mu.Lock()
	refresh = f
	mu.Unlock()
	var once sync.Once
	return func() {
		once.Do(func() {
			mu.Lock()
			refresh = nil
			mu.Unlock()
		})
	}
}

// throttle asks the refresh at most once a second, never from inside one.
type throttle struct {
	busy atomic.Bool
	last atomic.Int64 // when the last ask began, UnixNano
}

// ask runs the refresh when it is due and want (nil: always) says so.
func (t *throttle) ask(f func(), want func() bool) {
	now := time.Now().UnixNano()
	if now-t.last.Load() < int64(time.Second) || !t.busy.CompareAndSwap(false, true) {
		return
	}
	defer t.busy.Store(false)
	t.last.Store(now)
	if want == nil || want() {
		f()
	}
}

var forAgents throttle

func refreshFunc() func() {
	mu.Lock()
	defer mu.Unlock()
	return refresh
}

// AgentsWanted is Agents; with none, when want says some are (plugins.json
// has a middleware on), one refresh is asked first, at most once a second:
// a middleware added a moment ago turns its plugin on for the request that
// comes before the gateway's next look at the files.
func AgentsWanted(want func() bool) []Agent {
	if a := Agents(); len(a) > 0 {
		return a
	}
	if f := refreshFunc(); f != nil {
		forAgents.ask(f, want)
	}
	return Agents()
}

// ForgetRefreshForTest lets the next agent refresh run at once, as it would a
// second after the last one.
func ForgetRefreshForTest() {
	forAgents.last.Store(0)
}
