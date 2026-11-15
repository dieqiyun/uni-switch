package provider

import (
	"testing"
)

// Ticking no level of a model whose levels are known says it takes no
// reasoning field at all (yuexian7: a model models.dev lists as reasoning
// that answers any reasoning parameter with 422). Ticking levels again, or
// Restore default, takes that back; the two settings never both hold.
func TestModelReasonless(t *testing.T) {
	effortHome(t)
	if err := SetModelEfforts("a/m", []string{"high"}); err != nil {
		t.Fatal(err)
	}
	if err := SetModelReasonless("a/m", true); err != nil {
		t.Fatal(err)
	}
	if !Reasonless("a", "m") || Reasonless("a", "n") {
		t.Fatal("reasonless not kept for a/m alone")
	}
	if got := (Provider{ID: "a"}).ModelEfforts()["m"]; got != nil {
		t.Fatalf("its kept levels stayed: %v", got)
	}
	if err := SetModelEfforts("a/m", []string{"low", "high"}); err != nil {
		t.Fatal(err)
	}
	if Reasonless("a", "m") {
		t.Fatal("ticking levels left it reasonless")
	}
	if err := SetModelReasonless("a/m", true); err != nil {
		t.Fatal(err)
	}
	if err := SetModelEfforts("a/m", nil); err != nil { // Restore default
		t.Fatal(err)
	}
	if Reasonless("a", "m") {
		t.Fatal("Restore default left it reasonless")
	}
}
