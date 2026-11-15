package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// a one-pixel PNG
func tinyPNG(t *testing.T) []byte {
	t.Helper()
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// fakeTools is a gateway's MCP servers as `uni-switch tool` asks them: the
// image server's generate_image and generation_result, the web search
// server's web_search, and every request it was sent.
type fakeTools struct {
	mu    sync.Mutex
	calls []string // "<path> <method> <tool> <arguments>"
	gets  []string // "<path> <Authorization>"
	// call answers a tools/call; nil answers with an empty text
	call func(name string, args map[string]any) map[string]any
	file []byte
}

func (f *fakeTools) toolCalls() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for _, c := range f.calls {
		if strings.Contains(c, " tools/call ") {
			out = append(out, c)
		}
	}
	return out
}

func (f *fakeTools) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+gateway.Token || r.Header.Get("User-Agent") != toolAgent {
		http.Error(w, "unauthorised", 401)
		return
	}
	if r.Method == http.MethodGet {
		f.mu.Lock()
		f.gets = append(f.gets, r.URL.Path)
		f.mu.Unlock()
		w.Write(f.file)
		return
	}
	var m struct {
		ID     json.RawMessage `json:"id"`
		Method string          `json:"method"`
		Params struct {
			Name      string         `json:"name"`
			Arguments map[string]any `json:"arguments"`
		} `json:"params"`
	}
	json.NewDecoder(r.Body).Decode(&m)
	args, _ := json.Marshal(m.Params.Arguments)
	f.mu.Lock()
	f.calls = append(f.calls, r.URL.Path+" "+m.Method+" "+m.Params.Name+" "+string(args))
	f.mu.Unlock()
	var result any
	switch {
	case m.Method == "tools/list" && r.URL.Path == gateway.ImageMCPPath:
		result = map[string]any{"tools": []any{
			map[string]any{"name": "generate_image", "description": "Generates an image from a text prompt. More words here.", "inputSchema": map[string]any{
				"type": "object", "required": []string{"prompt"},
				"properties": map[string]any{
					"prompt":           map[string]any{"type": "string", "description": "What to draw"},
					"n":                map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
					"quality":          map[string]any{"type": "string", "enum": []string{"low", "medium", "high", "auto"}},
					"reference_images": map[string]any{"type": "array", "items": map[string]any{"type": "string"}, "description": "Images to edit: file paths or URLs"},
					"model":            map[string]any{"type": "string", "enum": []string{"art/gpt-image-1", "art/painter"}, "description": "The image model to use, as provider/model."},
				}}},
			map[string]any{"name": gateway.GenerationResultTool, "description": "Waits.", "inputSchema": map[string]any{
				"type": "object", "required": []string{"id"}, "properties": map[string]any{"id": map[string]any{"type": "string"}}}},
		}}
	case m.Method == "tools/list" && r.URL.Path == gateway.SearchMCPPath:
		result = map[string]any{"tools": []any{
			map[string]any{"name": "web_search", "description": "Searches the web.", "inputSchema": map[string]any{
				"type": "object", "required": []string{"query"},
				"properties": map[string]any{"query": map[string]any{"type": "string"}, "deep": map[string]any{"type": "boolean"}}}},
		}}
	case m.Method == "tools/list":
		result = map[string]any{"tools": []any{}}
	case m.Method == "tools/call" && f.call != nil:
		result = f.call(m.Params.Name, m.Params.Arguments)
	case m.Method == "tools/call":
		result = map[string]any{"content": []any{map[string]any{"type": "text", "text": "done"}}}
	}
	json.NewEncoder(w).Encode(map[string]any{"jsonrpc": "2.0", "id": m.ID, "result": result})
}

// toolHere is a fake gateway uni-switch tool asks, and a folder of its own the
// command runs in.
func toolHere(t *testing.T) (*fakeTools, string) {
	t.Helper()
	f := &fakeTools{}
	srv := httptest.NewServer(f)
	t.Cleanup(srv.Close)
	t.Setenv("UNI_SWITCH_GATEWAY", srv.URL)
	dir := t.TempDir()
	t.Chdir(dir)
	was := toolPoll
	toolPoll = 0
	t.Cleanup(func() { toolPoll = was })
	return f, dir
}

func filesIn(t *testing.T, dir string) []string {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range ents {
		out = append(out, e.Name())
	}
	return out
}

