package upstream

import (
	"net/http"
	"net/http/httptest"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// claudePage is status.claude.com's summary.json as it was read on
// 2026-10-06, its components' states set by the test.
func claudePage(api, code, web string, incidents string) string {
	return `{"page":{"id":"tymt9n04zgry","name":"Claude","url":"https://status.claude.com"},
"status":{"indicator":"none","description":"All Systems Operational"},
"components":[{"id":"rwppv331jlwc","name":"claude.ai","status":"` + web + `","group":false},
{"id":"0qbwn08sd68x","name":"Claude Console (platform.claude.com)","status":"operational","group":false},
{"id":"k8w3r06qmzrp","name":"Claude API (api.anthropic.com)","status":"` + api + `","group":false},
{"id":"yyzkbfz2thpt","name":"Claude Code","status":"` + code + `","group":false}],
"incidents":[` + incidents + `]}`
}

// openAIPage is status.openai.com's (incident.io): no groups, and
// incidents that name no components.
func openAIPage(responses, desktop string, incidents string) string {
	return `{"page":{"id":"01JMDK9XYNY6RXSED6SDWW50WY","name":"OpenAI","url":"https://status.openai.com/"},
"status":{"description":"Partial System Degradation","indicator":"minor"},
"components":[{"id":"01JMXBRMFE6N2NNT7DG6XZQ6PW","name":"Chat Completions","status":"operational","group":null},
{"id":"01JP8CD9JR3HR6Y7G4Q75N4DVW","name":"Responses","status":"` + responses + `","group":null},
{"id":"01KMKFAMWKQ81YWSE1Z18R6VHR","name":"Codex in ChatGPT Desktop","status":"` + desktop + `","group":null},
{"id":"01JMXBNJXGGT5SR5DB9J7GYY48","name":"Voice mode","status":"major_outage","group":null}],
"incidents":[` + incidents + `]}`
}

// serve points the vendors at a fake page and counts what it is asked.
func serve(t *testing.T, id string, body *atomic.Value, code *atomic.Int32) *atomic.Int32 {
	t.Helper()
	var asked atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked.Add(1)
		if c := code.Load(); c != 0 && c != 200 {
			w.WriteHeader(int(c))
			return
		}
		w.Write([]byte(body.Load().(string)))
	}))
	t.Cleanup(srv.Close)
	was := slices.Clone(Vendors)
	t.Cleanup(func() { Vendors = was; Forget() })
	for i := range Vendors {
		if Vendors[i].ID == id {
			Vendors[i].Summary = srv.URL + "/api/v2/summary.json"
		}
	}
	Forget()
	return &asked
}

func one(t *testing.T, id string) Status {
	t.Helper()
	s := Wait(5*time.Second, id)
	if len(s) != 1 {
		t.Fatalf("%d statuses for %s", len(s), id)
	}
	return s[0]
}

func TestClaudeAPIDownIsSaid(t *testing.T) {
	var body atomic.Value
	var code atomic.Int32
	body.Store(claudePage("partial_outage", "operational", "operational",
		`{"name":"Elevated errors on Claude API","status":"investigating","impact":"major","shortlink":"https://stspg.io/x","components":[{"id":"k8w3r06qmzrp","name":"Claude API (api.anthropic.com)"}]},
{"name":"claude.ai login issues","status":"investigating","impact":"minor","shortlink":"https://stspg.io/y","components":[{"id":"rwppv331jlwc","name":"claude.ai"}]},
{"name":"Old one","status":"resolved","impact":"major","components":[{"id":"k8w3r06qmzrp"}]}`))
	serve(t, "anthropic", &body, &code)
	s := one(t, "anthropic")
	if s.Level != "partial" || s.Error != "" {
		t.Fatalf("level %q (error %q), want partial", s.Level, s.Error)
	}
	if len(s.Parts) != 1 || s.Parts[0].Name != "Claude API (api.anthropic.com)" {
		t.Fatalf("parts %+v", s.Parts)
	}
	// the API's own incident, not claude.ai's, not a resolved one
	if len(s.Incidents) != 1 || s.Incidents[0].Name != "Elevated errors on Claude API" || s.Incidents[0].URL != "https://stspg.io/x" {
		t.Fatalf("incidents %+v", s.Incidents)
	}
	if s.Page != "https://status.claude.com" {
		t.Fatalf("page %q", s.Page)
	}
}

// claude.ai down says nothing of the API
func TestClaudeAppDownIsNotTheAPI(t *testing.T) {
	var body atomic.Value
	var code atomic.Int32
	body.Store(claudePage("operational", "operational", "major_outage",
		`{"name":"claude.ai down","status":"identified","impact":"critical","components":[{"id":"rwppv331jlwc","name":"claude.ai"}]}`))
	serve(t, "anthropic", &body, &code)
	if s := one(t, "anthropic"); s.Level != "ok" || len(s.Parts) != 0 || len(s.Incidents) != 0 {
		t.Fatalf("got %+v, want ok with nothing listed", s)
	}
}

// Claude Code is what a Claude subscription's requests run through
func TestClaudeCodeDegraded(t *testing.T) {
	var body atomic.Value
	var code atomic.Int32
	body.Store(claudePage("operational", "degraded_performance", "operational", ""))
	serve(t, "anthropic", &body, &code)
	if s := one(t, "anthropic"); s.Level != "degraded" {
		t.Fatalf("level %q, want degraded", s.Level)
	}
}

