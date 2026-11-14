package sessions

import (
	"path/filepath"
	"testing"
)

func TestTranscriptOpenCodeV2(t *testing.T) {
	data, _ := setupMore(t)
	db := ocMakeV2(t, filepath.Join(data, "opencode", "opencode.db"))
	defer db.Close()
	s := find(t, List(All), "opencode", ocMain)
	tr, err := TranscriptOf(s)
	if err != nil || len(tr.Parts) != 4 {
		t.Fatalf("OpenCode conversation parts=%d error=%v", len(tr.Parts), err)
	}
	if tr.Parts[0].Role != "user" || tr.Parts[3].Text != "It races the clock." {
		t.Fatalf("wrong conversation order: %+v", tr)
	}
}

func TestTranscriptOpenCodeFiles(t *testing.T) {
	setupMore(t)
	s := find(t, List(All), "opencode", ocMain)
	tr, err := TranscriptOf(s)
	if err != nil || len(tr.Parts) == 0 {
		t.Fatalf("legacy OpenCode conversation parts=%d error=%v", len(tr.Parts), err)
	}
}
