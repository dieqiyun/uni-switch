package usage

import (
	"context"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/probelog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// A provider renamed is shown by the id it has now everywhere usage names
// it, its calls from before the rename too: the Requests page listed it
// twice, once by each id, its model-list rows under the old one, and the
// Sessions tab named it by the old id (cybercaius on Discord). An id it
// gave up and another provider took later is the new one's only from
// then on.
func TestRenamedProviderShownOnce(t *testing.T) {
	reportedHome(t)
	if err := provider.Save(provider.Provider{ID: "myrelay", Name: "My relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-9"}}); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-time.Hour)
	Append(Record{Time: then, Agent: "codex", Session: "s1", Provider: "myrelay", Model: "gpt-9", Input: 100, Output: 10, Status: 200})
	probelog.Sink(probelog.Row{Time: then.Add(time.Second), Kind: probelog.KindList, Provider: "myrelay", Host: "relay.example", Endpoint: "GET /v1/models", Status: 502, Millis: 30, Error: "bad gateway"})
	// read once under the old id, so what is kept of it is built again
	if p := QueryPage(All, Filter{}, 0, 50); !slices.Equal(p.Providers, []string{"myrelay"}) {
		t.Fatalf("before: %v", p.Providers)
	}
	_ = Vias(time.Time{})
	if err := provider.Rename("myrelay", "newrelay"); err != nil {
		t.Fatal(err)
	}
	Append(Record{Time: then.Add(2 * time.Second), Agent: "codex", Session: "s1", Provider: "newrelay", Model: "gpt-9", Input: 100, Output: 10, Status: 200})

	page := QueryPage(All, Filter{}, 0, 50)
	if !slices.Equal(page.Providers, []string{"newrelay"}) {
		t.Fatalf("providers %v", page.Providers)
	}
	for _, r := range page.Rows {
		if r.Provider != "newrelay" {
			t.Fatalf("a %s row under %q", r.Source, r.Provider)
		}
	}
	if p := QueryPage(All, Filter{Provider: "newrelay"}, 0, 50); p.Total != 3 || p.Sum.Calls != 2 {
		t.Fatalf("filtered: total %d sum %+v", p.Total, p.Sum)
	}
	vs := Vias(time.Time{}).Of("codex|s1")
	if len(vs) != 1 || vs[0].Provider != "newrelay" || vs[0].Calls != 2 {
		t.Fatalf("vias %+v", vs)
	}

	// a provider added under the old id afterwards takes only its own calls
	if err := provider.Save(provider.Provider{ID: "myrelay", Name: "Another", Key: "k2", Chat: "http://127.0.0.1:2/v1", Models: []string{"gpt-9"}}); err != nil {
		t.Fatal(err)
	}
	Append(Record{Time: time.Now().Add(time.Second), Agent: "codex", Session: "s2", Provider: "myrelay", Model: "gpt-9", Input: 100, Output: 10, Status: 200})
	page = QueryPage(All, Filter{}, 0, 50)
	if !slices.Equal(page.Providers, []string{"myrelay", "newrelay"}) {
		t.Fatalf("providers after the id was taken %v", page.Providers)
	}
	got := map[string]int{}
	for _, r := range page.Rows {
		got[r.Provider]++
	}
	if got["newrelay"] != 3 || got["myrelay"] != 1 {
		t.Fatalf("rows by provider %v", got)
	}
	if vs := Vias(time.Time{}).Of("codex|s1"); len(vs) != 1 || vs[0].Provider != "newrelay" {
		t.Fatalf("vias of the renamed one %+v", vs)
	}
	if vs := Vias(time.Time{}).Of("codex|s2"); len(vs) != 1 || vs[0].Provider != "myrelay" {
		t.Fatalf("vias of the new one %+v", vs)
	}
	calls := map[string]int{}
	for _, g := range Summarize(All).Models {
		calls[g.ID] += g.Calls
	}
	if calls["newrelay/gpt-9"] != 2 || calls["myrelay/gpt-9"] != 1 {
		t.Fatalf("summary %v", calls)
	}
}

