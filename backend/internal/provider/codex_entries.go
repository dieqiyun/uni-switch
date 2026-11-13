package provider

import (
	"context"
	"net/url"
	"slices"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
)

// codexEntries are ms, the models a provider the user added by its address
// lists at its Responses base, with the entry each has in Codex's own shape
// where the vendor gives one (catalog.Model.Codex). A relay built for Codex
// (sub2api) lists its models plainly at /v1/models, ids and owners alone,
// and in Codex's shape — window, levels, instructions, tools — when asked
// with the client_version Codex asks with; uni-switch read the plain list, so
// Codex was handed windows and levels models.dev guessed for the ids
// (gpt-6.1-terra at 922000) where the relay had said its own (ctopsc on
// Discord). So the list is asked once more as Codex asks it, at the URL it
// answered at, when that list is fetched: a request more where the
// provider is added, its key changed or its list refreshed, never one
// each time Codex asks uni-switch for its list. The answer only adds to
// the plain list: a model it has that the plain list hasn't is not added,
// as the plain list is the one uni-switch has always served (a relay's image
// models are in it alone). A list already in Codex's shape is not asked
// again. One that can't be read says nothing new: each model keeps the
// entry the last list gave it, as a base that can't be asked keeps its
// models (#904); one read that is a plain list says the vendor gives none.
func (p Provider) codexEntries(ctx context.Context, list string, ms []catalog.Model) []catalog.Model {
	if p.Preset != "" || p.Account != nil || slices.ContainsFunc(ms, func(m catalog.Model) bool { return len(m.Codex) > 0 }) {
		return ms
	}
	u, err := url.Parse(list)
	if err != nil {
		return ms
	}
	q := u.Query()
	q.Set("client_version", codexVersion())
	u.RawQuery = q.Encode()
	got, err := catalog.FetchURL(ctx, u.String(), p.Key, false, p.listHeadersAt(list))
	if err != nil {
		got, _, _ = catalog.Live(p.ID)
	}
	byID := map[string]catalog.Model{}
	for _, m := range got {
		if len(m.Codex) > 0 {
			byID[m.ID] = m
		}
	}
	if len(byID) == 0 {
		return ms
	}
	out := slices.Clone(ms)
	for i := range out {
		if c, ok := byID[out[i].ID]; ok {
			withCodex(&out[i], c)
		}
	}
	return out
}

// withCodex gives m the Codex entry c has, and what that entry says of the
// model where m's own list said nothing.
func withCodex(m *catalog.Model, c catalog.Model) {
	m.Codex = c.Codex
	if m.Context == 0 {
		m.Context = c.Context
	}
	if m.MaxContext == 0 {
		m.MaxContext = c.MaxContext
	}
	if len(m.Efforts) == 0 {
		m.Efforts = c.Efforts
	}
	if m.ImageInput == nil && c.ImageInput != nil {
		m.ImageInput, m.Images, m.InputModalities = c.ImageInput, c.Images, c.InputModalities
	}
}
