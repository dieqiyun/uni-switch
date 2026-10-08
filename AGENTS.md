# Repository instructions

## Release policy

The maintainer explicitly restored open-source, multi-platform publication
starting with v0.5.20. Publish the complete reviewed source to GitHub under
AGPL-3.0-only and provide Windows, Linux and macOS build artifacts plus an exact
corresponding source archive, usage guide and checksums. Do not remove or replace
historical releases. Release tags must identify the source used for the build.

Use `.github/workflows/build-desktop.yml` for native multi-platform builds.
Only publish after all platform jobs and artifact checks succeed. Source audits
must exclude private client data, API keys, caches and generated build output.
The older Windows-only scripts default to source-inclusive releases; never
silently substitute a historical public commit for a newer binary's source tag.

## Windows text handling

PowerShell 5 text operations must explicitly use UTF-8. Read files with
`Get-Content -Raw -Encoding UTF8`, search with `rg` or `Select-String -Encoding UTF8`,
and write with `WriteAllText` and `UTF8Encoding(false)`. Set console input/output
and `$OutputEncoding` to UTF-8 before piping Chinese text to other programs.

## Client verification

Use isolated `.qa` data/config directories and synthetic provider credentials.
Do not change, restart or stop the user's real Codex, Claude or uni-switch
processes/configurations for testing. Only stop processes created by the current
QA run. Native UI scripts share CDP port 9223 and must run sequentially; Cargo
operations share the target directory and must also run sequentially.