// yyddbds on Discord: one custom provider, 大灰狼 on dahuilang.best, was
// listed twice in the Requests page's providers, 大灰狼 and
// dahuilang.best. The model lists asked while it was being added had no
// id to go under, so they were recorded under its host, and the page
// listed that host as a provider. They are the provider's on that host
// now, in a log written before this was fixed too, while a host no
// provider is on, or two are, stays as it was recorded.
func TestProbesOfAProviderBeingAddedListItOnce(t *testing.T) {
	reportedHome(t)
	then := time.Now().Add(-time.Hour)
	// the Add sheet's Detect, before the provider has an id: written as
	// probelog writes it, under the host of the address asked
	adding := probelog.With(context.Background(), probelog.KindDetect, "", "")
	probelog.Note(adding, "https://dahuilang.best/v1/models", http.MethodGet, then, 200, nil)
	probelog.Note(adding, "https://other.example/v1/models", http.MethodGet, then, 200, nil)
	probelog.Note(adding, "https://DaHuiLang.best/v1/chat/completions", http.MethodPost, then, 401, nil)
	probelog.Note(adding, "https://shared.example:8443/v1/models", http.MethodGet, then, 200, nil)
	id, err := provider.Add(provider.Provider{Name: "大灰狼", Key: "k", Chat: "https://dahuilang.best/v1", Models: []string{"gpt-9"}})
	if err != nil {
		t.Fatal(err)
	}
	for i, k := range []string{"k1", "k2"} {
		if err := provider.Save(provider.Provider{ID: "shared-" + k, Name: "Shared " + k, Key: k, Chat: "https://shared.example:8443/v" + string(rune('1'+i)), Models: []string{"m"}}); err != nil {
			t.Fatal(err)
		}
	}
	Append(Record{Time: then.Add(time.Minute), Agent: "codex", Session: "s1", Provider: id, Model: "gpt-9", Input: 100, Output: 10, Status: 200})

	page := QueryPage(All, Filter{}, 0, 50)
	if want := []string{id, "other.example", "shared.example:8443"}; !slices.Equal(page.Providers, want) {
		t.Fatalf("providers %v, want %v", page.Providers, want)
	}
	if p := QueryPage(All, Filter{Provider: id}, 0, 50); p.Total != 3 || p.Sum.Calls != 1 {
		t.Fatalf("%s's rows: total %d, sum %+v", id, p.Total, p.Sum)
	}
}

