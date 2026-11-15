# Settings and provider onboarding

The default client workflow is select a client, add Dieqiyun with an API key,
then choose a model and use it. Native subscription sign-in and custom API
setup remain available as secondary actions. Provider actions appear only on
the client/provider page. Sessions, Library and Settings keep their own actions.

The client sidebar lists Codex, Claude Code, DSH, WorkBuddy, ZCode, Pi, OpenCode,
Gemini CLI, Kimi Code, Qoder and Qoder CN. Qoder's international and China builds
have separate entries and configurations. Each entry uses its existing native
client adapter and scopes Add provider subscriptions to that client's ID.
Clients without a native subscription use API providers.
Local model preset configuration remains hidden in this workflow.

Saved providers use compact bordered cards: identity and endpoint above,
explicit Edit and Use actions, then the current model (only when chosen for
this client) or available model count, context window and credential status.
Addresses and model counts are not repeated. Existing switches, ordering and
model pickers keep their original actions. Context has an Adjust button that
opens the provider's advanced context field with keyboard focus.
The provider hint, search field and account masking action share one vertically
centered toolbar row. Long hints truncate visually and retain their full text
in the tooltip; search and masking remain available at narrow window sizes.

Every model defaults to 256,000 tokens (256K), regardless of vendor metadata,
for provider lists, served model entries and native Codex model windows.
Explicit saved provider/model context overrides retain their existing behavior;
the default is not written into the override map. Clearing an override returns
that model to 256K. Catalog-listed limits remain available as metadata.

When no ordinary providers are saved, the client page shows a compact centered
Add Dieqiyun provider button and a short API-key hint. The center and top-right
actions use the same label, plus sign, dimensions, typography and button styling.
The top-right primary action also names Dieqiyun and opens its existing preset
editor directly, or the saved provider's editor when it is already added.
The empty state counts saved Off providers but excludes the dedicated image
setup. Failed configuration reads never appear as an empty first-use state.

Add custom provider appears once as small text at the bottom right and opens
the existing custom form. It has no provider card, search fallback or second
entry in the choices sheet. A secondary Subscriptions disclosure remains only
for clients with native subscription support. It opens the choices with
uni-switch recommended providers before that client's subscriptions. Search,
clear-then-close Escape behavior, repeated-click collapse, keyboard access and
returning focus to the disclosure are retained. Main actions and the custom
entry open dialogs and keep the existing modal focus and draft guards.

The window no longer scans other applications for local provider configurations,
shows discovery notices or offers an Import from other apps picker. Manual API
setup, recommended providers, native subscription sign-in and existing saved
providers remain available. The explicit compatibility import API and CLI parsers
are retained; the window never requests them.

General shows language, appearance, text size and open at login on the desktop.
Other applicable controls stay in Advanced configuration. The Tauri bundle uses
WebView zoom and its native autostart plugin; browser access has no desktop-only
controls. Allowance formatting and alerts are folded; account cards retain
their balances and allowance windows. Network follows the existing automatic
proxy policy; a saved manual choice is retained. Proxy, GitHub token, web page
permissions and browser access each have a closed disclosure. Image, search,
MCP and reply options are folded by feature. Searching settings temporarily
opens these disclosures and restores their state when the query is cleared.

Warm-up and daily check-in controls are under the corresponding signed-in
account's advanced settings. No unrelated accounts or empty global automation
tabs are shown. AccountWarmups stores reset mode and daily times by
agent|lowercase-user for native Codex and Claude accounts. A missing entry
inherits existing shared schedules, including CodexWarmAtOf; an explicit empty
entry switches that account's automation off. Both workers honor the overrides
and wake for their own reset or daily schedule even with the shared mode off.
AccountCheckins stores explicit switches by the check-in account key. Scheduled
workers combine overrides with the existing global settings; turning one account
on cannot enable another. A manual check-in still works with automation off.
Dedicated account-warmup and account-checkin settings routes validate ownership
and save one account; ordinary settings saves preserve these maps.

Adding a custom provider needs only its address and key; an omitted name comes
from the address. Request URL is followed by a visible Request endpoint selector.
Custom providers keep their existing per-protocol URLs and switch the primary
URL field with this selector; identity and alternate endpoints remain under
Advanced. Dieqiyun offers Automatic (recommended), Chat Completions, Responses
and Anthropic Messages. An explicit choice scopes model listing, connection
tests and saved routing to that endpoint. Automatic retains all supported URLs.
Existing single-endpoint providers reopen with that selection and the correct
server node; switching back to Automatic restores that node's supported URLs.
Tests never save draft credentials or endpoints. Test failures appear below
their corresponding URL and expand the row; request-level failures appear below
the Test button. Expanding technical details never overlaps another row.
Custom headers, extra JSON body, request/reply header passthrough and
provider/account/key proxy overrides have no configuration controls in either
API-provider or subscription editors. Their stored values remain in the draft
and are preserved by save, duplication, model listing and connection tests.
New providers follow the global network proxy settings; existing overrides
continue to work. Connection limits, including concurrency, accounts at once and
requests per minute, have no disclosure or configuration controls, for new,
saved, copied or subscription providers. The core still enforces stored limits;
new providers inherit the existing default limits. Price rate, Count as free,
time-of-day rates and queue tuning also have no form controls. Saving and duplicating keep their
stored values, including an explicit unlimited queue. A new non-copy provider
added through the GUI save API defaults to 20 queued requests per key/account
and a 120-second queue wait when those values are omitted; explicit API values
take precedence. Existing providers and copies receive no new defaults.
Model listing starts after a short pause when
address/key are available, for custom providers and the retained relay preset.
Recommendations use model metadata when available and offer a general chat
model otherwise. A successful automatic or explicit model-list fetch selects
all returned models and keeps manually entered IDs. Users can then deselect
individual models, pick none or adjust the selection. Selection edits made
while a request is pending take precedence over the default selection.
Failed or empty responses leave the current selection unchanged. Existing
saved providers keep their own model choices when refreshed.
Failures open the manual entry field. Generation, draft identity and request
fingerprints prevent a late response from changing another form or newer input.

