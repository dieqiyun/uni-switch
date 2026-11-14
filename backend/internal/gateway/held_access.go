package gateway

import (
	"github.com/dieqiyun/uni-switch/backend/internal/access"
	"net/http"
)

// heldKey: the request's gateway key is held to a budget, models or
// accounts, and so sees only its own usage.
func heldKey(who access.Identity) bool {
	return who.KeyID != "" && (who.Limited() || len(who.Models) > 0 || len(who.Accounts) > 0)
}

// heldRefused is what a held key is told where this uni-switch's own accounts
// would be: the quotas, their cards and history, a card's refresh and the
// accounts' lanes (321sssrt-bit, #1981: a friend given a key with a
// budget saw every account's card, email and balance on their Remote
// uni-switch's Usage page). The key's own limit stays at /v1/uni-switch/limit.
const heldRefused = "this gateway key is held to a budget, models or accounts, so it is told nothing of this uni-switch's accounts: GET /v1/uni-switch/limit tells it its own limit"

// heldQuotasOff is what a held key whose owner hasn't turned its quotas on
// is told by the quotas and the status line (scopeQuotas): it names the
// switch that tells it those of what it may use (#2216), as steven_4422
// read the bare refusal as "a held key gets no status line".
const heldQuotasOff = "this gateway key is held to a budget, models or accounts, so it is told nothing of this uni-switch's accounts until its owner turns on Quotas for it (Gateway → Gateway keys, or uni-switch gateway-key quotas <id> on); it is then told the accounts it may use of each provider whose every model it may use. GET /v1/uni-switch/limit tells it its own limit and what holds it"

// refuseHeld answers a held key 403 with msg, marked with
// "gateway_key_held" so another uni-switch shows the key's own card alone
// rather than "isn't shared", and says whether it did.
func refuseHeld(w http.ResponseWriter, r *http.Request, msg string) bool {
	if !heldKey(access.Caller(r.Context())) {
		return false
	}
	writeJSON(w, http.StatusForbidden, map[string]any{"error": map[string]any{"message": msg, "type": "permission_error", "code": "gateway_key_held"}})
	return true
}
