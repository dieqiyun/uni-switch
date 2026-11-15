package provider

import (
	"context"
	"encoding/json"
	"os"
	"slices"

	"testing"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

func memberIDs(ms []Member) []string {
	var out []string
	for _, m := range ms {
		out = append(out, m.ID)
	}
	return out
}

// A list saved before its models made for other APIs were kept is fetched
// again once a run, with no schedule set, so embeddings and the rest find
// them without the user pressing Refresh (#5339); a list saved with them,
// and a provider never fetched, are left alone.
func TestKindlessListFetchedOnce(t *testing.T) {
	autolistHome(t)
	t.Cleanup(func() {
		listsTried.Lock()
		clear(listsTried.m)
		listsTried.Unlock()
	})
	old := newRelayList(t, "chat-x", "embed-1")
	kinded := newRelayList(t, "chat-x", "embed-1")
	never := newRelayList(t, "chat-x")
	for id, r := range map[string]*relayList{"old": old, "kinded": kinded, "never": never} {
		if err := Save(Provider{ID: id, Name: id, Chat: r.srv.URL + "/v1", Key: "k", Models: []string{"chat-x"}}); err != nil {
			t.Fatal(err)
		}
		if id == "never" {
			continue
		}
		found, _ := Find(id)
		if _, err := found.Fetch(context.Background()); err != nil {
			t.Fatal(err)
		}
	}
	// "old"'s list as a uni-switch before this one saved it: no kinded mark,
	// embed-1 dropped
	path := catalog.LivePath("old")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var f map[string]any
	json.Unmarshal(b, &f)
	delete(f, "kinded")
	var kept []any
	for _, m := range f["models"].([]any) {
		if m.(map[string]any)["ID"] != "embed-1" {
			kept = append(kept, m)
		}
	}
	f["models"] = kept
	b, _ = json.Marshal(f)
	if err := os.WriteFile(path, b, 0o644); err != nil {
		t.Fatal(err)
	}
	if catalog.LiveKinded("old") || !catalog.LiveKinded("kinded") || len(catalog.LiveServing("old", "")) != 0 {
		t.Fatal("the old list's setup")
	}
	o, k, n := old.asked.Load(), kinded.asked.Load(), never.asked.Load()
	now := time.Now()
	if got := refreshDueLists(context.Background(), now); !slices.Equal(got, []string{"old"}) {
		t.Fatalf("asked %v, want [old]", got)
	}
	if old.asked.Load() != o+1 || kinded.asked.Load() != k || never.asked.Load() != n {
		t.Fatalf("lists asked: old %d, kinded %d, never %d", old.asked.Load()-o, kinded.asked.Load()-k, never.asked.Load()-n)
	}
	if ms := catalog.LiveServing("old", catalog.ServesEmbedding); len(ms) != 1 || ms[0].ID != "embed-1" || !catalog.LiveKinded("old") {
		t.Fatalf("after the fetch: %v", ms)
	}
	if got := refreshDueLists(context.Background(), now.Add(time.Hour)); len(got) != 0 {
		t.Fatalf("asked %v again", got)
	}
}
