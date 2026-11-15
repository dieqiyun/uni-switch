package usage

import (
	"fmt"
	"reflect"
	"testing"
	"time"
	"unsafe"

	"github.com/dieqiyun/uni-switch/backend/internal/sessions"
)

// The Requests page reads a packed local chunk one chunk at a time instead of
// unpacking every chunk first (the warm start's peak). The page it builds is
// the page the unpacked rows build: Codex copies folded, gateway pairs with
// their compaction, Claude message copies, request-id and fallback matches.
func TestRequestPageSameFromPackedLocalChunks(t *testing.T) {
	pageHome(t)
	now := time.Date(2026, 9, 30, 12, 0, 0, 0, time.UTC)
	identities := map[string]sessions.CodexIdentity{
		"main-a":  {ID: "main-a", Role: "main", RootSession: "main-a"},
		"child-b": {ID: "child-b", Role: "subagent", ParentSession: "main-a", RootSession: "main-a"},
	}
	yes := true
	build := func(pack bool) (RequestPage, []*rowChunk) {
		gateway := &rowChunk{}
		var locals []*rowChunk
		for k := 0; k < 3; k++ {
			local := &rowChunk{}
			for i := 0; i < 80; i++ {
				at := now.Add(-time.Duration(k*100+i) * time.Minute)
				switch i % 4 {
				case 0: // a Codex call written twice, with its gateway row
					id := fmt.Sprintf("resp-%d-%d", k, i)
					if i%8 == 0 { // the other half has no gateway row and shows as written
						gateway.add(Row{Record: Record{Time: at.Add(time.Second), Agent: "codex", Provider: "openai", Model: "gw", Input: 500 + i, Output: 50, Status: 200, ResponseID: id}}, "", int64(k*1000+i), false)
					}
					for copy := 0; copy < 2; copy++ {
						local.add(Row{Record: Record{Time: at, Agent: "codex", Model: "native", Input: 80, Output: 8, Session: "s-child", ResponseID: id, Compaction: &yes}, Source: "log", codexActor: "child-b"}, "", int64(i*2+copy), false)
					}
				case 1: // a Claude message in two files
					msg := fmt.Sprintf("msg-%d-%d", k, i)
					local.add(Row{Record: Record{Time: at, Agent: "claude", Model: "claude-x", Input: 10 + i, Output: 1, Session: "c-" + msg}, Source: "log"}, msg, int64(i), false)
					local.add(Row{Record: Record{Time: at, Agent: "claude", Model: "claude-x", Input: 10 + i, Output: 1, Session: "c-" + msg}, Source: "log"}, msg, int64(i+1), false)
				case 2: // matched to a gateway row by request id
					rid := fmt.Sprintf("rid-%d-%d", k, i)
					gateway.add(Row{Record: Record{Time: at.Add(2 * time.Second), Agent: "claude", Provider: "anthropic", Model: "gw-claude", Input: 30, Output: 3, Status: 200, RequestID: rid}}, "", int64(k*1000+i), false)
					local.add(Row{Record: Record{Time: at, Agent: "claude", Model: "claude-x", Input: 30, Output: 3, RequestID: rid, Session: "r-" + rid}, Source: "log"}, "", int64(i), false)
				default: // matched by time and tokens, or left alone
					gateway.add(Row{Record: Record{Time: at.Add(time.Second), Agent: "claude", Provider: "anthropic", Model: "gw-claude", Input: 70 + i, Output: 7, Status: 200}}, "", int64(k*1000+i), false)
					local.add(Row{Record: Record{Time: at, Agent: "claude", Model: "claude-x", Input: 70 + i, Output: 7, Session: fmt.Sprintf("t-%d-%d", k, i)}, Source: "log"}, "", int64(i), false)
				}
			}
			if pack {
				local = local.pack()
			}
			locals = append(locals, local)
		}
		return buildRequestBlocksWithCodex(All, Filter{}, 0, 100, now, []*rowChunk{gateway}, locals, nil, nil, identities), locals
	}
	want, _ := build(false)
	got, packed := build(true)
	for _, c := range packed {
		if len(c.Archive) == 0 {
			t.Fatalf("a local chunk of %d rows was not packed", len(c.Rows))
		}
	}
	if len(want.Rows) == 0 || want.Sum.Calls == 0 {
		t.Fatalf("the unpacked page is empty: %+v", want)
	}
	subagent := false
	for _, r := range want.Rows {
		subagent = subagent || (r.Model == "native" && r.Subagent == "child-b")
	}
	if !subagent {
		t.Fatalf("no unpaired Codex row carries its subagent: %+v", want.Rows)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("packed page differs:\n got %+v\nwant %+v", got, want)
	}
}

// A page's strings are its own: a row or a name the page keeps from a packed
// chunk would otherwise hold the whole block of strings that chunk was read
// into, in the request cache, for as long as the page is kept.
func TestRequestPageOwnsItsStrings(t *testing.T) {
	pageHome(t)
	page := RequestPage{
		Rows:   []Row{{Record: Record{Agent: "codex", Model: "m", Session: "s"}}},
		Agents: []string{"codex"},
		By:     map[string][]Share{"agent": {{ID: "codex"}}},
		Names:  map[string]string{"pc": "laptop"},
		Heat:   &Heatmap{From: "2026-09-01"},
		Series: []SeriesPoint{{Point: Point{Label: "12:00"}, By: map[string]map[string]Part{"model": {"m": {Calls: 1}}}}},
	}
	before := map[*byte]bool{}
	var walk func(v reflect.Value, see func(string))
	walk = func(v reflect.Value, see func(string)) {
		switch v.Kind() {
		case reflect.String:
			see(v.String())
		case reflect.Pointer:
			if !v.IsNil() {
				walk(v.Elem(), see)
			}
		case reflect.Struct:
			for i := range v.NumField() {
				if v.Type().Field(i).IsExported() {
					walk(v.Field(i), see)
				}
			}
		case reflect.Slice, reflect.Array:
			for i := range v.Len() {
				walk(v.Index(i), see)
			}
		case reflect.Map:
			for it := v.MapRange(); it.Next(); {
				walk(it.Key(), see)
				walk(it.Value(), see)
			}
		}
	}
	n := 0
	walk(reflect.ValueOf(page), func(s string) {
		if s != "" {
			before[unsafe.StringData(s)] = true
			n++
		}
	})
	ownStrings(reflect.ValueOf(&page).Elem())
	kept := 0
	walk(reflect.ValueOf(page), func(s string) {
		if s != "" && before[unsafe.StringData(s)] {
			t.Errorf("%q still shares its bytes", s)
		}
		if s != "" {
			kept++
		}
	})
	if n != 12 || kept != n || page.Heat.From != "2026-09-01" || page.Names["pc"] != "laptop" || page.By["agent"][0].ID != "codex" || page.Series[0].By["model"]["m"].Calls != 1 {
		t.Fatalf("strings %d before, %d after: %+v", n, kept, page)
	}
}
