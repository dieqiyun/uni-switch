package gui

import (
	"fmt"
	"path/filepath"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// @NetRozee on X: a custom endpoint given a thousand keys froze uni-switch.
// The Providers page asked, for each key, what it rests by, and that
// listed every key on again: a thousand keys took 1.5s to draw, five
// thousand 2m40s, and each answer of /api/keys/* (an import too) waited
// as long. The listings are counted, not timed: a provider's row lists
// its keys as often with a thousand keys as with ten.
func TestProviderInfoListsKeysOnce(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	lists := func(n int) int64 {
		p := provider.Provider{ID: fmt.Sprintf("relay%d", n), Name: "Relay", Chat: "http://127.0.0.1:1/v1", Key: "sk-first"}
		for i := range n {
			p.Keys = append(p.Keys, provider.KeyAccount{Key: fmt.Sprintf("sk-key-%d-%04d", n, i)})
		}
		if err := provider.Save(p); err != nil {
			t.Fatal(err)
		}
		q, err := provider.Find(p.ID)
		if err != nil {
			t.Fatal(err)
		}
		providerInfo(*q, nil)
		before := provider.KeysListed()
		info := providerInfo(*q, nil)
		if len(info.KeyList) != n+1 {
			t.Errorf("%d keys listed, want %d", len(info.KeyList), n+1)
		}
		return provider.KeysListed() - before
	}
	few, many := lists(10), lists(1000)
	t.Logf("keys listed %d times for 10 keys, %d for 1000", few, many)
	if many > few+2 {
		t.Errorf("a provider's row listed its keys %d times with 1000 keys, %d with 10: once a key", many, few)
	}
}
