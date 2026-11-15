package catalog

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"
)

// OpenCode Go's /models lists what a model takes as media types
// ("image/jpeg", "video/mp4", "application/pdf"), not models.dev's words.
// Read word for word there was no "image" in them, so every Go model that
// sees was taken for text-only, its list's answer over models.dev's
// (zeo9901 on Discord: no Go model took an image). The rows are Go's own,
// as its list answered on 2026-10-31.
func TestOpenCodeGoMediaTypesSee(t *testing.T) {
	body, err := os.ReadFile("testdata/opencode-go-models-2026-10-31.json")
	if err != nil {
		t.Fatal(err)
	}
	srv := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		rw.Write(body)
	}))
	defer srv.Close()
	ms, _, err := FetchAt(context.Background(), srv.URL+"/v1", "k", false, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]struct {
		images bool
		in     []string
	}{
		"deepseek-v4.1-flash": {true, []string{"text", "image"}},
		"kimi-k3":             {true, []string{"text", "image", "video"}},
		"gemini-4-argon":      {true, []string{"text", "image", "video", "audio", "pdf"}},
		"glm-5.2":             {false, []string{"text"}},
	}
	if len(ms) != len(want) {
		t.Fatalf("%d models, want %d: %+v", len(ms), len(want), ms)
	}
	for _, m := range ms {
		w, ok := want[m.ID]
		if !ok {
			t.Errorf("unexpected %s", m.ID)
			continue
		}
		if m.ImageInput == nil || *m.ImageInput != w.images || m.Images != w.images {
			t.Errorf("%s: Images %v ImageInput %v, want %v", m.ID, m.Images, m.ImageInput, w.images)
		}
		if !slices.Equal(m.InputModalities, w.in) {
			t.Errorf("%s: input %v, want %v", m.ID, m.InputModalities, w.in)
		}
		if !slices.Equal(m.OutputModalities, []string{"text"}) {
			t.Errorf("%s: output %v", m.ID, m.OutputModalities)
		}
	}
}

func TestKinds(t *testing.T) {
	for _, c := range []struct{ in, want []string }{
		{nil, nil},
		{[]string{}, []string{}},
		{[]string{"text", "image"}, []string{"text", "image"}},
		{[]string{"text", "image/png", "IMAGE/JPEG", "audio/wav", "video/mp4", "application/pdf", "text/plain"}, []string{"text", "image", "audio", "video", "pdf"}},
		{[]string{"text", "file", "application/zip"}, []string{"text", "file", "application/zip"}},
	} {
		if got := Kinds(c.in); !slices.Equal(got, c.want) || (got == nil) != (c.want == nil) {
			t.Errorf("Kinds(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A list saved before Kinds, its media types kept as Go gave them and its
// models taken for text-only, is read as seeing from the first read after
// the update, not only once it is fetched again.
func TestSavedMediaTypesListSees(t *testing.T) {
	no := false
	saved := []Model{
		{ID: "kimi-k3", ImageInput: &no, InputModalities: []string{"text", "image/jpeg", "image/png", "video/mp4"}, OutputModalities: []string{"text"}},
		{ID: "glm-5.2", ImageInput: &no, InputModalities: []string{"text"}, OutputModalities: []string{"text"}},
	}
	if err := SaveLive("opencode-go-saved", "https://opencode.ai/zen/go/v1", saved); err != nil {
		t.Fatal(err)
	}
	k := LiveByID("opencode-go-saved", "kimi-k3")
	if len(k) != 1 || k[0].ImageInput == nil || !*k[0].ImageInput || !k[0].Images || !slices.Equal(k[0].InputModalities, []string{"text", "image", "video"}) {
		t.Errorf("kimi-k3 read back as %+v", k)
	}
	g := LiveByID("opencode-go-saved", "glm-5.2")
	if len(g) != 1 || g[0].ImageInput == nil || *g[0].ImageInput || g[0].Images {
		t.Errorf("glm-5.2 read back as %+v", g)
	}
}