// uni-switch tools lists every server's tools, each with its parameters.
func TestToolsListsTheServersTools(t *testing.T) {
	toolHere(t)
	out, err := runOut(t, "tools")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"image", "generate_image", "Generates an image from a text prompt.", "--prompt <text> (required)", "--n <1-4>", "--quality <low|medium|high|auto>", "web-search", "web_search", "--deep <true|false>"} {
		if !strings.Contains(out, want) {
			t.Errorf("no %q in:\n%s", want, out)
		}
	}
	if strings.Contains(out, "More words here") {
		t.Errorf("the list has more than a description's first sentence:\n%s", out)
	}
	out, err = runOut(t, "tools", "generate_image")
	if err != nil || !strings.Contains(out, "More words here") || !strings.Contains(out, "uni-switch tool generate_image <prompt>") {
		t.Fatalf("one tool: %v\n%s", err, out)
	}
	if _, err := runOut(t, "tools", "draw"); err == nil || !strings.Contains(err.Error(), `no tool called "draw"`) {
		t.Fatalf("an unknown tool: %v", err)
	}
}

// Parameters are read against the tool's schema: types, - and _ alike,
// lists given once per item, the words left over its prompt.
func TestToolReadsParameters(t *testing.T) {
	_, dir := toolHere(t)
	os.WriteFile(filepath.Join(dir, "ref.png"), tinyPNG(t), 0o644)
	tools, _, err := listTools(newToolGateway())
	if err != nil {
		t.Fatal(err)
	}
	img, _ := findTool(tools, "generate_image")
	c, err := parseToolArgs(img, []string{"a", "uni-switch", "--n", "2", "--quality=high", "--reference-images", "ref.png", "--reference_images", "https://x/y.png", "-o", "out", "--json"})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(c.args)
	want, _ := json.Marshal(map[string]any{"prompt": "a uni-switch", "n": 2, "quality": "high", "reference_images": []string{filepath.Join(dir, "ref.png"), "https://x/y.png"}})
	if string(got) != string(want) || c.output != "out" || !c.json {
		t.Fatalf("got %s %q %v, want %s", got, c.output, c.json, want)
	}
	search, _ := findTool(tools, "web_search")
	if c, err := parseToolArgs(search, []string{"--deep", "go", "news"}); err != nil || c.args["query"] != "go news" || c.args["deep"] != true {
		t.Fatalf("words are the query: %v %v", c.args, err)
	}
	if c, err := parseToolArgs(search, []string{"--", "-x", "y"}); err != nil || c.args["query"] != "-x y" {
		t.Fatalf("words after --: %v %v", c.args, err)
	}
}

// An unknown parameter, a missing required one or a bad value is refused
// before anything is called.
func TestToolRefusesWhatItDoesntKnowAndCallsNothing(t *testing.T) {
	f, dir := toolHere(t)
	for _, args := range [][]string{
		{"tool", "generate_image", "--style", "vivid", "a uni-switch"},
		{"tool", "generate_image", "--model", "art/nope", "a uni-switch"},
		{"tool", "generate_image", "--n", "2"},
		{"tool", "generate_image", "--n", "two", "a uni-switch"},
		{"tool", "generate_image", "--quality", "ultra", "a uni-switch"},
		{"tool", "generate_image", "--prompt", "a", "a uni-switch"},
		{"tool", "generate_image", "--n", "1", "--n", "2", "a uni-switch"},
		{"tool", "generate_image", "-x", "a uni-switch"},
		{"tool", "generate_image", "a uni-switch", "-o"},
		{"tool", "draw", "a uni-switch"},
		{"tool"},
	} {
		_, err := runOut(t, args...)
		if err == nil {
			t.Errorf("uni-switch %s: no error", strings.Join(args, " "))
		}
	}
	if c := f.toolCalls(); len(c) != 0 {
		t.Fatalf("a refused call was made: %v", c)
	}
	if got := filesIn(t, dir); len(got) != 0 {
		t.Fatalf("a refused call wrote %v", got)
	}
	_, err := runOut(t, "tool", "generate_image", "--style", "x", "a uni-switch")
	if err == nil || !strings.Contains(err.Error(), "takes no --style") || !strings.Contains(err.Error(), "--prompt") || !strings.Contains(err.Error(), "nothing was called") {
		t.Fatalf("an unknown parameter: %v", err)
	}
}