// OpenAI's page as it was on 2026-10-06: its desktop app's Codex
// degraded and voice mode down, the API well — and an incident that
// names no parts is the API's only while the API's parts are affected.
func TestOpenAIAppIsNotTheAPI(t *testing.T) {
	var body atomic.Value
	var code atomic.Int32
	inc := `{"name":"Elevated Work Mode errors","status":"monitoring","impact":"minor","shortlink":null,"components":null}`
	body.Store(openAIPage("operational", "degraded_performance", inc))
	serve(t, "openai", &body, &code)
	if s := one(t, "openai"); s.Level != "ok" || len(s.Incidents) != 0 {
		t.Fatalf("got %+v, want ok", s)
	}
	body.Store(openAIPage("major_outage", "operational", inc))
	Forget()
	s := one(t, "openai")
	if s.Level != "major" || len(s.Incidents) != 1 || s.Incidents[0].Name != "Elevated Work Mode errors" {
		t.Fatalf("got %+v, want major with its incident", s)
	}
}

// a page that fails is unknown, never "all well", and it isn't kept
// saying the outage it said before either
func TestFailedPageIsUnknown(t *testing.T) {
	var body atomic.Value
	var code atomic.Int32
	body.Store(claudePage("major_outage", "operational", "operational", ""))
	serve(t, "anthropic", &body, &code)
	if s := one(t, "anthropic"); s.Level != "major" {
		t.Fatalf("level %q", s.Level)
	}
	code.Store(503)
	defer func(n func() time.Time) { now = n }(now)
	now = func() time.Time { return time.Now().Add(Fresh) }
	s := one(t, "anthropic")
	if s.Level != "" || s.Error == "" {
		t.Fatalf("got %+v, want unknown with an error", s)
	}
	code.Store(0)
	body.Store(`<html>Just a moment...</html>`)
	now = func() time.Time { return time.Now().Add(3 * Fresh) }
	if s := one(t, "anthropic"); s.Level != "" || s.Error == "" {
		t.Fatalf("got %+v from a page that isn't JSON", s)
	}
	// a page that no longer lists the API's parts can't say anything of it
	body.Store(`{"components":[{"id":"a","name":"Something else","status":"operational"}],"incidents":[]}`)
	now = func() time.Time { return time.Now().Add(5 * Fresh) }
	if s := one(t, "anthropic"); s.Level != "" || s.Error == "" {
		t.Fatalf("got %+v from a page without the API's parts", s)
	}
}

// a page is read once in Fresh however often it is asked for, and the
// asking never waits for it
func TestReadOnceInFresh(t *testing.T) {
	var body atomic.Value
	var code atomic.Int32
	body.Store(claudePage("operational", "operational", "operational", ""))
	slow := make(chan struct{})
	asked := serve(t, "anthropic", &body, &code)
	one(t, "anthropic")
	for range 20 {
		Of("anthropic")
	}
	if n := asked.Load(); n != 1 {
		t.Fatalf("asked %d times within Fresh, want 1", n)
	}
	// stale: asked again, and Of answers at once with the last reading
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-slow
		w.Write([]byte(claudePage("major_outage", "operational", "operational", "")))
	}))
	defer srv.Close()
	// deferred after srv.Close, so it runs first: Close waits for the held
	// read, which a failed check would leave held (#1808)
	let := sync.OnceFunc(func() { close(slow) })
	defer let()
	for i := range Vendors {
		if Vendors[i].ID == "anthropic" {
			Vendors[i].Summary = srv.URL
		}
	}
	defer func(n func() time.Time) { now = n }(now)
	now = func() time.Time { return time.Now().Add(Fresh) }
	// the page is held until let, so Of coming back at all is its not
	// waiting for it: no wall-clock bound a loaded machine can pass (#1808).
	// The deadline only turns a wait into a FAIL, and is short of the
	// read's own 15s, after which a wait would come back too.
	looked := make(chan []Status, 1)
	go func() { looked <- Of("anthropic") }()
	var s []Status
	select {
	case s = <-looked:
	case <-time.After(10 * time.Second):
		t.Fatal("Of waited for the page")
	}
	if s[0].Level != "ok" {
		t.Fatalf("level %q while the page is read again, want the last one", s[0].Level)
	}
	let()
	if s := one(t, "anthropic"); s.Level != "major" {
		t.Fatalf("level %q once read again, want major", s.Level)
	}
}

// DeepSeek's page has nothing to read: it is only linked, never asked
func TestLinkedOnly(t *testing.T) {
	s := Of("deepseek")
	if len(s) != 1 || s[0].Level != "" || s[0].Error != "" || s[0].Page != "https://status.deepseek.com" {
		t.Fatalf("got %+v", s)
	}
}

func TestVendorOf(t *testing.T) {
	for _, c := range []struct {
		bases []string
		want  string
	}{
		{[]string{"", "", "https://api.anthropic.com"}, "anthropic"},
		{[]string{"https://api.openai.com/v1"}, "openai"},
		{[]string{"", "https://chatgpt.com/backend-api/codex"}, "openai"},
		{[]string{"https://api.deepseek.com/v1", "", "https://api.deepseek.com/anthropic"}, "deepseek"},
		{[]string{"https://api.moonshot.cn/v1"}, "moonshot"},
		{[]string{"https://API.Moonshot.ai:443/v1"}, "moonshot"},
		// a relay that serves the same models is not the vendor
		{[]string{"https://relay.example.com/v1", "", "https://relay.example.com"}, ""},
		{[]string{"https://api.anthropic.com.evil.example/v1"}, ""},
		{[]string{"http://127.0.0.1:11434/v1"}, ""},
	} {
		if got := VendorOf(c.bases...); got != c.want {
			t.Errorf("VendorOf(%q) = %q, want %q", c.bases, got, c.want)
		}
	}
}
