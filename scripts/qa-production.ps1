$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$smokeRoot = Join-Path (Get-Location) ('.qa/reasoning-repair/release-smoke/' + [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds())
$exePath = (Resolve-Path -LiteralPath 'release/windows-x64/uni-switch_0.4.0_x64.exe').Path
$env:UNI_SWITCH_DATA_DIR = Join-Path $smokeRoot 'data'
$env:WEBVIEW2_USER_DATA_FOLDER = Join-Path $smokeRoot 'webview'
$env:CODEX_HOME = Join-Path $smokeRoot 'codex'
$env:CLAUDE_CONFIG_DIR = Join-Path $smokeRoot 'claude-cli'
# Keep even read-only desktop discovery in an isolated directory.
$env:LOCALAPPDATA = Join-Path $smokeRoot 'local-app-data'
New-Item -ItemType Directory -Force -Path $smokeRoot,$env:CODEX_HOME,$env:CLAUDE_CONFIG_DIR,$env:LOCALAPPDATA | Out-Null
$smokeProcess = $null
try {
    $smokeProcess = Start-Process -FilePath $exePath -WindowStyle Hidden -PassThru
    $end = [DateTime]::UtcNow.AddSeconds(20)
    do {
        Start-Sleep -Milliseconds 300
        $smokeProcess.Refresh()
        if ($smokeProcess.HasExited) { throw 'Production app exited before initialization' }
    } until ((Test-Path -LiteralPath (Join-Path $env:UNI_SWITCH_DATA_DIR 'uni-switch.db')) -and $smokeProcess.MainWindowHandle -ne 0 -or [DateTime]::UtcNow -gt $end)
    $client = [System.Net.Sockets.TcpClient]::new()
    $debugPortOpen = $false
    try {
        $connect = $client.ConnectAsync('127.0.0.1',9223)
        $debugPortOpen = $connect.Wait(500) -and $client.Connected
    } catch { $debugPortOpen = $false } finally { $client.Dispose() }
    $smokeProcess.Refresh()
    $result = [ordered]@{
        version = '0.4.0'
        databaseInitialized = (Test-Path -LiteralPath (Join-Path $env:UNI_SWITCH_DATA_DIR 'uni-switch.db'))
        windowTitle = $smokeProcess.MainWindowTitle
        responding = $smokeProcess.Responding
        qaDebugPortOpen = $debugPortOpen
        path = $exePath
        runDirectory = $smokeRoot
    }
    $json = $result | ConvertTo-Json -Depth 4
    [System.IO.File]::WriteAllText((Join-Path $smokeRoot 'results.json'),$json,[System.Text.UTF8Encoding]::new($false))
    [System.IO.File]::WriteAllText((Join-Path (Get-Location) '.qa/reasoning-repair/release-smoke/results.json'),$json,[System.Text.UTF8Encoding]::new($false))
    $json
    if (-not $result.databaseInitialized -or -not $result.responding -or $result.windowTitle -ne 'uni-switch' -or $result.qaDebugPortOpen) {
        throw 'Production smoke verification failed'
    }
} finally {
    if ($smokeProcess -and -not $smokeProcess.HasExited) { Stop-Process -Id $smokeProcess.Id }
}
