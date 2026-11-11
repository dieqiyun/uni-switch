# Removed configurable request masking

The product no longer modifies request or response content for privacy masking.
The local identification model, PII recognizers, user word and pattern rules,
mask/warn/block actions, allow list, reversible placeholders, alternative model
route, per-provider bypass, records, timing, notifications and their GUI/CLI/API
controls are removed from source. No model helper is built or downloaded.

Legacy `redact*` and `maskAlert` fields are ignored when old JSON is read and are
not emitted by the settings API or subsequent saves. Old provider `unredacted`
values are ignored. Legacy files are not loaded by the removed subsystem, and
this change does not delete files in a user's existing data directory.

Inference keeps the original user text through chat/counting, classifier/search,
embeddings/rerank, speech/transcription, image/video and native forwarding paths.
Normal protocol conversion, authentication, model rewriting, access controls,
streaming, tools, reasoning and previous-response context remain in place.
Anthropic `redacted_thinking`, signatures and opaque/encrypted reasoning are
protocol data and remain supported. An image editing `mask` is unrelated.

`internal/logsafe` removes credentials irreversibly from diagnostic copies only:
request archives, captured bodies/headers and displayed prompt/reply copies.
It has fixed credential patterns, no personal information
detector, no user rules, no model, no placeholder registry and no restoration.
It never supplies a body sent to a provider or client. Logs must not expose API
keys or login tokens. Anonymous reporting controls, account discovery and login
page choices, session read permissions and the explicitly selected screenshot
account blur remain separate Privacy controls.

Historical usage formats retain an unused numeric slot so a former masking
duration cannot shift subsequent counters. No current API or view exposes it.

Regression entry points:

- `TestRetiredMaskingSettingsDoNotChangeOutboundCount` exercises legacy block,
  model-route and word settings against an actual fake provider.
- `TestRetiredMaskingEndpointsAndAssetsAreAbsent` and
  `TestRetiredMaskingSettingsAreNotKeptOrExposed` exercise the real GUI handler.
- `TestRetiredTimingSlotDoesNotShiftStoredUsage` reads an old nonzero duration.
- `internal/logsafe` tests credential removal and preservation of personal text
  and exact JSON numbers.
- `node scripts/qa-source-no-masking.mjs` runs the compiled core with isolated
  settings in Chromium and WebKit, five locales, and checks the remaining
  Privacy controls without touching real client configuration.
