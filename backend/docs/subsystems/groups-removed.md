# Removed routing groups

Routing groups are removed from the source of this distribution. There is no
Groups page, add/edit/import/template workflow, groups CLI or TUI page, groups
plugin, group catalog builder, classifier rules, nested group routing or media
group execution. All /api/groups routes return 404. Legacy Groups bookmarks
fall back to Providers. No automatic group is created for repeated model names.

The gateway still resolves ordinary provider/model references and bare model
names in provider order. Provider fallbacks, key/account rotation, session
affinity, concurrency limits, reasoning levels and Fast mode remain available.
Local request diagnostics are shown in Settings > Gateway's collapsible Request records section. The independent Routing page, animation and replay client are removed.

Old providers.json group records and related ordering/skip flags are preserved
as opaque JSON while editing providers. They are never interpreted, expanded,
rewritten or served. The retired group/ namespace is reserved and rejected as
a model target. Historical request traces can retain group labels and rule-hit
shapes for reading old local diagnostics; these do not execute rules.

Shared model targets, time windows and pattern parsing now live in
internal/provider/routing_models.go, time_window.go, settings_parsing.go and
model_patterns.go. Ordinary account selection remains in the gateway.

Verification uses isolated homes and synthetic providers:
- TestRetiredGroupsRemainInert checks serving, fallback and legacy preservation.
- TestDirectProviderKeyFailoverWithoutGroups checks a direct request across keys.
- TestRetiredGroupCannotReachProvider checks rejected retired targets.
- TestRetiredGroupAPIsUnavailable checks all former management paths.
- scripts/qa-source-sidebar.mjs checks navigation, update states and keyboard
  disclosure behavior in Chromium and WebKit across all five UI languages.

The obsolete Intent classifier timeout is removed from the Settings UI, saved settings schema and public settings-key catalog. Legacy input cannot configure the internal wait.
