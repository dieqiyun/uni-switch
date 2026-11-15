package gateway

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// sideRoute is the Routing page's route of a request the chat path doesn't
// carry: an image, a video's start, an embedding, a rerank or a
// transcription. Each wrote its usage row but began no route, so Usage
// listed it and Routing never did (zzzkl3994 on Discord: a video the
// uni-switch-image MCP made). One route stands for one request, its tries the
// accounts, keys or group members asked in turn; a video's polls and its
// download are the same generation and add none, as they add no usage row.
// A nil sideRoute does nothing, so a caller with none goes on as before.
type sideRoute struct {
	s     *Server
	tr    *Route
	start time.Time
}

// beginSide begins r's side route: asked is the model as the caller named
// it, prov the provider it resolved to first, group the routing group it
// named (nil for none).
func (s *Server) beginSide(r *http.Request, start time.Time, asked, prov string, group *GroupRef) *sideRoute {
	who := callerOf(r)
	tr := s.trace.beginFor(r, Route{Time: start, Agent: who.agent, Session: sessionOf(r.Header), Model: asked, Provider: prov,
		Group: group, Order: []Weighed{}})
	return &sideRoute{s: s, tr: tr, start: start}
}

// id is the route's id, which its usage row names (RouteID); 0 for none.
func (sr *sideRoute) id() int64 {
	if sr == nil {
		return 0
	}
	return sr.tr.ID
}

// try begins a try on c, put in the route's order too when it isn't yet,
// as the page finds a try's seat there; the func it answers ends it.
func (sr *sideRoute) try(c candidate) func(status int, err error) {
	return sr.tryAt(c, time.Now())
}

// tryAt is try for one begun at at, told once it is known who had it
// (a video's start picks its key inside startVideo).
func (sr *sideRoute) tryAt(c candidate, at time.Time) func(status int, err error) {
	if sr == nil {
		return func(int, error) {}
	}
	if c.rest == "" {
		c.rest = candidateOf(c.p, c.model).rest
	}
	sr.s.trace.update(sr.tr, func(t *Route) {
		t.Provider = c.p.ID
		if !slices.ContainsFunc(t.Order, func(w Weighed) bool { return w.ID == c.rest && w.Model == c.model }) {
			t.Order = append(t.Order, weighed(c, c.p, weighing{}, false, provider.Chat))
		}
		t.Tries = append(t.Tries, Try{ID: c.rest, Model: c.model, Start: at})
	})
	i := -1
	sr.s.trace.update(sr.tr, func(t *Route) { i = len(t.Tries) - 1 })
	return func(status int, err error) {
		ms := time.Since(at).Milliseconds()
		sr.s.trace.update(sr.tr, func(t *Route) {
			if i < 0 || i >= len(t.Tries) {
				return
			}
			try := &t.Tries[i]
			try.Done, try.Status, try.Millis = true, status, ms
			if err != nil {
				try.Error = err.Error()
			}
		})
	}
}

// end ends the route as the request ended: p and model the ones that had
// it last, u what they counted. A route ends once; a second end is a no-op.
func (sr *sideRoute) end(p provider.Provider, model string, status int, err error, u Usage) {
	if sr == nil {
		return
	}
	ms := time.Since(sr.start).Milliseconds()
	sr.s.trace.update(sr.tr, func(t *Route) {
		if t.Done {
			return
		}
		if p.ID != "" {
			t.Provider = p.ID
		}
		t.Done, t.Status, t.Millis, t.Tokens, t.Output = true, status, ms, u.Input+u.Output, u.Output
		if err != nil {
			t.Error = err.Error()
		}
		t.Usage = routeUsage(t.Provider, model, u)
	})
}

// candidateOf is the candidate p, the key or account a request went out
// on, stands for, named as perKeyBarred names it in rest: the account
// signed in to by the provider's id, another by its user after an "@",
// one of several keys by its key after a "#", else the provider alone.
func candidateOf(p provider.Provider, model string) candidate {
	c := candidate{p: p, model: model, rest: p.ID}
	switch {
	case p.Account != nil:
		if own, err := provider.Find(p.ID); err == nil && own.Account != nil && !strings.EqualFold(own.Account.User, p.Account.User) {
			c.rest = p.ID + "@" + p.Account.User
		}
	case p.Key != "" && len(p.KeysOn()) > 1:
		c.rest = p.ID + "#" + provider.KeyID(p.Key)
	}
	return c
}

type sideRouteKey struct{}

// withSide carries sr to the code that asks each account (drawOnAccounts).
func withSide(ctx context.Context, sr *sideRoute) context.Context {
	if sr == nil {
		return ctx
	}
	return context.WithValue(ctx, sideRouteKey{}, sr)
}

// sideOf is the side route ctx carries, nil for none.
func sideOf(ctx context.Context) *sideRoute {
	sr, _ := ctx.Value(sideRouteKey{}).(*sideRoute)
	return sr
}
