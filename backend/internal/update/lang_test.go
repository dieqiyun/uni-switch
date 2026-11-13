package update

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

const (
	enNotes  = "### Features\n\n- One thing (#1)\n\n### Install\n\nDownload it."
	zhNotes  = "### 新功能\n\n- 一件事 (#1)"
	twoNotes = enNotes + "\n\n<!-- lang:zh -->\n\n" + zhNotes + "\n"
)

// A release's notes are English, then Chinese below the marker (freecss on
// Discord: What's new should follow Settings' language). Notes from before
// are English alone, in any language.
func TestInLang(t *testing.T) {
	for _, c := range []struct{ md, lang, want string }{
		{twoNotes, "zh", zhNotes},
		{twoNotes, "en", enNotes},
		{twoNotes, "", enNotes},
		{"- old\n", "zh", "- old\n"},
		{enNotes + "\n<!-- lang:zh -->\n  \n", "zh", enNotes}, // no Chinese after all
		{zhNotes, "zh", zhNotes},                              // the site cut them already
	} {
		if got := InLang(c.md, c.lang); got != c.want {
			t.Errorf("InLang(%q, %q) = %q, want %q", c.md, c.lang, got, c.want)
		}
	}
}

// The feed and the notes are asked in the language, and whatever comes
// back whole (a site from before it cut them) is cut here.
func TestFeedsAskInLang(t *testing.T) {
	var asked []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asked = append(asked, r.URL.Path+"?"+r.URL.RawQuery)
		if r.URL.Path == "/notes" {
			json.NewEncoder(w).Encode(map[string]any{"releases": []Note{{Version: "0.1.604", Notes: twoNotes}, {Version: "0.1.603", Notes: "- three"}}})
			return
		}
		json.NewEncoder(w).Encode(Release{Version: "0.1.604", Notes: twoNotes})
	}))
	defer srv.Close()
	t.Setenv("UNI_SWITCH_UPDATE_FEED", srv.URL+"/latest")
	t.Setenv("UNI_SWITCH_NOTES_FEED", srv.URL+"/notes")
	ctx := context.Background()

	rel, err := LatestIn(ctx, "zh")
	if err != nil || rel.Notes != zhNotes {
		t.Fatalf("zh: %+v %v", rel, err)
	}
	if rel, err = Latest(ctx); err != nil || rel.Notes != enNotes {
		t.Fatalf("no lang: %+v %v", rel, err)
	}
	notes, err := NotesBetween(ctx, "0.1.602", "0.1.604", "zh")
	if err != nil || len(notes) != 2 || notes[0].Notes != zhNotes || notes[1].Notes != "- three" {
		t.Fatalf("notes: %+v %v", notes, err)
	}
	want := []string{"/latest?lang=zh", "/latest?", "/notes?after=0.1.602&lang=zh&upto=0.1.604"}
	if len(asked) != len(want) {
		t.Fatalf("asked %q", asked)
	}
	for i := range want {
		if asked[i] != want[i] {
			t.Errorf("asked %q, want %q", asked[i], want[i])
		}
	}
}
