package gateway

import (
	"testing"
)

// upstreamFailed400 is a relay's own word that the provider behind it
// turned the request away: sub2api's answer to a translated Claude Code
// turn whose system message its Codex backend refused (#4632). It is not
// the client's mistake, and not the relay seat's: the group's next member
// is asked, and the seat rests for nothing.
const upstreamFailed400 = `{"error":{"message":"sub2api: Upstream request failed","type":"upstream_error"}}`

// The words fail a 400 or a 422 over, and nothing beside them: a status of
// the account's own (401, 403, 429), a plain bad request, and a relay that
// also says the quota is spent keep the reading they had.
func TestUpstreamFailedWordsFailARelayOver(t *testing.T) {
	for _, x := range []struct {
		status int
		body   string
		want   bool
	}{
		{400, upstreamFailed400, true},
		// as uni-switch tells it on, with the relay's name before, and the
		// relay's own type around it; and apex's, whose type said
		// invalid_request_error (#1511)
		{400, `{"error":{"message":"apex: Upstream request failed","type":"invalid_request_error"}}`, true},
		{422, `{"error":{"message":"Upstream request failed"}}`, true},
		// a status of the account's own keeps its reading
		{401, upstreamFailed400, false},
		{403, upstreamFailed400, false},
		{429, upstreamFailed400, false},
		// a plain bad request ends the route, as it would on every member
		{400, `{"error":{"message":"Invalid parameter: model"}}`, false},
		{400, `{"error":{"message":"Upstream request failed, insufficient_quota"}}`, false},
	} {
		if got := shapeRefused(x.status, []byte(x.body)); got != x.want {
			t.Errorf("shapeRefused(%d, %s) = %v, want %v", x.status, x.body, got, x.want)
		}
	}
}
