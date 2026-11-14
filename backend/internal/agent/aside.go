package agent

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/proc"
)

func aside(home string) *Agent { return asideIn(here(home)) }

func asideIn(at place) *Agent {
	c := newAsideConnection(at)
	a := &Agent{ID: "aside", Name: "Aside", Icon: "aside", Bin: "aside", Path: c.path, Spelled: prefixed, Dir: at.join(".aside"), Sync: c.sync}
	a.Native = &NativeConnection{Read: c.state, Connect: c.connect, Apply: c.apply, Stage: c.stage, Disconnect: c.plan, Execute: c.execute, ExecuteOffline: c.executeOffline}
	a.Check = func() string {
		status, detail := c.provider()
		if status == "invalid" {
			return detail
		}
		return ""
	}
	a.Joined = func() bool { status, _ := c.provider(); return status == "connected" }
	for _, key := range append([]string{"model", "effort", "image"}, asideRoles...) {
		label := key
		if key == "effort" {
			label = "thinking"
		}
		f := Field{Key: key, Label: label, Quiet: key != "model" && key != "effort", Get: func() string { return c.value(key) }, Set: func(v string) error { return c.apply(key, v) }}
		if f.Quiet && key != "image" {
			f.Follows = "model"
		}
		f.Options = func(cur map[string]string) []Option {
			if key == "effort" {
				if offered := piOffered("aside", cur["model"]); offered != nil {
					return static(offered...)
				}
				return static(piLevels...)
			}
			if key == "image" {
				opts := asideImageOptions()
				if v := cur[key]; v != "" {
					found := false
					for _, o := range opts {
						if o.Value == v {
							found = true
						}
					}
					if !found {
						opts = append([]Option{{Value: v, Note: "current image selection"}}, opts...)
					}
				}
				return opts
			}
			return withVia(asideOwnOptions(c.models, cur[key]), "aside", "uni-switch/")
		}
		a.Fields = append(a.Fields, f)
	}
	// which of Aside's accounts uni-switch configures: empty follows the one
	// Aside has active (#1499)
	a.Fields = append(a.Fields, Field{Key: "account", Label: "account", Quiet: true,
		Get: func() string {
			if id, ok := asidePicked(); ok {
				return asideAccountName(id)
			}
			return ""
		},
		Set:     c.pickAccount,
		Options: func(cur map[string]string) []Option { return asideAccountOptions(at, cur["account"]) },
	})
	return a
}

var asideRoles = []string{"fast", "standard", "deep", "visual"}

const asideImageKey = "imageGenerationModel"

// asideDir is the folder of the Aside account uni-switch configures
// (asideChoice).
func asideDir(at place) string { return asideAccountDir(at, asideChoice(at)) }

var asideTimeout = 10 * time.Second

const asideOK = "UNI_SWITCH_ASIDE_OK"

var asideSet = func(account, expr string) error {
	ctx, cancel := context.WithTimeout(context.Background(), asideTimeout)
	defer cancel()
	out, err := proc.CommandContext(ctx, "aside", "repl", "--account", account, "--host", "local", expr).Output()
	if err != nil {
		return fmt.Errorf("Aside did not accept the settings change: %w", err)
	}
	if !bytes.Contains(out, []byte(asideOK)) {
		return fmt.Errorf("Aside did not confirm the settings change")
	}
	return nil
}