// cybercaius on Discord: a provider tested while it was being added was
// listed on the Requests page under its id and its host, and after it
// was deleted the host stayed beside the id. Its probes are listed under
// the id its calls to that host were made under, deleted or not; a host
// two deleted providers were on stays as it was recorded.
func TestProbesOfADeletedProviderStayWithIt(t *testing.T) {
	reportedHome(t)
	then := time.Now().Add(-time.Hour)
	adding := probelog.With(context.Background(), probelog.KindDetect, "", "")
	probelog.Note(adding, "https://api.example.me/v1/models", http.MethodGet, then, 200, nil)
	probelog.Note(adding, "https://API.example.me/v1/chat/completions", http.MethodPost, then, 200, nil)
	probelog.Note(adding, "https://twice.example/v1/models", http.MethodGet, then, 200, nil)
	probelog.Note(adding, "https://Twice.Example/v1/models", http.MethodGet, then, 200, nil)
	id, err := provider.Add(provider.Provider{Name: "Example", Key: "k", Chat: "https://api.example.me/v1", Models: []string{"m"}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := provider.Find(id)
	if err != nil {
		t.Fatal(err)
	}
	tested := probelog.With(context.Background(), probelog.KindTest, id, "")
	probelog.Note(tested, "https://api.example.me/v1/chat/completions", http.MethodPost, then.Add(time.Minute), 200, nil)
	Append(Record{Time: then.Add(2 * time.Minute), Agent: "codex", Session: "s1", Provider: id, Host: saved.Where(), Model: "m", Input: 100, Output: 10, Status: 200})
	for _, other := range []string{"one", "two"} {
		Append(Record{Time: then.Add(2 * time.Minute), Agent: "codex", Session: "s2", Provider: other, Host: "twice.example", Model: "m", Input: 1, Output: 1, Status: 200})
	}
	list := func() []string { return QueryPage(All, Filter{}, 0, 50).Providers }
	if want := []string{id, "one", "twice.example", "two"}; !slices.Equal(list(), want) {
		t.Fatalf("saved: providers %v, want %v", list(), want)
	}
	if err := provider.Delete(id); err != nil {
		t.Fatal(err)
	}
	if want := []string{id, "one", "twice.example", "two"}; !slices.Equal(list(), want) {
		t.Fatalf("deleted: providers %v, want %v", list(), want)
	}
	if p := QueryPage(All, Filter{Provider: id}, 0, 50); p.Total != 4 || p.Sum.Calls != 1 {
		t.Fatalf("deleted %s's rows: total %d, sum %+v", id, p.Total, p.Sum)
	}
	if p := QueryPage(All, Filter{Provider: "twice.example"}, 0, 50); p.Total != 2 {
		t.Fatalf("twice.example's rows: total %d", p.Total)
	}
}

// CyberCaius on Discord: Leee's model list was asked at leee.baby while it
// was being added, and it was saved on another host of that site, so the
// Usage page's provider filter listed Leee and leee.baby (as example and
// example.me before: a provider on api.example.me). A probe recorded under
// a host no provider is on is the one provider's on a host of the same site
// (its registrable domain, whatever the subdomain or port), and after that
// one is deleted, the one the log's calls to that site went to. A site two
// providers are on, a hosting platform's names (vercel.app is a suffix of
// its own) and an address stay as they were recorded.
func TestProbesOfAProviderOnAnotherHostOfItsSiteListIt(t *testing.T) {
	reportedHome(t)
	then := time.Now().Add(-time.Hour)
	adding := probelog.With(context.Background(), probelog.KindList, "", "")
	for _, u := range []string{
		"https://leee.baby/v1/models", "https://Leee.Baby/v1/models", "https://www.leee.baby:443/v1/models",
		"https://two.example/v1/models", "https://mine.vercel.app/v1/models", "http://127.0.0.1:7/v1/models",
	} {
		probelog.Note(adding, u, http.MethodGet, then, 401, nil) // an answered list is no row (quietProbe)
	}
	id, err := provider.Add(provider.Provider{Name: "Leee", Key: "k", Chat: "https://api.leee.baby/v1", Models: []string{"gpt-9"}})
	if err != nil {
		t.Fatal(err)
	}
	saved, err := provider.Find(id)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []provider.Provider{
		{ID: "two-a", Name: "Two A", Key: "a", Chat: "https://a.two.example/v1", Models: []string{"m"}},
		{ID: "two-b", Name: "Two B", Key: "b", Chat: "https://b.two.example/v1", Models: []string{"m"}},
		{ID: "theirs", Name: "Theirs", Key: "c", Chat: "https://theirs.vercel.app/v1", Models: []string{"m"}},
		{ID: "local", Name: "Local", Key: "d", Chat: "http://127.0.0.1:8/v1", Models: []string{"m"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	list := func() []string { return QueryPage(All, Filter{}, 0, 50).Providers }
	want := []string{"127.0.0.1:7", id, "mine.vercel.app", "two.example"}
	if !slices.Equal(list(), want) { // saved, no call made yet
		t.Fatalf("no calls: providers %v, want %v", list(), want)
	}
	Append(Record{Time: then.Add(time.Minute), Agent: "codex", Session: "s1", Provider: id, Host: saved.Where(), Model: "gpt-9", Input: 100, Output: 10, Status: 200})
	if !slices.Equal(list(), want) {
		t.Fatalf("saved: providers %v, want %v", list(), want)
	}
	if p := QueryPage(All, Filter{Provider: id}, 0, 50); p.Total != 4 || p.Sum.Calls != 1 {
		t.Fatalf("%s's rows: total %d, sum %+v", id, p.Total, p.Sum)
	}
	if err := provider.Delete(id); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(list(), want) {
		t.Fatalf("deleted: providers %v, want %v", list(), want)
	}
	if p := QueryPage(All, Filter{Provider: id}, 0, 50); p.Total != 4 {
		t.Fatalf("deleted %s's rows: total %d", id, p.Total)
	}
}

// cybercaius on Discord: a provider deleted and another added under its
// id were one on the Usage page, the new one with the old one's calls.
// The deleted one's calls stay its own, under an id of their own, named
// as it was and marked deleted; the new one has only its own.
func TestDeletedProviderKeepsItsCallsFromOneAddedUnderItsID(t *testing.T) {
	reportedHome(t)
	if err := provider.Save(provider.Provider{ID: "relay", Name: "Old relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-9"}}); err != nil {
		t.Fatal(err)
	}
	then := time.Now().Add(-time.Hour)
	Append(Record{Time: then, Agent: "codex", Session: "s1", Provider: "relay", Model: "gpt-9", Input: 100, Output: 10, Status: 200})
	Append(Record{Time: then.Add(time.Second), Agent: "codex", Session: "s1", Provider: "relay", Model: "gpt-9", Input: 100, Output: 10, Status: 200})
	if err := provider.Delete("relay"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Save(provider.Provider{ID: "relay", Name: "New relay", Key: "k2", Chat: "http://127.0.0.1:2/v1", Models: []string{"gpt-9"}}); err != nil {
		t.Fatal(err)
	}
	Append(Record{Time: time.Now().Add(time.Second), Agent: "codex", Session: "s2", Provider: "relay", Model: "gpt-9", Input: 100, Output: 10, Status: 200})

	page := QueryPage(All, Filter{}, 0, 50)
	if len(page.Providers) != 2 || page.Providers[0] != "relay" || !strings.HasPrefix(page.Providers[1], "relay~") {
		t.Fatalf("providers %v", page.Providers)
	}
	old := page.Providers[1]
	d, ok := provider.UsageRenames().Gone(old)
	if !ok || d.ID != "relay" || d.Name != "Old relay" {
		t.Fatalf("gone %q: %+v %v", old, d, ok)
	}
	if p := QueryPage(All, Filter{Provider: "relay"}, 0, 50); p.Sum.Calls != 1 {
		t.Fatalf("the new one's: %+v", p.Sum)
	}
	if p := QueryPage(All, Filter{Provider: old}, 0, 50); p.Sum.Calls != 2 {
		t.Fatalf("the deleted one's: %+v", p.Sum)
	}
	if vs := Vias(time.Time{}).Of("codex|s1"); len(vs) != 1 || vs[0].Provider != old {
		t.Fatalf("vias of the deleted one %+v", vs)
	}
	calls := map[string]int{}
	for _, g := range Summarize(All).Models {
		calls[g.ID] += g.Calls
	}
	if calls["relay/gpt-9"] != 1 || calls[old+"/gpt-9"] != 2 {
		t.Fatalf("summary %v", calls)
	}
}

// CyberCaius on Discord: a deleted provider's calls can be merged into a
// provider there is now, from the Usage page, without renaming either:
// they are listed, filtered and summed as that one's, each still costing
// what it did (priced as the deleted provider's), and parted again on
// undo. A provider added later under the deleted one's id keeps its own
// calls; renaming the provider merged into keeps the merge, and deleting
// it ends it.
func TestDeletedProviderMergedIntoAnother(t *testing.T) {
	reportedHome(t)
	s := settings.Load()
	s.ModelPrices = map[string]settings.ModelPrice{
		"relay/gpt-9": {Input: new(1.0), Output: new(2.0), CacheRead: new(0.0), CacheWrite: new(0.0)},
		"main/gpt-9":  {Input: new(100.0), Output: new(200.0), CacheRead: new(0.0), CacheWrite: new(0.0)},
	}
	if err := settings.Save(s); err != nil {
		t.Fatal(err)
	}
	for _, p := range []provider.Provider{
		{ID: "relay", Name: "Old relay", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"gpt-9"}},
		{ID: "main", Name: "Main", Key: "k2", Chat: "http://127.0.0.1:2/v1", Models: []string{"gpt-9"}},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	then := time.Now().Add(-time.Hour)
	Append(Record{Time: then, Agent: "codex", Session: "s1", Provider: "relay", Model: "gpt-9", Input: 1e6, Output: 1e6, Status: 200})
	Append(Record{Time: then.Add(time.Second), Agent: "codex", Session: "s1", Provider: "relay", Model: "gpt-9", Input: 1e6, Output: 1e6, Status: 200})
	Append(Record{Time: then.Add(2 * time.Second), Agent: "codex", Session: "s2", Provider: "main", Model: "gpt-9", Input: 1e6, Output: 1e6, Status: 200})
	if err := provider.Delete("relay"); err != nil {
		t.Fatal(err)
	}
	before := QueryPage(All, Filter{}, 0, 50)
	if !slices.Equal(before.Providers, []string{"main", "relay"}) {
		t.Fatalf("before: %v", before.Providers)
	}
	cost := func(s Summary) (float64, map[string]int) {
		calls, sum := map[string]int{}, 0.0
		for _, g := range s.Models {
			calls[g.ID] += g.Calls
			sum += g.Cost
		}
		return sum, calls
	}
	sumBefore, _ := cost(Summarize(All))

	if err := provider.MergeDeleted("relay", "main"); err != nil {
		t.Fatal(err)
	}
	page := QueryPage(All, Filter{}, 0, 50)
	if !slices.Equal(page.Providers, []string{"main"}) {
		t.Fatalf("merged: %v", page.Providers)
	}
	for _, r := range page.Rows {
		if r.Provider != "main" {
			t.Fatalf("a row under %q", r.Provider)
		}
	}
	// 2 calls at relay's $3 and one at main's $300, as before the merge
	if page.Sum.Cost != before.Sum.Cost || page.Sum.Cost != 306 {
		t.Fatalf("cost %v, before %v", page.Sum.Cost, before.Sum.Cost)
	}
	if p := QueryPage(All, Filter{Provider: "main"}, 0, 50); p.Sum.Calls != 3 || p.Sum.Cost != 306 {
		t.Fatalf("main's: %+v", p.Sum)
	}
	sumAfter, calls := cost(Summarize(All))
	if sumAfter != sumBefore || calls["main/gpt-9"] != 3 || calls["relay/gpt-9"] != 0 {
		t.Fatalf("summary %v (cost %v, before %v)", calls, sumAfter, sumBefore)
	}
	if vs := Vias(time.Time{}).Of("codex|s1"); len(vs) != 1 || vs[0].Provider != "main" {
		t.Fatalf("vias %+v", vs)
	}
	coster := NewCoster()
	if c, ok := coster(Record{Time: then, Provider: "relay", Model: "gpt-9", Input: 1e6, Output: 1e6}); !ok || c != 3 {
		t.Fatalf("coster %v %v", c, ok)
	}
	if got := NewPricer()([]Record{{Time: then, Provider: "relay", Model: "gpt-9", Input: 1e6, Output: 1e6}}); got.Cost != 3 {
		t.Fatalf("pricer %+v", got)
	}

	// a provider added under relay's id later is not merged
	if err := provider.Save(provider.Provider{ID: "relay", Name: "New relay", Key: "k3", Chat: "http://127.0.0.1:3/v1", Models: []string{"gpt-9"}}); err != nil {
		t.Fatal(err)
	}
	Append(Record{Time: time.Now().Add(time.Second), Agent: "codex", Session: "s3", Provider: "relay", Model: "gpt-9", Input: 1e6, Output: 1e6, Status: 200})
	if p := QueryPage(All, Filter{Provider: "relay"}, 0, 50); p.Sum.Calls != 1 {
		t.Fatalf("the new relay's: %+v", p.Sum)
	}
	mergedSum := QueryPage(All, Filter{Provider: "main"}, 0, 50).Sum
	if mergedSum.Calls != 3 {
		t.Fatalf("main's, a relay added since: %+v", mergedSum)
	}

	// the provider merged into renamed: still merged
	if err := provider.Rename("main", "primary"); err != nil {
		t.Fatal(err)
	}
	if p := QueryPage(All, Filter{Provider: "primary"}, 0, 50); p.Sum.Calls != 3 {
		t.Fatalf("renamed: %+v (providers %v)", p.Sum, p.Providers)
	}

	// undone: the deleted relay's calls are its own again, by the id the
	// page lists it under now that another has relay
	old := ""
	for _, id := range QueryPage(All, Filter{}, 0, 50).Providers {
		if strings.HasPrefix(id, "relay~") {
			old = id
		}
	}
	if old != "" {
		t.Fatalf("listed apart while merged: %q", old)
	}
	gone := provider.UsageRenames().Merged("primary")
	if len(gone) != 1 || !strings.HasPrefix(gone[0], "relay~") {
		t.Fatalf("merged into primary: %v", gone)
	}
	if err := provider.MergeDeleted(gone[0], ""); err != nil {
		t.Fatal(err)
	}
	// the parts sum to what the merge showed, each call as it cost
	parted, own := QueryPage(All, Filter{Provider: gone[0]}, 0, 50).Sum, QueryPage(All, Filter{Provider: "primary"}, 0, 50).Sum
	if parted.Calls != 2 || own.Calls != 1 || own.Cost != 300 || parted.Cost+own.Cost != mergedSum.Cost || parted.Unpriced+own.Unpriced != mergedSum.Unpriced {
		t.Fatalf("parted %+v, primary's own %+v, merged %+v", parted, own, mergedSum)
	}

	// merged again, then the provider merged into deleted: the merge ends
	if err := provider.MergeDeleted(gone[0], "primary"); err != nil {
		t.Fatal(err)
	}
	if err := provider.Delete("primary"); err != nil {
		t.Fatal(err)
	}
	if p := QueryPage(All, Filter{Provider: gone[0]}, 0, 50); p.Sum.Calls != 2 {
		t.Fatalf("its target deleted: %+v (providers %v)", p.Sum, QueryPage(All, Filter{}, 0, 50).Providers)
	}
	if err := provider.MergeDeleted(gone[0], "nobody"); err == nil {
		t.Fatal("merged into a provider there is not")
	}
}
