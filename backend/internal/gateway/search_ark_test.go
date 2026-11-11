package gateway

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
	"github.com/dieqiyun/uni-switch/backend/internal/settings"
)

// Volcengine Ark's Responses API runs web_search itself, on each of its
// preset's regions (the plans and pay-as-you-go share its host): Codex's
// tool goes to it rather than to uni-switch's search (axelburks on Discord).
// Its Chat and Anthropic APIs are left as they were.
func TestArkSearchesOnResponses(t *testing.T) {
	pre, err := provider.FromPreset("volcengine")
	if err != nil {
		t.Fatal(err)
	}
	i := slices.IndexFunc(provider.Presets(), func(d provider.PresetDef) bool { return d.ID == "volcengine" })
	if i < 0 || len(provider.Presets()[i].Regions) == 0 {
		t.Fatal("Volcengine Ark's preset has no regions")
	}
	for _, r := range provider.Presets()[i].Regions {
		if r.Responses == "" {
			continue
		}
		p := pre
		p.Chat, p.Responses, p.Anthropic = r.Chat, r.Responses, r.Anthropic
		if !searchesItself(p, provider.Responses) {
			t.Errorf("Ark's %s doesn't search by itself on its Responses API (%s)", r.ID, r.Responses)
		}
		for _, proto := range []provider.Protocol{provider.Chat, provider.Anthropic} {
			if searchesItself(p, proto) {
				t.Errorf("Ark's %s searches by itself on %s", r.ID, proto)
			}
		}
	}
	// BytePlus ModelArk's docs name no web_search tool
	if searchesItself(provider.Provider{ID: "byteplus", Responses: "https://ark.ap-southeast.bytepluses.com/api/v3"}, provider.Responses) {
		t.Error("BytePlus ModelArk searches by itself")
	}
}

const arkSearchReply = "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
	"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"item_id\":\"msg_1\",\"output_index\":0,\"content_index\":0,\"delta\":\"ok\"}\n\n" +
	"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"status\":\"completed\",\"output\":[{\"type\":\"message\",\"id\":\"msg_1\",\"role\":\"assistant\",\"content\":[{\"type\":\"output_text\",\"text\":\"ok\"}]}],\"usage\":{\"input_tokens\":3,\"output_tokens\":1,\"total_tokens\":4}}}\n\n"

