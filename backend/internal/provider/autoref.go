package provider

// The id agents are given for a group uni-switch found (ZeroClover on X): "auto/<model>",
// the model's own id as a vendor spells it (auto/claude-opus-5.5), so that a
// client reading the part after the slash as a model id — as models.dev keys
// its models — knows the model. It was "group/auto-<slug>" (Slug's, dots
// dashed), which nothing outside uni-switch could read, and that id is still
// served as it was, forever: agents' configs, gateway keys, saved groups and
// rules hold it.
//
// Inside uni-switch a found group stays "group/auto-<slug>": providers.json's
// members, rules and classifiers, the gateway's routing, a key's limits and
// the usage log keep that one id, so a group is counted under one name
// whichever an agent sent (CanonicalRef, taken back in the gateway before
// anything reads the request). The catalog lists the group by its public id
// (Group.Ref), so every list and config that is built from the catalog —
// /v1/models, the model lists written into agents, the pickers — names it so.
//
// "auto" is not a provider's id: Save and Rename refuse it, and a plugin
// that calls its provider auto is auto-plugin (PluginID). One of the user's
// own saved as "auto" before keeps its models: while it is there, auto/<x>
// is its model x, and found groups are listed as group/auto-<slug> as before
// (autoRefsOn).

import (
	"errors"

	"strings"
)

// AutoPrefix starts the id agents are given for a group uni-switch found:
// "auto/<model>".
const AutoPrefix = "auto/"

// autoID is the provider id that would take auto/<model> for its own.
const autoID = "auto"

// errAutoID refuses "auto" as a new provider's id: auto/<model> names the
// groups uni-switch finds.
var errAutoID = errors.New(`"auto" is reserved for retired model references; pick another id`)

func IsGroupRef(ref string) bool {
	return strings.HasPrefix(ref, GroupPrefix) || strings.HasPrefix(ref, AutoPrefix) && !customIDs()[autoID]
}

func CanonicalRef(ref string) string { return ref }

func PublicRef(ref string) string { return ref }

// lastPart is a model's id without a vendor's prefix: what follows its
// last slash, trimmed.
func lastPart(m string) string {
	m = strings.TrimSpace(m)
	if i := strings.LastIndex(m, "/"); i >= 0 {
		m = m[i+1:]
	}
	return m
}
