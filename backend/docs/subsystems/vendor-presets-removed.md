# Removed built-in vendor API presets

The Add sheet no longer has a Vendors section. All vendor API presets,
including regional variants, coding/token plans and hosted decision services,
are deleted from `internal/provider/presets.go`. Only the Dieqiyun relay,
local servers remain. The remote gateway preset and its implementation
are also removed; see [local-only networking](local-only.md). Native
subscriptions and custom endpoint configuration are separate features.

`Presets`, `Preset` and `FromPreset` share the same reduced directory. The
CLI and import links cannot create a removed vendor preset, including old
Qianfan and Tencent aliases. Key-only imports from other apps cannot invent
an endpoint for these providers. Imports that explicitly supply an endpoint
remain custom providers. Regional pairing in the Add sheet and the associated
editor switch have been removed, together with default vendor model lists,
signup URLs, region definitions and endpoint migration tables.

Removing built-in definitions does not delete existing `providers.json`
entries or rewrite their endpoints. Shared API protocols, native subscription
adapters, manually configured endpoints and local model servers still use
the provider and gateway code. Model-maker catalog IDs remain metadata for
pricing and reasoning levels; API host exclusions prevent relay balance
probing of manually configured vendor endpoints. Neither is an addable preset.

Verification: `TestUniSwitchVendorPresetsRemoved` rejects the former IDs and
their import links; the directory, custom-import and saved-configuration tests
in `internal/provider/uni_switch_directory_test.go` cover the retained paths.
`scripts/tests/no-vendor-presets.test.mjs` checks physical source deletion;
`scripts/qa-source-directory.mjs` exercises the compiled API and Add sheet in
Chromium and WebKit across the five UI languages, using isolated QA data.
