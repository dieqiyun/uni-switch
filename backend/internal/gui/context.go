package gui

import (
	"net/http"

	"strconv"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// ownerOf is the session an agent's route that names none belongs to: the
// one whose own replies carry the route's reply id, and, when no one's does,
// the one whose conversation the route opens. A reply id is the vendor's,
// which some give out again (Ollama's chatcmpl-<0-998>), so it names a
// session over the opening only where the two agree: the opening is no
// one's, or two sessions', or the reply's session's own, or one it was
// forked from (sessions' ForkOf, or the opening its seed carried when the
// fork names no parent). Another session's opening goes by the opening, as
// it did before reply ids were read.
func ownerOf(agent, responseID, conv string, of map[string]string) string {
	id, _ := ownerBy(agent, responseID, conv, of)
	return id
}

// ownerBy is ownerOf, and whether the reply named the session rather than
// the opening: a route whose opening a fork copied too, and whose reply names
// no session, belongs to neither (joinConvs).
func ownerBy(agent, responseID, conv string, of map[string]string) (id string, byReply bool) {
	opened := ""
	if conv != "" {
		opened = of[ownerKey(agent, ownerConv, conv)]
	}
	if responseID != "" {
		if id := of[ownerKey(agent, ownerResponse, responseID)]; id != "" && (opened == "" || forkedFrom(agent, id, opened, conv, of)) {
			return id, true
		}
	}
	return opened, false
}

// forkedFrom says session id is from, or a fork of it, or of a fork of it:
// by the source its header names (ForkOf), or, for a fork dsh wrote with
// none, by the opening conv its seed carried (sessions' SeedConvs).
func forkedFrom(agent, id, from, conv string, of map[string]string) bool {
	for n := 0; id != "" && n < 64; n++ {
		if id == from {
			return true
		}
		if conv != "" && of[ownerKey(agent, ownerSeed, id+"\x00"+conv)] != "" {
			return true
		}
		id = of[ownerKey(agent, ownerFork, id)]
	}
	return false
}

// The kinds of id a route without a session id is named by: the reply it
// got, and the conversation it opens; and a session's source, for the
// session a fork's reply names (ForkOf), the opening a fork's seed carried,
// kept on the fork itself (SeedConvs), and that opening kept again among the
// openings a fork copied, which names neither the source nor the fork alone
// (ownerCopied).
const (
	ownerResponse = "response"
	ownerConv     = "conv"
	ownerFork     = "fork"
	ownerSeed     = "seed"
	ownerCopied   = "copied"
)

func ownerKey(agent, kind, id string) string { return agent + "\x00" + kind + "\x00" + id }

// routeOwners is each agent's reply ids and conversation ids (agent, NUL,
// kind, NUL, the id) and the session of that agent each was read from
// (sessions' ResponseIDs and Convs); "" for an id two sessions share, which
// stays as it is. A fork's session id is kept with the one it was forked
// from (ownerFork), and the openings its seed carried among the openings a
// fork copied (ownerCopied).
func routeOwners(list []sessions.Session) map[string]string {
	of := map[string]string{}
	put := func(k, id string) {
		if was, ok := of[k]; ok && was != id {
			of[k] = ""
		} else if !ok {
			of[k] = id
		}
	}
	for _, s := range list {
		for _, c := range s.Convs {
			put(ownerKey(s.Agent, ownerConv, c), s.ID)
		}
		for _, r := range s.ResponseIDs {
			put(ownerKey(s.Agent, ownerResponse, r), s.ID)
		}
		if s.ForkOf != "" {
			of[ownerKey(s.Agent, ownerFork, s.ID)] = s.ForkOf
		}
		for _, c := range s.SeedConvs {

			of[ownerKey(s.Agent, ownerSeed, s.ID+"\x00"+c)] = s.ID
			of[ownerKey(s.Agent, ownerCopied, c)] = s.ID
		}
	}
	return of
}

func contextRoutesAPI(mux *http.ServeMux) {

	mux.HandleFunc("GET /api/context/text", func(rw http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		id, err := strconv.ParseInt(q.Get("id"), 10, 64)
		if err != nil || id <= 0 {
			http.Error(rw, "invalid route id", http.StatusBadRequest)
			return
		}
		item := -1
		if v := q.Get("item"); v != "" {
			if item, err = strconv.Atoi(v); err != nil || item < 0 {
				http.Error(rw, "invalid item", http.StatusBadRequest)
				return
			}
		}
		var pt gateway.PromptText
		ok := false
		if gw := served.Load(); gw != nil {
			pt, ok = gw.PromptText(id, q.Get("kind"), item >= 0)
		}
		if !ok {

			if pt, ok = archivedPromptText(rw, r, id, q.Get("kind"), item >= 0); !ok {
				return
			}
		}
		if item >= 0 {

			if item >= len(pt.Items) {
				http.Error(rw, "invalid item", http.StatusBadRequest)
				return
			}
			for i := range pt.Items {
				if i != item {
					pt.Items[i].Pieces = nil
				}
			}
			if pt.Items[item].Pieces == nil {
				pt.Items[item].Pieces = []gateway.PromptPiece{}
			}
		}
		rw.Header().Set("Cache-Control", "no-store")
		writeJSON(rw, pt)
	})
	contextReplyAPI(mux)
}
