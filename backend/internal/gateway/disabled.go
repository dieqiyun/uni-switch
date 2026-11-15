package gateway

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// Disabled models (#1867, provider/disabled.go): a model the user turned
// off is refused on every route before anything is sent upstream, with a
// 403 in the API's own shape. A routing group goes on without it; one left
// with none is refused the same way. The catalog leaves a disabled model
// out, so the lists and the groups never offer it; the checks here are for
// the requests that name it anyway — an agent's old config, a session that
// was on it, a fallback, a sub-agent's pick, count_tokens, a decision
// model — and candidates() never plans one (perKeyBarred).

// disabledError is what a request for a disabled model is told.
func disabledError(id, pat string) string {
	return fmt.Sprintf("model %s is disabled in uni-switch (it matches %q in Settings › Models › Disabled models); enable it there or with `uni-switch model enable %s`. Only requests through uni-switch are refused", id, pat, pat)
}

// unknownDisabledError is what every request is told while settings.json
// can't be read and no list of disabled models was read before it.
func unknownDisabledError(id string) string {
	return fmt.Sprintf("model %s is disabled in uni-switch for now: settings.json can't be read, so uni-switch doesn't know which models are disabled; fix or restore settings.json", id)
}

// modelOff is the refusal for p's model, resolved, as id names it to the
// caller; ok is false when it isn't disabled.
func modelOff(p provider.Provider, model, id string) (string, bool) {
	if provider.DisabledUnknown() {
		return unknownDisabledError(id), true
	}
	if pat, off := provider.ModelDisabled(p.ID, model); off {
		return disabledError(id, pat), true
	}
	return "", false
}

// idOff is the refusal for a model id as asked, resolved or not: a group's
// when every model it would have is disabled, else the model's.
func idOff(id string) (string, bool) {
	id = strings.TrimSpace(id)
	if id == "" {
		return "", false
	}
	if provider.DisabledUnknown() {
		return unknownDisabledError(id), true
	}
	if len(provider.DisabledModels()) == 0 {
		return "", false
	}

	if pat, off := provider.IDDisabled(id); off {
		return disabledError(id, pat), true
	}
	if p, model, ok := provider.Resolve(id); ok {
		return modelOff(p, model, id)
	}
	// a bare id that resolves to nothing because the catalog left it out:
	// one of the providers' models the user disabled ("embed-1" with
	// lib/embed-1 disabled), refused as itself, not as a model uni-switch
	// doesn't know
	if pat, off := provider.BareDisabled(id); off {
		return disabledError(id, pat), true
	}
	return "", false
}

// refuseOff answers a request for a disabled model with its 403 in proto's
// shape; false when the model isn't disabled.
func refuseOff(w http.ResponseWriter, proto provider.Protocol, id string) bool {
	msg, off := idOff(id)
	if off {
		writeError(w, proto, http.StatusForbidden, msg)
	}
	return off
}

// countOff is refuseOff for count_tokens, id resolved as the count is: a
// bare group's name, an auto group's stand-in.
func countOff(w http.ResponseWriter, proto provider.Protocol, id string) bool {

	return refuseOff(w, proto, id)
}

// codexNativeOff is the refusal for one of Codex's own models, asked at
// the ChatGPT backend's route by its bare id: disabled by that id, or as
// the codex provider's ("codex/gpt-5.5").
func codexNativeOff(model string) (string, bool) {
	if model == "" {
		return "", false
	}
	if provider.DisabledUnknown() {
		return unknownDisabledError(model), true
	}
	if pat, off := provider.IDDisabled(model); off {
		return disabledError(model, pat), true
	}
	if pat, off := provider.ModelDisabled("codex", model); off {
		return disabledError(model, pat), true
	}
	return "", false
}
