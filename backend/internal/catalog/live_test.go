package catalog

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestEndpointAPIs(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{[]string{"/messages"}, "anthropic"},
		{[]string{"/v1/messages", "/chat/completions", "ws:/responses", "/responses"}, "anthropic,chat,responses"},
		{[]string{"/v1/chat/completions/", "/chat/completions"}, "chat"},
		{[]string{"/embeddings"}, ""},
		{nil, ""},
	} {
		if got := strings.Join(EndpointAPIs(c.in), ","); got != c.want {
			t.Errorf("%v: %q", c.in, got)
		}
	}
}

// A list that tells each model's context window (Command Code, OpenRouter)
// has it kept; an odd value is only left out.
func TestFetchContextLength(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Write([]byte(`{"object":"list","data":[{"id":"claude-sonnet-5","name":"Claude Sonnet 5","context_length":1000000,"supported_endpoints":["/messages"]},{"id":"kimi-k3","context_length":"lots"},{"id":"glm-5"}]}`))
	}))
	defer srv.Close()
	ms, _, err := FetchAt(context.Background(), srv.URL+"/v1", "k", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]int{}
	for _, m := range ms {
		got[m.ID] = m.Context
	}
	if len(got) != 3 || got["claude-sonnet-5"] != 1000000 || got["kimi-k3"] != 0 || got["glm-5"] != 0 {
		t.Errorf("contexts %v", got)
	}
}

// Retired remote gateway price metadata must not affect ordinary vendor models.
func TestRetiredGatewayPricesIgnored(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"id":"a/priced","uni_switch_price":{"input":3,"output":12,"cache_read":0.3,"cache_write":3.75,"tiers":[{"above":200000,"input":6,"output":18,"cache_read":0.6,"cache_write":7.5}]}},
			{"id":"a/free","uni_switch_price":{"input":0,"output":0,"cache_read":0,"cache_write":0}},
			{"id":"a/negative","uni_switch_price":{"input":-1,"output":12,"cache_read":0,"cache_write":0}},
			{"id":"a/sizeless","uni_switch_price":{"input":1,"output":2,"cache_read":0,"cache_write":0,"tiers":[{"above":0,"input":2,"output":4}]}},
			{"id":"a/odd","uni_switch_price":"cheap"},
			{"id":"a/none"}]}`)
	}))
	defer srv.Close()
	ms, err := FetchURL(context.Background(), srv.URL+"/v1/models", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]*Price{}
	for _, m := range ms {
		got[m.ID] = m.Price
	}
	for _, id := range []string{"a/priced", "a/free", "a/negative", "a/none"} {
		if p := got[id]; p != nil {
			t.Errorf("%s: %+v", id, p)
		}
	}
	if len(ms) != 6 {
		t.Errorf("an odd price lost a model: %d of 6", len(ms))
	}
}

// Command Code supplies distinct names for its Flash and Flash Fast models.
func TestFetchCommandCodeNames(t *testing.T) {
	writeCatalog(t, `{
	  "coralbricks":{"models":{
	    "deepseek-v4.1-flash":{"id":"deepseek-v4.1-flash","name":"DeepSeek V4.1 Flash"},
	    "deepseek-v4.1-flash-fast":{"id":"deepseek-v4.1-flash-fast","name":"DeepSeek V4.1 Flash"}}},
	  "vercel":{"models":{
	    "deepseek-v4.1-flash-fast":{"id":"deepseek-v4.1-flash-fast","name":"DeepSeek V4.1 Flash Fast"}}}
	}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"object":"list","data":[
		  {"id":"deepseek/deepseek-v4.1-flash","object":"model","created":1791381046,"owned_by":"command-code","name":"DeepSeek V4.1 Flash","context_length":1000000,"supported_endpoints":["/chat/completions","/responses"]},
		  {"id":"deepseek/deepseek-v4.1-flash-fast","object":"model","created":1791381046,"owned_by":"command-code","name":"DeepSeek V4.1 Flash Fast","context_length":1000000,"supported_endpoints":["/chat/completions","/responses"]}]}`))
	}))
	defer srv.Close()
	ms, err := Fetch(context.Background(), srv.URL+"/v1", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("models: %+v", ms)
	}
	for i, m := range Named(Decorate(ms, Provider("coralbricks"))) {
		wantID, wantName := "deepseek/deepseek-v4.1-flash", "DeepSeek V4.1 Flash"
		if i == 1 {
			wantID += "-fast"
			wantName += " Fast"
		}
		if m.ID != wantID || m.Name != wantName {
			t.Errorf("model %d: id %q, name %q; want %q, %q", i, m.ID, m.Name, wantID, wantName)
		}
		if m.Context != 1000000 || strings.Join(m.APIs, ",") != "chat,responses" {
			t.Errorf("model facts lost: %+v", m)
		}
	}
}

func TestFetchNamePrecedence(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"models":[
		  {"id":"all","uni_switch_label":"label","display_name":"display","name":"name"},
		  {"id":"display","uni_switch_label":"","display_name":"display name","name":"name"},
		  {"id":"name","display_name":"","name":"provider name"},
		  {"id":"id","name":""},
		  {"id":"prefixed","name":"Vendor: Model"},
		  {"id":"prefixed-display","display_name":"Vendor: Display","name":"Vendor: Name"},
		  {"id":"prefixed-label","uni_switch_label":"Vendor: Label","name":"Vendor: Name"},
		  {"id":"variant","name":"Model:free"},
		  {"name":"name-only"},
		  {"display_name":"no identifier"},
		  {}]}`))
	}))
	defer srv.Close()
	ms, err := Fetch(context.Background(), srv.URL+"/v1", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range ms {
		got = append(got, m.ID+"|"+m.Name)
	}
	want := "all|display\ndisplay|display name\nname|provider name\nid|id\nprefixed|prefixed\nprefixed-display|Vendor: Display\nprefixed-label|prefixed-label\nvariant|Model:free\nname-only|name-only"
	if strings.Join(got, "\n") != want {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(got, "\n"), want)
	}
}

