package gateway

import (
	"fmt"
	"strings"

	"github.com/dieqiyun/uni-switch/backend/internal/provider"
)

// unknownModel is what a request for a model uni-switch doesn't know is told
// (MOMO on Discord: "Zen-Space-Bunny" got a 404 that said neither how a
// model is named nor which one was meant): the ids nearest to it, the
// ways a model can be named, then every id there is. what is "" for a
// chat, or what the model was for (" to draw with").
func unknownModel(asked, what string) string {
	msg := fmt.Sprintf("uni-switch knows no model %q%s", asked, what)
	if near := provider.Closest(asked, 3); len(near) > 0 {
		msg += "; did you mean " + strings.Join(near, ", ") + "?"
	}
	msg += " Name a model as provider/model (as /v1/models lists it), a routing group's id or name, or a bare model id" +
		" (one served by several providers goes to the group uni-switch found for it, else to the first of them in the Providers order)"
	if ids := provider.IDs(); len(ids) > 0 {
		msg += "; it has " + strings.Join(ids, ", ")
	} else {
		msg += "; add a provider in uni-switch first"
	}
	return msg
}
