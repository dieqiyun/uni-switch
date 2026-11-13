package provider

import (
	"context"
	"slices"
	"time"

	"github.com/dieqiyun/uni-switch/backend/internal/probelog"
)

// A Google sign-in (Antigravity, Gemini CLI) speaks Code Assist alone, an
// API the gateway builds every request for in the account's envelope, so
// a test had no request of its own to send: Test models was off and said
// to ask the model from an agent (lc on Discord: Antigravity's test button
// was unusable while every other provider's worked). Its test is asked as
// an agent's request is, by the gateway's own translator (TranslatedProbe),
// the smallest "hi" in that envelope, read to the model's first word.

// translatedProbe asks model on proto through the gateway's translator,
// and says how it failed: status, when the vendor answered one, and its
// message. The gateway sets it (ProbeTranslatedVia).
var translatedProbe func(ctx context.Context, p Provider, proto Protocol, model string) (status int, err error)

// ProbeTranslatedVia sets how a model only the gateway can ask is tested.
func ProbeTranslatedVia(f func(ctx context.Context, p Provider, proto Protocol, model string) (int, error)) {
	translatedProbe = f
}

// testsTranslated is whether p's models are tested through the gateway's
// translator: a Google sign-in, on Code Assist, when the gateway is there
// to ask it.
func (p Provider) testsTranslated() bool {
	return translatedProbe != nil && p.Account != nil && !p.IsPlugin() && p.Account.codeAssist != ""
}

// pluginTranslated is whether model is a plugin's that only Code Assist
// serves (@ai-sdk/google): the gateway asks it in Gemini's generateContent
// through the plugin's fetch, so a test does too. Sent as any of the
// plugin's other APIs, its vendor read a body of another shape (Zed's
// cloud: 400 "failed to parse Google AI request: missing field contents",
// Been on Discord).
func (p Provider) pluginTranslated(model string) bool {
	return translatedProbe != nil && p.IsPlugin() && slices.Equal(p.APIs(model), []Protocol{CodeAssist})
}

// testTranslated is the test of model on a provider testsTranslated, or
// of a plugin's model pluginTranslated.
func (p Provider) testTranslated(ctx context.Context, model string) Result {
	r := Result{Protocol: CodeAssist, Model: model, Account: p.Account.User}
	if model == "" {
		r.Error = "no model to try: expose one, or refresh the model list"
		return r
	}
	wait := testWait
	if p.IsPlugin() {
		// its fetch may renew the sign-in first (pluginTestWait)
		wait = max(wait, pluginTestWait)
	}
	ctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	start := time.Now()
	status, err := translatedProbe(probelog.Taken(ctx), p, CodeAssist, model)
	r.Millis = time.Since(start).Milliseconds()
	// the gateway's translator sent it: written down once, from what it
	// came to
	noted, msg := status, ""
	if err != nil {
		msg = err.Error()
	} else {
		noted = 200
	}
	probelog.NoteResult(probelog.WithModel(ctx, model), string(CodeAssist), start, noted, msg)
	if err != nil {
		r.Status, r.Error = status, err.Error()
		return r
	}
	r.OK, r.Status = true, 200
	return r
}