Connection errors show a concise reason first with technical text under View
details. All error text is inserted as text, never interpreted as HTML. Existing
provider endpoints and protocol execution remain in the core.

Verification: scripts/qa-source-newcomer.mjs covers both browser engines and all
five languages. scripts/qa-source-client-flow.mjs checks that loading, refreshing,
switching clients and opening Add never request local discovery or imports, and
that no discovery notice or cross-application import control remains, alongside
manual setup, native subscriptions, keyboard operation and accessible controls.
scripts/qa-source-provider-limits.mjs checks removed controls,
queue defaults and preservation of connection limits and transport overrides
on save/copy in both engines and all languages.
gui/newcomer_limits_test.go covers the save API's new defaults, explicit values
and preservation. Account automation execution, independent schedules and settings
preservation are tested in provider/account_automation_test.go and
settingsapi/account_automation_test.go. Native verification uses an isolated QA
desktop and never changes the user's client configuration or autostart entry.

## Direct image setup on the client page

Every client page has a visible Image generation card and an Image setup
button. The complete card header is the disclosure button, including its title
and explanation. A chevron and Image setup/Collapse label show its state;
clicking again or pressing Enter/Space collapses it. It expands in place to
show the shared image model selector and a switch for giving the built-in image
tool to that client. Before setup it also shows an API Key field. After setup,
that field is replaced by a configured summary and Edit button. Editing opens
the key/refresh form in place, and Cancel closes it without saving. A successful
save returns to the summary. Neither opening nor switching clients exposes the
saved key or offers a second Add action. The
image service defaults to `https://cfimg.dieqiyun.top/v1`; there is no address
input. After a short pause in typing, the key is used to request `/models`.
Refresh image models also works without re-entering a saved key. It reuses
`renderImageGen` and the existing settings save queue; model choice remains
global and is labeled as shared by all clients. A first successful setup selects
the first returned image model; an explicit model selection or Off is preserved.
Timeout tuning remains under
Advanced configuration. Image generation, its timeout and its MCP address no
longer have a second settings-page entry in the local product. Image recognition
keeps its settings. Library installation still uses the same image configuration.
The client tool control has a visible switch track/thumb in addition to its
On/Off label. Disabled controls have a muted appearance and unavailable cursor;
their nearby explanation says what is needed. Disclosure headings and actions
have hover and keyboard focus states. Image setup, Add provider and provider
cards are verified in both browser engines across five languages, including
contrast and accessible names; light, dark and compact-window screenshots are
captured by `scripts/qa-source-client-flow.mjs`.

`assets/image-setup.js` reads the Library and image provider status when the card
is opened. `image_provider.go` serves GET/POST `/api/image-provider`. GET reports
configuration state and model references without returning credentials. POST
accepts only a key (or no key when refreshing) and validates the authenticated
list before saving the separate `dieqiyun-image` provider. Only image models are
cached for this flow; chat, video and retrieval entries are excluded. It never
calls image generation to validate a key. Existing ordinary providers and their
credentials are left intact, and an incompatible provider at the dedicated ID
is reported as a conflict. Invalid credentials, an empty/non-image list and
unreadable provider data do not overwrite a working setup. Setup requests are
serialized separately from readers, and canceled or outdated browser responses
cannot overwrite a newer form. Existing advanced provider values survive a
successful key replacement. The password field clears after saving and when
leaving the form; keys are never included in the status response.

`providersState` excludes only the dedicated ID with the image setup's exact
endpoint/protocol shape from the ordinary provider directory, including legacy
saved and Off rows. It therefore has no ordinary provider card, disabled card,
provider count or Add-preset marker. The saved row and live image catalog remain
available to the image picker and image tool. A different user-created provider
at the same host, or an incompatible custom row at that ID, stays visible.

Opening or changing clients writes nothing. Enabling installs the built-in
`uni-switch-image` server if needed or updates its agents through the existing
Library APIs. Disabling removes only this client's selection; it does not
remove the server, other clients, models or credentials. Each change reads
the current assignments first. Unsupported or unavailable clients, missing
image models and failed reads have explanatory disabled states. Failed
writes and per-client application problems stay visible in the card. A
late response for a previously selected client cannot replace the new one's
state. `scripts/qa-source-image-setup.mjs` exercises the visible entry, shared
model save, client-specific injection, preservation and failure states in
both browser engines and all five languages with synthetic API responses. It
also covers key-only automatic listing, saved-key refresh, validation errors
and stale responses, along with edit-only saved state, cancellation and the
absence of duplicate settings entries. `TestImageProvider*` covers authenticated discovery,
image-only model availability, persistence, failed validation, conflicting IDs,
unreadable data and cancellation with an isolated home and an HTTP test server.
It also verifies that a saved image setup stays editable and usable for images
while absent from ordinary providers and the chat model catalog.
