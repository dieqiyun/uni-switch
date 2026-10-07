param([ValidateSet('dev', 'test', 'build', 'check', 'clippy', 'qa', 'restart-fixture')][string]$Action = 'build')
$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$env:Path = (Join-Path $env:USERPROFILE '.cargo\bin') + ';' + $env:Path
$vsWherePath = Join-Path ${env:ProgramFiles(x86)} 'Microsoft Visual Studio\Installer\vswhere.exe'
if (Test-Path -LiteralPath $vsWherePath) {
    $vsPath = & $vsWherePath -latest -products '*' -requires Microsoft.VisualStudio.Component.VC.Tools.x86.x64 -property installationPath
    if ($vsPath) {
        & (Join-Path $vsPath 'Common7\Tools\Launch-VsDevShell.ps1') -Arch amd64 -HostArch amd64 -SkipAutomaticLocation
    }
}
Push-Location (Join-Path $PSScriptRoot '..')
try {
    switch ($Action) {
        'dev' { corepack pnpm dev }
        'test' { cargo test --manifest-path src-tauri/Cargo.toml --no-default-features }
        'check' { cargo check --manifest-path src-tauri/Cargo.toml }
        'clippy' { cargo clippy --manifest-path src-tauri/Cargo.toml --all-targets -- -D warnings }
        'build' { corepack pnpm build:desktop }
        'qa' { cargo build --manifest-path src-tauri/Cargo.toml --features qa-webview,tauri/custom-protocol }
        'restart-fixture' { cargo build --manifest-path src-tauri/Cargo.toml --no-default-features --example codex_restart_fixture --example claude_cli_restart_fixture }
    }
    exit $LASTEXITCODE
} finally { Pop-Location }
