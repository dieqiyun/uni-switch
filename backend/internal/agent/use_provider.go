package agent

import (
	"errors"
	"fmt"
	"strings"
	"sync"

	"github.com/dieqiyun/uni-switch/backend/internal/gateway"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

var usingProvider sync.Mutex

// ProviderModels keeps the Use picker independent of the previous provider's
// catalog restriction, so switching to another provider remains possible.
func (a *Agent) ProviderModels(id string) []Option {
	out := []Option{}
	off := provider.ModelOff(a.ListsFor())
	for _, e := range provider.CatalogOf(a.ListsFor()) {
		if e.Provider.ID == id && !off(e.ID) {
			out = append(out, Option{Value: e.ID, Ref: e.ID, Label: e.Label(), Context: e.Context})
		}
	}
	return out
}

// UseProvider applies an explicit provider/model choice. Claude Code's page
// configures its CLI and Desktop adapters together; Desktop needs its own
// third-party deployment profile and tier aliases, not only CLI environment.
func (a *Agent) UseProvider(id, model string) (err error) {
	usingProvider.Lock()
	defer usingProvider.Unlock()
	valid := false
	for _, o := range a.ProviderModels(id) {
		if o.Ref == model {
			valid = true
			break
		}
	}
	if !valid {
		return fmt.Errorf("%s: select an enabled model from provider %s", a.Name, id)
	}
	targets := []*Agent{a}
	if a.ID == "claude" || a.ID == "claude-desktop" {
		other := "claude-desktop"
		if a.ID == other {
			other = "claude"
		}
		twin, findErr := Find(other)
		if findErr != nil {
			return findErr
		}
		targets = append(targets, twin)
	}
	type before struct {
		a        *Agent
		provider string
		wired    bool
		values   map[string]string
	}
	var saved []before
	defer func() {
		if err == nil {
			return
		}
		for i := len(saved) - 1; i >= 0; i-- {
			old := saved[i]
			err = errors.Join(err, provider.SetAgentProvider(old.a.ListsFor(), old.provider))
			if !old.wired {
				err = errors.Join(err, old.a.Disconnect())
			} else {
				for _, f := range old.a.Fields {
					if f.Get() != old.values[f.Key] {
						err = errors.Join(err, old.a.Apply(f.Key, old.values[f.Key]))
					}
				}
				if old.a.Sync != nil {
					err = errors.Join(err, old.a.Sync())
				}
			}
		}
	}()
	for _, target := range targets {
		saved = append(saved, before{target, provider.AgentProvider(target.ListsFor()), target.Wired(), target.Values()})
		if err = provider.SetAgentProvider(target.ListsFor(), id); err != nil {
			return err
		}
		apply := func(key, value string) error {
			spelled, e := target.Spell(key, value)
			if e == nil {
				e = target.Pick(key, spelled)
			}
			if e != nil {
				return fmt.Errorf("%s: %w", target.Name, e)
			}
			return nil
		}
		if target.ID == "claude-desktop" {
			if err = apply("provider", uniSwitchID); err != nil {
				return err
			}
			for _, tier := range gateway.DesktopTierNames {
				if err = apply(tier, model); err != nil {
					return err
				}
			}
			if err = apply("subagent_model", model); err != nil {
				return err
			}
		} else if target.Field("model") != nil {
			if err = apply("model", model); err != nil {
				return err
			}
			// Explicit overrides from the previous provider must follow the
			// new main model instead of keeping a different provider alive.
			for _, f := range target.Fields {
				if f.Follows == "model" && strings.TrimSpace(f.Get()) != "" {
					if err = apply(f.Key, ""); err != nil {
						return err
					}
				}
			}
		} else if err = target.Connect(); err != nil {
			return err
		}
		if target.Sync != nil {
			if err = target.Sync(); err != nil {
				return fmt.Errorf("%s: %w", target.Name, err)
			}
		}
	}
	return nil
}
