package provider

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"
)

// An id given up by a rename and taken by another provider later keeps
// the calls recorded under it before then with the one renamed, and
// gives the new one only its own (cybercaius: a renamed provider's usage
// was shown under both ids).
func TestRenamesByWhenTheIDWasTaken(t *testing.T) {
	isolate(t)
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_CACHE_HOME", t.TempDir())
	if err := Save(Provider{ID: "deepseek", Name: "DeepSeek", Key: "k", Chat: "http://127.0.0.1:1/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if err := Rename("deepseek", "ds"); err != nil {
		t.Fatal(err)
	}
	before := time.Now().Add(-time.Hour)
	if r := UsageRenames(); r.Of("deepseek", before) != "ds" || r.Of("deepseek", time.Now()) != "ds" || r.Timed("deepseek") {
		t.Fatalf("renamed: %+v", r)
	}
	// one added again from the list is not given the old id
	if got := FreeID("deepseek"); got != "deepseek-2" {
		t.Fatalf("free id %q", got)
	}
	// one saved under it on purpose takes it from now on
	if err := Save(Provider{ID: "deepseek", Name: "DeepSeek again", Key: "k2", Chat: "http://127.0.0.1:2/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if p, _ := Find("ds"); len(p.Was) != 0 || len(p.Gave) != 1 || p.Gave[0].ID != "deepseek" {
		t.Fatalf("ds %+v", p)
	}
	r := UsageRenames()
	if got := r.Of("deepseek", before); got != "ds" {
		t.Fatalf("a call before the id was taken went to %q", got)
	}
	if got := r.Of("deepseek", time.Now().Add(time.Second)); got != "deepseek" {
		t.Fatalf("a call after went to %q", got)
	}
	if got := Renamed(); len(got) != 0 {
		t.Fatalf("refs renamed %v", got)
	}
	if got := r.Each("deepseek"); !slices.Equal(got, []string{"deepseek", "ds"}) {
		t.Fatalf("each %v", got)
	}

	// a rename onto it hands it over the same way
	if err := Rename("deepseek", "ds2"); err != nil {
		t.Fatal(err)
	}
	mid := time.Now()
	if err := Save(Provider{ID: "other", Name: "Other", Key: "k3", Chat: "http://127.0.0.1:3/v1", Models: []string{"m"}}); err != nil {
		t.Fatal(err)
	}
	if err := Rename("other", "deepseek"); err != nil {
		t.Fatal(err)
	}
	r = UsageRenames()
	if r.Of("deepseek", before) != "ds" || r.Of("deepseek", mid) != "ds2" || r.Of("deepseek", time.Now().Add(time.Second)) != "deepseek" || r.Of("other", before) != "deepseek" {
		t.Fatalf("%q %q %q %q", r.Of("deepseek", before), r.Of("deepseek", mid), r.Of("deepseek", time.Now().Add(time.Second)), r.Of("other", before))
	}

	// a providers.json that can't be read is not "nothing renamed"
	if err := os.WriteFile(Path(), []byte("{not json"), 0o600); err != nil {
		t.Fatal(err)
	}
	matches, _ := filepath.Glob(Path() + "*")
	for _, m := range matches {
		if m != Path() {
			os.Remove(m)
		}
	}
	if _, err := read(); err == nil {
		t.Fatal("the broken file was read")
	}
	if got := UsageRenames(); got.Of("deepseek", mid) != "ds2" || got.Of("other", before) != "deepseek" {
		t.Fatalf("unreadable: %+v", got)
	}
}

// A host's site is its registrable domain whatever its subdomain, case or
// port, and none for an address, a bare name or a tenant of a hosting
// platform's suffix (CyberCaius: leee.baby and api.leee.baby are one).
func TestSiteOf(t *testing.T) {
	for host, want := range map[string]string{
		"leee.baby": "leee.baby", "API.Leee.Baby": "leee.baby", "www.leee.baby:443": "leee.baby",
		"api.example.co.uk": "example.co.uk", "mine.vercel.app": "mine.vercel.app",
		"127.0.0.1:8080": "", "[::1]:8": "", "localhost:3000": "", "": "",
	} {
		if got := SiteOf(host); got != want {
			t.Errorf("SiteOf(%q) = %q, want %q", host, got, want)
		}
	}
}
