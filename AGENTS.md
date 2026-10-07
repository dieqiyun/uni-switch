# Repository instructions

## Release policy

The maintainer explicitly requested binary-only releases starting with v0.5.19.
Keep source changes and commits local. Do not push source commits or upload a
source archive to GitHub unless the human maintainer explicitly changes this
policy. Do not remove already published historical source or releases.

The prepare/publish scripts default to compiled Windows programs, usage guide
and checksums. Do not pass `-IncludeSource` for the normal release workflow.
A binary-only GitHub release tag reuses an existing public commit; the automatic
GitHub Source code downloads are historical snapshots, not this binary's source.
Preserve that distinction in release notes. Verify the remote main revision is
unchanged after publication.

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
