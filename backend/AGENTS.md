# Notes for coding agents

## Subsystem design and review

Before changing a documented subsystem, read its
[design reference](docs/subsystems/README.md) and verify the linked source.
When preparing or reviewing a PR, follow the same page's documentation update,
semantic change description, and review rules.

## Code standards

Write and review changes against the [code standards](docs/code-standards.md).

## Community PRs

Before reviewing a community PR, read [contributors](docs/contributors.md):
what stops a review for possible harm, what is closed unreviewed, and the
authors whose PRs are closed on sight.

## Lessons

Before changing code, read [LESSONS.md](LESSONS.md): what recent merged
work got wrong and the rule that would have caught it.

## Removed third-party extensions

Product identity is `uni-switch`, including generated client configuration,
wire namespaces, environment variables and native resources. Do not restore
upstream branding or substitute nonexistent hosted services or packages.
Preserve legal attribution and captured third-party inputs. See
[product identity](docs/subsystems/product-brand.md).

Built-in vendor API presets have also been removed from the provider directory.
Do not restore their definitions or implicit endpoint imports. Keep native
subscriptions, custom endpoints, protocol support and model pricing metadata.
See [the removal boundary](docs/subsystems/vendor-presets-removed.md).

This product no longer ships third-party extension installation, markets,
authentication hosts, JavaScript execution, pages, or provider migrations.
Do not restore them as a workaround for a retired subscription. Native client
configuration adapters and native subscription providers are separate features.

The statically compiled modules under plugins/ are core features, not external
extensions. Keep sessions, library and privacy pages. Gateway controls and request
records belong in Settings > Gateway; do not restore an independent page.
Their runtime uses compiled defaults and ignores legacy plugins.yml choices.
Legacy extension files are left intact and must not be loaded or executed.

Read [the current runtime boundary](docs/subsystems/plugin-runtime.md) and
[subscription ownership](docs/subsystems/provider-plugins.md) before editing.

## Removed sync and portable backups

Cross-computer synchronization and portable/manual/automatic backups are removed.
Do not reintroduce their workers, commands, APIs or management UI. Leave legacy
files untouched. Client config atomic writes, rollback and disconnect recovery,
and local library application remain necessary. See
[the removal boundary](docs/subsystems/sync-backup-removed.md).

## Local-only networking

LAN sharing, IP allow lists and remote management are removed. Do not restore
wildcard listeners or public-address discovery. Keep the dedicated local WSL
adapter/key contract and outbound provider connections. See
[local-only networking](docs/subsystems/local-only.md).

## Removed request masking

Configurable privacy masking, local PII models, user rules, reversible
placeholders, alternative privacy routes, records and notifications are removed.
Do not restore them or the per-provider bypass. Keep credential cleanup for
diagnostic copies (`internal/logsafe`), protocol `redacted_thinking`, image edit
masks and the unrelated Privacy controls. See
[the removal boundary](docs/subsystems/masking-removed.md).

## Removed observability export

External observability export is removed from source: no OTLP/Langfuse module,
collector setup, metrics exporter, request observer or agent conversation export.
Legacy settings and environment variables cannot reactivate it. Keep local
usage accounting, routing diagnostics, request archives and the Sessions page.
See [the removal boundary](docs/subsystems/observability-removed.md).

## Removed usage reports

The Usage page and reporting APIs are removed. Sessions must not render Token
usage, cache counters or cost. Keep protocol usage, account allowances, gateway
limits and routing diagnostics used by the retained features. See
[the removal boundary](docs/subsystems/usage-removed.md).

## Removed routing groups

Routing groups, their management API/CLI/TUI, templates, automatic grouping and
classifier rules are removed. Keep ordinary provider routing and Settings > Gateway's collapsible
request records. Preserve old records as inert JSON without reactivating them.
See [the removal boundary](docs/subsystems/groups-removed.md).