// OpenRouter's real /api/v1/models shape has vendor-prefixed names and no
// display_name. Those names still come from its catalog, without the prefix.
func TestFetchOpenRouterNames(t *testing.T) {
	writeCatalog(t, `{"openrouter":{"models":{
	  "anthropic/claude-haiku-5.5":{"id":"anthropic/claude-haiku-5.5","name":"Claude Haiku 5.5"},
	  "google/gemini-nano-banana-2.1":{"id":"google/gemini-nano-banana-2.1","name":"Nano Banana 2.1"}
	}}}`)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Fields from https://openrouter.ai/api/v1/models, 2026-10-08.
		w.Write([]byte(`{"data":[
		  {"id":"anthropic/claude-haiku-5.5","canonical_slug":"anthropic/claude-haiku-5.5-20261007","name":"Anthropic: Claude Haiku 5.5","created":1791397883,"context_length":1000000,"architecture":{"modality":"text+image+file->text","input_modalities":["text","image","file"],"output_modalities":["text"],"tokenizer":"Claude","instruct_type":null},"top_provider":{"context_length":1000000,"max_completion_tokens":128000,"is_moderated":true}},
		  {"id":"google/gemini-nano-banana-2.1","canonical_slug":"google/gemini-nano-banana-2.1-20261006","name":"Google: Nano Banana 2.1","created":1791300825,"context_length":65536,"architecture":{"modality":"text+image->text+image","input_modalities":["image","text"],"output_modalities":["image","text"],"tokenizer":"Gemini","instruct_type":null},"top_provider":{"context_length":65536,"max_completion_tokens":58982,"is_moderated":false}}]}`))
	}))
	defer srv.Close()
	ms, err := Fetch(context.Background(), srv.URL+"/api/v1", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(ms) != 2 {
		t.Fatalf("models: %+v", ms)
	}
	for _, m := range ms {
		if m.Name != m.ID {
			t.Errorf("prefixed name kept for %q: %q", m.ID, m.Name)
		}
	}
	wantNames := []string{"Claude Haiku 5.5", "Nano Banana 2.1"}
	wantContexts := []int{1000000, 65536}
	for i, m := range Named(Decorate(ms, Provider("openrouter"))) {
		if m.ID != ms[i].ID || m.Name != wantNames[i] || m.Context != wantContexts[i] {
			t.Errorf("model %d: %+v; want id %q, name %q, context %d", i, m, ms[i].ID, wantNames[i], wantContexts[i])
		}
	}
}

// A relay names Gemini's image models for what they are called, not for
// what Google ships: "Nano Banana 2.1" and its spellings draw, though no
// word of Google's own ids is in them (#2071). A model the list says
// nothing of stays in the chat list, where the user marks what it makes
// (#1399).
func TestFetchRelayDrawerNames(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"data":[
			{"id":"Nano Banana 2.1"},
			{"id":"nano-banana-pro"},
			{"id":"NanoBanana2"},
			{"id":"gemini-3-pro-image-preview"},
			{"id":"omni_flash-10s"},
			{"id":"claude-sonnet-4-5"}]}`)
	}))
	defer srv.Close()
	ms, err := FetchURL(context.Background(), srv.URL+"/v1/models", "", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	draws, chat := map[string]bool{}, map[string]bool{}
	for _, m := range ms {
		if m.Films {
			t.Errorf("%s: films, which nothing here said", m.ID)
		} else if m.Draws {
			draws[m.ID] = true
		} else {
			chat[m.ID] = true
		}
	}
	for _, id := range []string{"gemini-3-pro-image-preview", "Nano Banana 2.1", "nano-banana-pro", "NanoBanana2"} {
		if !draws[id] {
			t.Errorf("%s: not a drawer", id)
		}
	}
	if !chat["omni_flash-10s"] || !chat["claude-sonnet-4-5"] {
		t.Errorf("the chat list lost a model: omni_flash-10s %v, claude-sonnet-4-5 %v", chat["omni_flash-10s"], chat["claude-sonnet-4-5"])
	}
}
