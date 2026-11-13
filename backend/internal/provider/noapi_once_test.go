package provider

import (
	"fmt"
	"slices"
	"testing"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// Which models a provider can't be asked for (#2094) is weighed for each
// model of a list, and the APIs the provider has a URL for are found once
// for the list, not once for each model.
func TestNoAPIFindsTheProvidersAPIsOnce(t *testing.T) {
	oneHome(t)
	p := Provider{ID: "relay", Name: "Relay", Key: "sk-x", Chat: "http://127.0.0.1:1/v1"}
	var ms []catalog.Model
	var chat []string
	for i := range 100 {
		m := catalog.Model{ID: fmt.Sprintf("m%02d", i), APIs: []string{"anthropic"}}
		if i%2 == 0 {
			m.APIs = []string{"chat"}
			chat = append(chat, m.ID)
		}
		ms = append(ms, m)
	}
	if err := Save(p); err != nil {
		t.Fatal(err)
	}
	if err := catalog.SaveLive(p.ID, p.Chat, ms); err != nil {
		t.Fatal(err)
	}
	before := speaksLooked.Load()
	var kept []string
	for _, m := range p.servable(ms) {
		kept = append(kept, m.ID)
	}
	// once, give or take a look another test left running takes
	if n := speaksLooked.Load() - before; n > 5 {
		t.Fatalf("100 models looked up the provider's APIs %d times", n)
	}
	if !slices.Equal(kept, chat) {
		t.Fatalf("kept %v, want the models listed on chat %v", kept, chat)
	}
	for _, m := range ms {
		if p.NoAPIFor(m.ID) != !slices.Contains(chat, m.ID) {
			t.Fatalf("NoAPIFor(%s) = %v", m.ID, p.NoAPIFor(m.ID))
		}
	}
}
