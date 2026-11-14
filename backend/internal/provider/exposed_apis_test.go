package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// #2094 (wangjianjq): Command Code's Provider API lists its Claude models on
// /messages alone and the rest on chat and Responses. A provider given only
// the chat and Responses URLs, with every model picked (Select all, or the
// CLI's whole list), offered agents the Claude models too, which no request
// could reach; one given only the Anthropic URL offered every other model.
// What it offers is the models listed on an API it has a URL for, whatever
// was picked, and with nothing picked the first 24 of those; a model whose
// list says no API stays as it was.
func TestExposedOnlyOnTheAPIsTheProviderHas(t *testing.T) {
	oneHome(t)
	list, err := os.ReadFile(filepath.Join("testdata", "commandcode_models.json"))
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/models") {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write(list)
	}))
	defer srv.Close()
	// the APIs each model is listed on, from the list itself
	var body struct {
		Data []struct {
			ID        string   `json:"id"`
			Endpoints []string `json:"supported_endpoints"`
		} `json:"data"`
	}
	if err := json.Unmarshal(list, &body); err != nil {
		t.Fatal(err)
	}
	var all, messages, chat []string
	for _, m := range body.Data {
		all = append(all, m.ID)
		apis := catalog.EndpointAPIs(m.Endpoints)
		if slices.Contains(apis, "anthropic") {
			messages = append(messages, m.ID)
		}
		if slices.Contains(apis, "chat") || slices.Contains(apis, "responses") {
			chat = append(chat, m.ID)
		}
	}
	if len(messages) == 0 || len(chat) <= manyModels {
		t.Fatalf("the fixture has %d models on /messages and %d on chat", len(messages), len(chat))
	}
	base := srv.URL + "/provider"
	for _, c := range []struct {
		id                        string
		chat, responses, anthropc string
		want                      []string
	}{
		{"cc-chat", base + "/v1", base + "/v1", "", chat},
		{"cc-messages", "", "", base, messages},
		{"cc-all", base + "/v1", base + "/v1", base, all},
	} {
		t.Run(c.id, func(t *testing.T) {
			p := Provider{ID: c.id, Name: c.id, Key: "sk-x", Chat: c.chat, Responses: c.responses, Anthropic: c.anthropc}
			if err := Save(p); err != nil {
				t.Fatal(err)
			}
			q, err := Find(c.id)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := q.Fetch(context.Background()); err != nil {
				t.Fatal(err)
			}
			if q, err = Find(c.id); err != nil {
				t.Fatal(err)
			}
			if n := len(q.Available()); n != len(all) {
				t.Fatalf("listed %d, want the whole list of %d", n, len(all))
			}
			// nothing picked: the first 24 of those it can serve
			got := idsOf(q.Exposed())
			if wantN := min(len(c.want), manyModels); len(got) != wantN {
				t.Errorf("with none picked, exposed %d, want %d", len(got), wantN)
			}
			for _, id := range got {
				if !slices.Contains(c.want, id) {
					t.Errorf("with none picked, exposed %s, which it has no URL for", id)
				}
			}
			// every model picked, as Select all or the CLI's whole list
			// saves them, and one added by hand that no list names
			q.Models = append(slices.Clone(all), "typed-by-hand")
			if err := Save(*q); err != nil {
				t.Fatal(err)
			}
			if q, err = Find(c.id); err != nil {
				t.Fatal(err)
			}
			want := append(slices.Clone(c.want), "typed-by-hand")
			got = idsOf(q.Exposed())
			slices.Sort(got)
			slices.Sort(want)
			if !slices.Equal(got, want) {
				t.Errorf("with every model picked, exposed %d, want %d:\n got %v\nwant %v", len(got), len(want), got, want)
			}
			for _, e := range Catalog() {
				if e.Provider.ID == c.id && !slices.Contains(want, e.Model) {
					t.Errorf("agents are offered %s/%s", c.id, e.Model)
				}
			}
		})
	}
}

// The user's own API for a model is the one it is asked on (SetModelAPI):
// a model listed on /messages alone, set to Responses at a provider with a Responses
// URL, is offered; and a provider whose list says no API offers every pick.
func TestExposedKeepsTheUsersAPIAndUnlistedAPIs(t *testing.T) {
	oneHome(t)
	p := Provider{ID: "plain", Name: "plain", Key: "sk-x", Chat: "https://plain.example.invalid/v1", Responses: "https://plain.example.invalid/v1", Models: []string{"a", "b"}}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("plain", p.Chat, []catalog.Model{{ID: "a"}, {ID: "b", APIs: []string{"anthropic"}}}); err != nil {
		t.Fatal(err)
	}
	q, err := Find("plain")
	if err != nil {
		t.Fatal(err)
	}
	if got := idsOf(q.Exposed()); !slices.Equal(got, []string{"a"}) {
		t.Errorf("exposed %v, want [a]: b is listed on /messages alone", got)
	}
	if err := SetModelAPI("plain/b", string(Responses)); err != nil {
		t.Fatal(err)
	}
	if q, err = Find("plain"); err != nil {
		t.Fatal(err)
	}
	if got := idsOf(q.Exposed()); !slices.Equal(got, []string{"a", "b"}) {
		t.Errorf("exposed %v, want [a b]: b was set to be asked on Responses", got)
	}
}
