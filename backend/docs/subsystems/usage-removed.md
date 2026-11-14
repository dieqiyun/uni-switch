# Removed usage reports

The product has no Usage navigation, overview, request ledger reports, session
statistics, heatmap, context-analysis page, tray totals or tray reporting tab. Their compiled
module, scripts and first-party reporting endpoints are deleted. Legacy saved
page selections fall back to the provider page. Existing history files are left
in place.

The independent Sessions page keeps titles, models, message counts, file
metadata, search, transcripts, resume, migration and trash controls. It has no
Token, input/output/cache counter or money rendering code in rows or details.

Protocol Token usage, gateway limits and budget enforcement, routing diagnostics,
archives, session parsers and account allowances serve retained features. Those
are separate from the deleted reports. Shared UI helpers are in
`internal/gui/assets/request-details.js`. Account allowance/reset/check-in
operations are in `internal/gui/account_allowances.go`, under `/api/accounts/`.
Outbound vendor balance APIs keep their vendor-defined paths.

Deleted endpoints include `/api/usage` and its report routes,
`/api/sessions/stats`, `/api/sessions/overview`, aggregate `/api/context`,
`/api/tune` and `/v1/uni-switch/usage` (including request ingestion). Routing
context text/reply and request archive endpoints remain.

Verification: `scripts/qa-source-no-usage.mjs`,
`scripts/tests/no-usage.test.mjs` and isolated GUI/gateway tests.
