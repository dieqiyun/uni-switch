package gui

import (
	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// replaceKey puts a new value in place of one of a provider's keys
// (#1869), keeping its name, place and settings (provider.ReplaceKey).
// A gateway key held to the key is held to it still (access.ReplaceAccount),
// and the key's rests are lifted, as its new value is yet to be refused:
// the ones it had by its old fingerprint, and the provider's own while it
// was the one key on (provider.RestKey).
func replaceKey(id, ref, key string) error {
	p, err := provider.Find(id)
	if err != nil {
		return err
	}
	var rest []string
	for _, k := range p.KeyList() {
		if k.ID == ref {
			rest = append(rest, p.ID+"#"+ref)
			if r := p.RestKey(k); k.On && r != rest[0] {
				rest = append(rest, r)
			}
		}
	}
	was, now, err := provider.ReplaceKey(id, ref, key)
	if err != nil {
		return err
	}
	if _, err := access.ReplaceAccount(p.ID, was, now); err != nil {
		return err
	}
	liftRests(rest)
	return nil
}

// liftRests lifts every rest of each key named, in the gateway this uni-switch
// serves; one served by another uni-switch keeps them. Swapped in tests.
var liftRests = func(keys []string) {
	if gw := served.Load(); gw != nil {
		for _, k := range keys {
			gw.UnrestAll(k)
		}
	}
}