// Codex's web_search reaches Ark's Responses API as a hosted web_search
// tool, in the shape Ark's docs give: external_web_access, which Ark
// doesn't name, is left out, and a location keeps Ark's members. Before,
// Ark was taken for a provider that doesn't search, and the tool was
// taken out for uni-switch's. Zhipu's Responses API, which searches too, is
// sent Codex's tool as it was; a relay that doesn't search is still given
// uni-switch's search in its place.
func TestArkGetsCodexWebSearch(t *testing.T) {
	f := &fake{t: t, reply: arkSearchReply}
	setup(t, provider.Responses, f)
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	for _, p := range []provider.Provider{
		{ID: "volcengine", Preset: "volcengine", Name: "Volcengine Ark", Key: "k", Models: []string{"doubao-seed-2-1-pro"}, Responses: "http://ark.cn-beijing.volces.com/api/coding/v3"},
		{ID: "zhipu", Name: "Zhipu", Key: "k", Models: []string{"glm-5"}, Responses: "http://open.bigmodel.cn/api/v1"},
		{ID: "relay", Name: "Relay", Key: "k", Models: []string{"doubao-seed-2-1-pro"}, Responses: "http://relay.example.com/v1"},
	} {
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
	}
	s := New()
	addr := strings.TrimPrefix(up.URL, "http://")
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	const body = `{"model":"%s","stream":true,"instructions":"be brief","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"今天有什么 AI 新闻？"}]}],` +
		`"tools":[{"type":"function","name":"shell","description":"run","parameters":{"type":"object","properties":{}}},` +
		`{"type":"web_search","external_web_access":true,"search_context_size":"medium","user_location":{"type":"approximate","country":"CN","city":"Beijing","timezone":"Asia/Shanghai"}}]}`
	tools := func(t *testing.T) []map[string]any {
		var got struct {
			Tools []map[string]any `json:"tools"`
		}
		if err := json.Unmarshal(f.got, &got); err != nil {
			t.Fatalf("sent %s: %v", f.got, err)
		}
		return got.Tools
	}
	hosted := func(ts []map[string]any) map[string]any {
		for _, tl := range ts {
			if tl["type"] == "web_search" {
				return tl
			}
		}
		return nil
	}
	post := func(t *testing.T, model string) {
		f.got = nil
		code, reply := postTo(t, s, "/v1/responses", strings.Replace(body, "%s", model, 1))
		if code != 200 {
			t.Fatalf("%s: %d %s", model, code, reply)
		}
	}

	t.Run("ark", func(t *testing.T) {
		post(t, "volcengine/doubao-seed-2-1-pro")
		ws := hosted(tools(t))
		if ws == nil {
			t.Fatalf("Ark wasn't sent web_search: %s", f.got)
		}
		for _, k := range []string{"external_web_access", "search_context_size"} {
			if _, ok := ws[k]; ok {
				t.Errorf("Ark was sent web_search's %s, which its docs don't name: %v", k, ws)
			}
		}
		loc, _ := ws["user_location"].(map[string]any)
		if loc["city"] != "Beijing" || loc["country"] != "CN" {
			t.Errorf("Ark's user_location lost the city or country: %v", ws)
		}
		if _, ok := loc["timezone"]; ok {
			t.Errorf("Ark was sent a location's timezone: %v", ws)
		}
		if !strings.Contains(string(f.got), `"name":"shell"`) {
			t.Errorf("Ark lost the function tool: %s", f.got)
		}
	})
	t.Run("zhipu as sent", func(t *testing.T) {
		post(t, "zhipu/glm-5")
		ws := hosted(tools(t))
		if ws == nil || ws["external_web_access"] != true || ws["search_context_size"] != "medium" {
			t.Errorf("Zhipu wasn't sent Codex's web_search as it was: %s", f.got)
		}
	})
	t.Run("relay given uni-switch's", func(t *testing.T) {
		post(t, "relay/doubao-seed-2-1-pro")
		if ws := hosted(tools(t)); ws != nil {
			t.Errorf("a relay that doesn't search was sent the hosted web_search: %s", f.got)
		}
	})
}

// forArkSearch leaves a body bound elsewhere, or with no search, as it came.
func TestForArkSearchScope(t *testing.T) {
	in := []byte(`{"model":"m","tools":[{"type":"web_search","external_web_access":true}]}`)
	for _, p := range []provider.Provider{
		{ID: "openai", Responses: "https://api.openai.com/v1"},
		{ID: "byteplus", Responses: "https://ark.ap-southeast.bytepluses.com/api/v3"},
		{ID: "ark-acct", Responses: "https://ark.cn-beijing.volces.com/api/v3", Account: &provider.Account{Agent: "plugin"}},
	} {
		if out := forArkSearch(p, in); string(out) != string(in) {
			t.Errorf("%s: %s, want it as it came", p.ID, out)
		}
	}
	ark := provider.Provider{ID: "volcengine", Responses: "https://ark.cn-beijing.volces.com/api/v3"}
	plain := []byte(`{"model":"m","tools":[{"type":"function","name":"f"}]}`)
	if out := forArkSearch(ark, plain); string(out) != string(plain) {
		t.Errorf("a body with no search changed: %s", out)
	}
	if out := forArkSearch(ark, []byte(`{"tools":[{"type":"web_search_preview"}]}`)); !strings.Contains(string(out), `{"type":"web_search"}`) {
		t.Errorf("web_search_preview isn't web_search for Ark: %s", out)
	}
	doubao := []byte(`{"tools":[{"type":"web_search","sources":["doubao"]}]}`)
	if out := forArkSearch(ark, doubao); string(out) != string(doubao) {
		t.Errorf("Ark's own shape changed: %s", out)
	}
}

