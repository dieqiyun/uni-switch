# Removed observability export

The Settings observability page and the OTLP/Langfuse collector integration are
removed from source. There is no exporter module, built-in registration,
collector UI, metrics worker, gateway request observer, usage ledger observer or
agent conversation trace reader for export. Exporting request and response
bodies, tool arguments, tool output and file contents is no longer supported.

The `otel` settings object is ignored when reading legacy JSON. It is absent
from the settings API and from subsequent ordinary saves. Legacy
`MAGPIE_OTEL_*`, `OTEL_RESOURCE_ATTRIBUTES` and `OTEL_SERVICE_NAME` values are not
read and cannot enable export. CLI settings operations reject the retired key.
Opening a saved `settings&tab=otel` link falls back to an available settings
page. A saved retired tab preference cannot restore the removed page.

The removal does not delete or rewrite a user's existing data files at startup.
Local request and token accounting, routing diagnostics, request archives,
session browsing, provider connections and the remaining Privacy preferences
are separate features. Credential cleanup of diagnostic copies stays in
`internal/logsafe`; it does not change forwarded request or response bodies.

Verification:

- `node --test scripts/tests/no-observability.test.mjs` checks that the retired
  sources, runtime imports, environment readers and translated UI are absent.
- `TestRetiredObservability*` exercises the real GUI handler with malformed
  legacy settings and valid legacy environment values requesting all exports.
- `TestSettingsRefusesAndWritesNothing` checks that the CLI cannot get, set or
  reset the removed settings object.
- `node scripts/qa-source-no-observability.mjs` builds the current core and
  checks settings, old links, search and ordinary saves using isolated data,
  Chromium and WebKit, and all five supported languages.
