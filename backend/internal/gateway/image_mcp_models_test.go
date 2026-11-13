package gateway

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/access"
)

// mcpModelProp is the model argument tools/list gives tool, for the key
// in hdr.
func mcpModelProp(t *testing.T, h http.Handler, addr, tool string, hdr ...string) (enum []string, desc string) {
	t.Helper()
	var list struct {
		Result struct {
			Tools []struct {
				Name        string
				InputSchema struct {
					Properties map[string]struct {
						Enum        []string
						Description string
					}
				}
			}
		}
	}
	w := imagePost(h, addr, `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`, hdr...)
	json.Unmarshal(w.Body.Bytes(), &list)
	for _, tl := range list.Result.Tools {
		if tl.Name == tool {
			p, ok := tl.InputSchema.Properties["model"]
			if !ok {
				t.Fatalf("%s takes no model: %s", tool, w.Body.String())
			}
			return p.Enum, p.Description
		}
	}
	t.Fatalf("no %s: %s", tool, w.Body.String())
	return nil, ""
}

// generate_image takes the model to draw with (XYenon on Discord): one the
// schema lists, by its whole id or a bare one, is the one drawn with; left
// out, Settings' is; one it doesn't list is an error naming those it
// does, and nothing is drawn.
func TestImageMCPDrawsWithTheModelAsked(t *testing.T) {
	s, e := easeled(t)
	t.Cleanup(s.imageStore.running.Wait)
	h := localGuard(s.Handler())
	const here = "127.0.0.1:5000"
	enum, desc := mcpModelProp(t, h, here, GenerateImageTool)
	if !slices.Contains(enum, "art/gpt-image-1") || !slices.Contains(enum, "art/painter-image-preview") || slices.Contains(enum, "art/text") || enum[0] != Drawer() {
		t.Fatalf("enum %v (default %s)", enum, Drawer())
	}
	if !strings.Contains(desc, "now "+Drawer()) {
		t.Fatalf("description %q", desc)
	}
	// left out: the default, at the images API
	m := readImageReply(t, imagePost(h, here, imageToolCall(GenerateImageTool, map[string]any{"prompt": "a uni-switch"})))
	if m.Result.IsError || len(e.got("/v1/images/generations")) != 1 || len(e.got("/v1/chat/completions")) != 0 {
		t.Fatalf("default: %+v %v", m, e.got("/v1/chat/completions"))
	}
	// asked by its whole id, then by its bare one: drawn with it
	for i, model := range []string{"art/painter-image-preview", "painter-image-preview"} {
		m = readImageReply(t, imagePost(h, here, imageToolCall(GenerateImageTool, map[string]any{"prompt": "a uni-switch", "model": model})))
		if sent := e.got("/v1/chat/completions"); m.Result.IsError || len(sent) != i+1 || !strings.Contains(sent[i], "painter-image-preview") || !strings.Contains(m.Result.Content[0].Text, "art/painter-image-preview") {
			t.Fatalf("%s: %+v, sent %v", model, m, sent)
		}
	}
	if n := len(e.got("/v1/images/generations")); n != 1 {
		t.Fatalf("the default drew %d times", n)
	}
	// unknown, or not an image model: an error naming those there are
	for _, model := range []string{"art/nope", "art/text", "nope"} {
		m = readImageReply(t, imagePost(h, here, imageToolCall(GenerateImageTool, map[string]any{"prompt": "a uni-switch", "model": model})))
		if txt := m.Result.Content[0].Text; !m.Result.IsError || !strings.Contains(txt, `"`+model+`"`) || !strings.Contains(txt, "art/gpt-image-1") || !strings.Contains(txt, "art/painter-image-preview") {
			t.Fatalf("%s: %+v", model, m)
		}
	}
	if len(e.got("/v1/images/generations")) != 1 || len(e.got("/v1/chat/completions")) != 2 {
		t.Fatalf("an unknown model drew: %v %v", e.got("/v1/images/generations"), e.got("/v1/chat/completions"))
	}
	// the stdio server asks the same
	ask := func(model string) mediaReply {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 9, "method": MediaModelMethod, "params": map[string]any{"tool": GenerateImageTool, "model": model}})
		return readMCPRaw(t, imagePost(h, here, string(b)))
	}
	if r := ask("painter-image-preview"); r.Error != nil || !strings.Contains(string(r.Result), `"art/painter-image-preview"`) {
		t.Fatalf("media_model: %+v", r)
	}
	if r := ask("art/nope"); r.Error == nil || !strings.Contains(r.Error.Message, "art/gpt-image-1") {
		t.Fatalf("media_model, unknown: %+v", r)
	}
}

type mediaReply struct {
	Result json.RawMessage
	Error  *struct {
		Code    int
		Message string
	}
}

func readMCPRaw(t *testing.T, w *httptest.ResponseRecorder) mediaReply {
	t.Helper()
	var r mediaReply
	res := w.Result()
	defer res.Body.Close()
	if err := json.NewDecoder(res.Body).Decode(&r); err != nil {
		t.Fatal(err)
	}
	return r
}

// A gateway key held to some models is held to them here too: the schema
// lists only those, one it may not use is refused by the images API's own
// check, and nothing is drawn.
func TestImageMCPModelHeldToTheKey(t *testing.T) {
	s, e := easeled(t)
	t.Cleanup(s.imageStore.running.Wait)
	keys, secrets := newCaller(t, "Narrow")
	if _, err := access.Update("models-key", access.Change{Key: keys[0].ID, Models: []string{Drawer()}}); err != nil {
		t.Fatal(err)
	}
	h := localGuard(s.Handler())
	const there = "192.0.2.7:4000"
	auth := []string{"Authorization", "Bearer " + secrets[0]}
	if enum, _ := mcpModelProp(t, h, there, GenerateImageTool, auth...); len(enum) != 1 || enum[0] != Drawer() {
		t.Fatalf("the key's enum %v", enum)
	}
	m := readImageReply(t, imagePost(h, there, imageToolCall(GenerateImageTool, map[string]any{"prompt": "a uni-switch", "model": "art/painter-image-preview"}), auth...))
	if txt := m.Result.Content[0].Text; !m.Result.IsError || !strings.Contains(txt, "may not use") || !strings.Contains(txt, `"Narrow"`) {
		t.Fatalf("a model the key may not use: %+v", m)
	}
	m = readImageReply(t, imagePost(h, there, imageToolCall(GenerateImageTool, map[string]any{"prompt": "a uni-switch", "model": "art/nope"}), auth...))
	if txt := m.Result.Content[0].Text; !m.Result.IsError || !strings.Contains(txt, Drawer()) || strings.Contains(txt, "painter") {
		t.Fatalf("unknown, for the key: %+v", m)
	}
	if n := len(e.got("/v1/chat/completions")) + len(e.got("/v1/images/generations")); n != 0 {
		t.Fatalf("drew %d times", n)
	}
	// the one it may use draws
	m = readImageReply(t, imagePost(h, there, imageToolCall(GenerateImageTool, map[string]any{"prompt": "a uni-switch", "model": Drawer()}), auth...))
	if m.Result.IsError || len(e.got("/v1/images/generations")) != 1 {
		t.Fatalf("the key's model: %+v", m)
	}
}