// arkRefusesInclude answers as Ark's Responses API does a request whose
// include names a value it doesn't take (asked with a real key,
// 2026-11-01): the whole request is refused.
func arkRefusesInclude(body []byte) (int, string) {
	var q struct {
		Include []string `json:"include"`
	}
	json.Unmarshal(body, &q)
	for _, v := range q.Include {
		if !arkIncludes[v] {
			return 400, `{"error":{"code":"InvalidParameter","message":"The parameter ` + "`include`" + ` specified in the request are not valid: unknown type: ` + v + `. Request id: 021793510893623dc6263e9e52ba05a43f20bd75063a4f8b23617","param":"include","type":"BadRequest"}}`
		}
	}
	return 0, ""
}

const arkSearchedReply = "data: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\",\"model\":\"deepseek-v4-1-flash-260910\"}}\n\n" +
	"data: {\"type\":\"response.output_item.done\",\"item\":{\"type\":\"web_search_call\",\"status\":\"completed\",\"action\":{\"type\":\"search\",\"query\":\"今天北京天气\"}}}\n\n" +
	"data: {\"type\":\"response.output_text.delta\",\"delta\":\"北京今天晴，见 https://www.weather.com.cn/weather/101010100.shtml\"}\n\n" +
	"data: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"usage\":{\"input_tokens\":40,\"output_tokens\":20}}}\n\n"

// Every search that reaches Ark's Responses API goes without an include
// value Ark refuses (axelburks on Discord: 400 "The parameter `include`
// specified in the request are not valid: unknown type: ..."): a client's
// web search on an Ark model, translated (Claude Code's
// web_search_20250305) or relayed (OpenCode's AI SDK, which adds
// web_search_call.action.sources itself), and Settings' searcher on an
// Ark model. Sealed reasoning, which Ark takes, still goes.
func TestArkSearchSendsNoIncludeItRefuses(t *testing.T) {
	searchSandbox(t)
	f := &fake{t: t, reply: arkSearchedReply, refuse: arkRefusesInclude}
	up := httptest.NewServer(f)
	t.Cleanup(up.Close)
	ark := provider.Provider{ID: "volcengine", Preset: "volcengine", Name: "Volcengine Ark", Key: "k", Models: []string{"deepseek-v4-1-flash-260910"}, Responses: "http://ark.cn-beijing.volces.com/api/v3"}
	if err := provider.Save(ark); err != nil {
		t.Fatal(err)
	}
	s := New()
	addr := strings.TrimPrefix(up.URL, "http://")
	s.client = &http.Client{Transport: &http.Transport{DialContext: func(ctx context.Context, network, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, network, addr)
	}}}
	sent := func(t *testing.T) []string {
		t.Helper()
		var q struct {
			Include []string         `json:"include"`
			Tools   []map[string]any `json:"tools"`
		}
		if err := json.Unmarshal(f.got, &q); err != nil {
			t.Fatalf("sent %s: %v", f.got, err)
		}
		if !slices.ContainsFunc(q.Tools, func(tl map[string]any) bool { return tl["type"] == "web_search" }) {
			t.Fatalf("Ark wasn't sent its web_search: %s", f.got)
		}
		return q.Include
	}

	t.Run("claude code", func(t *testing.T) {
		code, reply := postTo(t, s, "/v1/messages", `{"model":"volcengine/deepseek-v4-1-flash-260910","max_tokens":3000,"stream":true,"messages":[{"role":"user","content":"今天北京天气"}],"tools":[{"type":"web_search_20250305","name":"web_search"}]}`)
		if code != 200 || strings.Contains(reply, "unknown type") {
			t.Fatalf("%d %s", code, reply)
		}
		if inc := sent(t); len(inc) != 0 {
			t.Errorf("Ark was asked for include %q", inc)
		}
	})
	t.Run("opencode relayed", func(t *testing.T) {
		code, reply := postTo(t, s, "/v1/responses", `{"model":"volcengine/deepseek-v4-1-flash-260910","stream":true,"reasoning":{"effort":"low"},"input":[{"role":"user","content":[{"type":"input_text","text":"今天北京天气"}]}],`+
			`"tools":[{"type":"web_search"}],"include":["reasoning.encrypted_content","web_search_call.action.sources"]}`)
		if code != 200 || strings.Contains(reply, "unknown type") {
			t.Fatalf("%d %s", code, reply)
		}
		if inc := sent(t); !slices.Equal(inc, []string{"reasoning.encrypted_content"}) {
			t.Errorf("Ark was asked for include %q, want the sealed reasoning alone", inc)
		}
	})
	t.Run("settings searcher", func(t *testing.T) {
		st := settings.Load()
		st.Searcher = "volcengine/deepseek-v4-1-flash-260910"
		if err := settings.Save(st); err != nil {
			t.Fatal(err)
		}
		said, _, err := s.modelSearch(context.Background(), "今天北京天气")
		if err != nil || !strings.Contains(said, "北京今天晴") {
			t.Fatalf("search = %q %v", said, err)
		}
		if inc := sent(t); len(inc) != 0 {
			t.Errorf("Ark was asked for include %q", inc)
		}
	})
}

