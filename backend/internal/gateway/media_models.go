package gateway

import (
	"slices"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/catalog"
	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// bareRef is whether ref names no provider: "embed-1", or a vendor's id
// with a slash of its own ("BAAI/bge-m3") where no provider has the part
// before it as its id.
func bareRef(ref string) bool {
	pid, _, ok := strings.Cut(strings.TrimSpace(ref), "/")
	if !ok {
		return true
	}
	_, err := provider.Find(pid)
	return err != nil
}

// servesKind says whether a provider serves a model of kind's under that
// id: its fetched list says so (catalog.LiveServing; Videomakers for
// videos), its chat list has an id that says so (another uni-switch's list,
// which keeps them as it lists them), or, for embeddings and rerank, its
// preset names it (PlanRetrieval).
func servesKind(kind string) func(p provider.Provider, model string) bool {
	is := func(m catalog.Model, model string) bool { return m.ID == model }
	if kind == kindDecision {
		return servesDecision
	}
	if kind == provider.KindVideo {
		return func(p provider.Provider, model string) bool {
			return slices.ContainsFunc(Videomakers(p), func(m catalog.Model) bool { return is(m, model) })
		}
	}
	return func(p provider.Provider, model string) bool {
		if slices.ContainsFunc(catalog.LiveServing(p.ID, kind), func(m catalog.Model) bool { return is(m, model) }) {
			return true
		}
		if catalog.ServesOf(model) == kind && slices.ContainsFunc(provider.KeptListing(p).Available, func(m catalog.Model) bool { return is(m, model) }) {
			return true
		}
		return slices.ContainsFunc(p.PlanRetrieval(kind), func(m catalog.Model) bool { return is(m, model) })
	}
}

// retrievalKind is the kind of model an operation of serveRetrieval's is
// asked of.
func retrievalKind(operation string) string {
	switch operation {
	case "rerank":
		return catalog.ServesRerank
	case "speech":
		return catalog.ServesSpeech
	case "transcriptions", "translations":
		return catalog.ServesTranscription
	case "decisions":
		return kindDecision
	}
	return catalog.ServesEmbedding
}

// resolveMediaModel finds the first active provider serving this bare model.
func resolveMediaModel(ref, kind string) (provider.Provider, string, bool) {
	if !bareRef(ref) {
		return provider.Provider{}, "", false
	}
	serves := servesKind(kind)
	for _, p := range provider.All() {
		if !p.On() {
			continue
		}
		if _, disabled := provider.ModelDisabled(p.ID, ref); disabled {
			continue
		}
		if serves(p, ref) {
			return p, ref, true
		}
	}
	return provider.Provider{}, "", false
}

func offChatList(p provider.Provider, model string) bool {
	return !slices.ContainsFunc(provider.KeptListing(p).Available, func(m catalog.Model) bool { return m.ID == model })
}