// --help prints the usage and asks the gateway nothing.
func TestToolHelpCallsNothing(t *testing.T) {
	f, dir := toolHere(t)
	for _, args := range [][]string{{"tool", "generate_image", "a uni-switch", "--help"}, {"tool", "-h"}, {"tools", "--help"}, {"tool", "help"}} {
		out, err := runOut(t, args...)
		if err != nil || !strings.Contains(out, "uni-switch tool <name>") {
			t.Fatalf("uni-switch %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.calls) != 0 || len(f.gets) != 0 || len(filesIn(t, dir)) != 0 {
		t.Fatalf("--help asked %v %v", f.calls, f.gets)
	}
}

// An image the tool makes is saved: at -o, else here named from the
// prompt, never over a file that is there; text is printed.
func TestToolSavesTheImage(t *testing.T) {
	f, dir := toolHere(t)
	pic := tinyPNG(t)
	f.call = func(name string, args map[string]any) map[string]any {
		return map[string]any{
			"content": []any{
				map[string]any{"type": "text", "text": "Drew 1 image with art/gpt-image-1"},
				map[string]any{"type": "image", "mimeType": "image/png", "data": base64.StdEncoding.EncodeToString(pic)},
			},
			"_meta": map[string]any{gateway.FilesMeta: []any{map[string]any{"url": "http://127.0.0.1:3425" + gateway.ImageMCPFiles + "a.png", "inline": true}}},
		}
	}
	out, err := runOut(t, "tool", "generate_image", "a uni-switch on a branch", "-o", "hero.png")
	if err != nil {
		t.Fatal(err)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "hero.png")); err != nil || !bytes.Equal(got, pic) {
		t.Fatalf("hero.png: %v", err)
	}
	if !strings.Contains(out, "Drew 1 image") || !strings.Contains(out, filepath.Join(dir, "hero.png")) {
		t.Fatalf("printed %q", out)
	}
	// hero.png is there: the next is saved beside it
	if _, err := runOut(t, "tool", "generate_image", "a uni-switch on a branch", "-o", "hero.png"); err != nil {
		t.Fatal(err)
	}
	if got := filesIn(t, dir); len(got) != 2 {
		t.Fatalf("files %v: one was written over", got)
	}
	// no -o: here, named from the prompt
	if _, err := runOut(t, "tool", "generate_image", "a uni-switch on a branch"); err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(filesIn(t, dir), " "); len(filesIn(t, dir)) != 3 || !strings.Contains(got, "uni-switch") {
		t.Fatalf("files %s", got)
	}
	if c := f.toolCalls(); len(c) != 3 || !strings.Contains(c[0], gateway.ImageMCPPath+` tools/call generate_image {"prompt":"a uni-switch on a branch"}`) {
		t.Fatalf("calls %v", c)
	}
	// --json prints the answer and saves nothing
	out, err = runOut(t, "tool", "generate_image", "--json", "a uni-switch")
	if err != nil || !strings.Contains(out, gateway.FilesMeta) || len(filesIn(t, dir)) != 3 {
		t.Fatalf("--json: %v %q", err, out)
	}
	// --model goes to the tool as its model argument
	if _, err := runOut(t, "tool", "generate_image", "--model", "art/painter", "--json", "a uni-switch"); err != nil {
		t.Fatal(err)
	}
	if c := f.toolCalls(); !strings.Contains(c[len(c)-1], `"model":"art/painter"`) {
		t.Fatalf("--model: %v", c)
	}
}

// One still being made is waited on with generation_result, and one that
// isn't inline is downloaded from the gateway asked, with its key.
func TestToolWaitsOnWhatIsStillMade(t *testing.T) {
	f, dir := toolHere(t)
	f.file = []byte("\x00\x00\x00\x18ftypmp42\x00\x00\x00\x00mp42isomfake-mp4-bytes")
	asks := 0
	f.call = func(name string, args map[string]any) map[string]any {
		if name == "generate_image" {
			return map[string]any{
				"content": []any{map[string]any{"type": "text", "text": `still being made: call generation_result with id "img_1"`}},
				"_meta":   map[string]any{gateway.PendingMeta: "img_1"},
			}
		}
		if asks++; asks < 2 {
			// a gateway older than _meta: its text alone says to wait
			return map[string]any{"content": []any{map[string]any{"type": "text", "text": `still: call generation_result with id "img_1" again`}}}
		}
		return map[string]any{
			"content": []any{map[string]any{"type": "text", "text": "made"}},
			"_meta":   map[string]any{gateway.FilesMeta: []any{map[string]any{"url": "https://uni-switch.example.net/v1/videos/img_1/content"}}},
		}
	}
	out, err := runOut(t, "tool", "generate_image", "a kite", "-o", "clips")
	if err != nil {
		t.Fatal(err)
	}
	c := f.toolCalls()
	if len(c) != 3 || !strings.Contains(c[1], `generation_result {"id":"img_1"}`) || !strings.Contains(c[2], `generation_result {"id":"img_1"}`) {
		t.Fatalf("calls %v", c)
	}
	f.mu.Lock()
	gets := f.gets
	f.mu.Unlock()
	if len(gets) != 1 || gets[0] != "/v1/videos/img_1/content" {
		t.Fatalf("downloaded %v", gets)
	}
	saved := filesIn(t, filepath.Join(dir, "clips"))
	if len(saved) != 1 || filepath.Ext(saved[0]) != ".mp4" || !strings.Contains(out, "saved") {
		t.Fatalf("saved %v, printed %q", saved, out)
	}
}

// A tool's error is the command's, and nothing is saved.
func TestToolErrorIsSaid(t *testing.T) {
	f, dir := toolHere(t)
	f.call = func(string, map[string]any) map[string]any {
		return map[string]any{"content": []any{map[string]any{"type": "text", "text": "no image model is set"}}, "isError": true}
	}
	if _, err := runOut(t, "tool", "web_search", "go"); err == nil || err.Error() != "no image model is set" {
		t.Fatalf("err %v", err)
	}
	if len(filesIn(t, dir)) != 0 {
		t.Fatal("saved")
	}
}

// With no uni-switch running it says so, plainly.
func TestToolSaysUniSwitchIsntRunning(t *testing.T) {
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := l.Addr().String()
	l.Close()
	t.Setenv("UNI_SWITCH_GATEWAY", "http://"+addr)
	for _, args := range [][]string{{"tools"}, {"tool", "web_search", "go"}} {
		if _, err := runOut(t, args...); err == nil || !strings.Contains(err.Error(), "uni-switch isn't running at http://"+addr) {
			t.Fatalf("uni-switch %s: %v", strings.Join(args, " "), err)
		}
	}
}

// Against the real gateway, with a fake vendor: the image server's own
// tools and schema are what uni-switch tool lists and calls, and the image the
// vendor drew is saved.
func TestToolDrawsThroughTheGateway(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	t.Setenv("UNI_SWITCH_PUBLIC_URL", "")
	pic := tinyPNG(t)
	var sent []string
	var mu sync.Mutex
	up := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		sent = append(sent, r.URL.Path+" "+string(b))
		mu.Unlock()
		if r.URL.Path != "/v1/images/generations" {
			http.NotFound(w, r)
			return
		}
		json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"b64_json": base64.StdEncoding.EncodeToString(pic)}}})
	}))
	t.Cleanup(up.Close)
	if err := provider.Save(provider.Provider{ID: "art", Name: "Art", Chat: up.URL + "/v1", Key: "key", Models: []string{"text", "gpt-image-1"}}); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive("art", up.URL+"/v1", []catalog.Model{{ID: "text"}}); err != nil {
		t.Fatal(err)
	}
	gw := httptest.NewServer(gateway.New().Handler())
	t.Cleanup(gw.Close)
	t.Setenv("UNI_SWITCH_GATEWAY", gw.URL)
	dir := t.TempDir()
	t.Chdir(dir)
	out, err := runOut(t, "tools")
	if err != nil || !strings.Contains(out, "generate_image") || !strings.Contains(out, "--prompt <text> (required)") || !strings.Contains(out, "analyze_image") || !strings.Contains(out, "web_search") {
		t.Fatalf("tools: %v\n%s", err, out)
	}
	out, err = runOut(t, "tool", "generate_image", "a uni-switch", "--size", "1024x1024", "-o", "hero.png")
	if err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
	if got, err := os.ReadFile(filepath.Join(dir, "hero.png")); err != nil || !bytes.Equal(got, pic) {
		t.Fatalf("hero.png: %v (printed %q)", err, out)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(sent) != 1 || !strings.Contains(sent[0], `"prompt":"a uni-switch"`) || !strings.Contains(sent[0], `"size":"1024x1024"`) {
		t.Fatalf("vendor was sent %v", sent)
	}
}