// Ark's rule is Ark's: OpenAI's Responses API, and a relay, are still
// asked for the pages a search found, and a client's include goes on as
// it came; Ark keeps only what it takes, with no sources added.
func TestArkIncludeScope(t *testing.T) {
	include := func(body, host string) []string {
		t.Helper()
		r, err := parseResponses([]byte(body))
		if err != nil {
			t.Fatal(err)
		}
		var q struct {
			Include []string `json:"include"`
		}
		json.Unmarshal(buildResponses(r, "m", host, false), &q)
		return q.Include
	}
	const body = `{"model":"m","input":"hi","reasoning":{"effort":"low"},"include":["reasoning.encrypted_content","message.output_text.logprobs"],"tools":[{"type":"web_search"}]}`
	for host, want := range map[string][]string{
		"api.openai.com": {"reasoning.encrypted_content", "message.output_text.logprobs", "web_search_call.action.sources"},
		"relay.example":  {"reasoning.encrypted_content", "message.output_text.logprobs", "web_search_call.action.sources"},
		arkHost:          {"reasoning.encrypted_content"},
	} {
		if got := include(body, host); !slices.Equal(got, want) {
			t.Errorf("%s: include %q, want %q", host, got, want)
		}
	}
	// a relayed body: OpenAI's as it came, Ark's without what it refuses
	in := []byte(`{"model":"m","tools":[{"type":"function","name":"f"}],"include":["reasoning.encrypted_content","web_search_call.action.sources"]}`)
	if out := forArkSearch(provider.Provider{ID: "openai", Responses: "https://api.openai.com/v1"}, in); string(out) != string(in) {
		t.Errorf("OpenAI's body changed: %s", out)
	}
	out := forArkSearch(provider.Provider{ID: "volcengine", Responses: "https://ark.cn-beijing.volces.com/api/v3"}, in)
	if !strings.Contains(string(out), `"include":["reasoning.encrypted_content"]`) || !strings.Contains(string(out), `"name":"f"`) {
		t.Errorf("Ark's body: %s", out)
	}
	only := forArkSearch(provider.Provider{ID: "volcengine", Responses: "https://ark.cn-beijing.volces.com/api/v3"}, []byte(`{"model":"m","include":["web_search_call.action.sources"]}`))
	if strings.Contains(string(only), "include") || strings.Contains(string(only), "tools") {
		t.Errorf("Ark's body with nothing it takes: %s", only)
	}
}
