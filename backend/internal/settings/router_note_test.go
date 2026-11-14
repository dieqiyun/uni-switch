package settings

import (
	"strings"
	"testing"
)

// The MCP router's note is kept one line and short, and saved alone
// beside the rest of the settings.
func TestRouterNote(t *testing.T) {
	if got := CleanRouterNote("  Los\tAngeles\n(US) "); got != "Los Angeles (US)" {
		t.Fatalf("cleaned %q", got)
	}
	if got := CleanRouterNote(strings.Repeat("洛", RouterNoteMost+5)); got != strings.Repeat("洛", RouterNoteMost) {
		t.Fatalf("not cut at %d characters: %d", RouterNoteMost, len([]rune(got)))
	}
	if err := Save(Settings{Theme: "dark"}); err != nil {
		t.Fatal(err)
	}
	if err := SetRouterNote(" Los Angeles "); err != nil {
		t.Fatal(err)
	}
	if s := Load(); s.RouterNote != "Los Angeles" || s.Theme != "dark" {
		t.Fatalf("saved %+v", s)
	}
	if err := SetRouterNote(""); err != nil {
		t.Fatal(err)
	}
	if s := Load(); s.RouterNote != "" {
		t.Fatalf("not cleared: %q", s.RouterNote)
	}
	if err := SetRouterAllTools(true); err != nil {
		t.Fatal(err)
	}
	if s := Load(); !s.RouterAllTools || s.Theme != "dark" {
		t.Fatalf("saved %+v", s)
	}
}
