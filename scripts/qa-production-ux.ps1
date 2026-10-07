param([ValidatePattern('^\d+\.\d+\.\d+$')][string]$Version = '0.5.16')
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$taskExisting = @(Get-Process -Name 'uni-switch*' -ErrorAction SilentlyContinue)
if ($taskExisting.Count) {
    $taskSkipped = [ordered]@{ version = $Version; status = 'not_run_existing_instance'; reason = 'Existing uni-switch instance is preserved; production single-instance handoff prevents isolated startup'; existingProcessIds = @($taskExisting | ForEach-Object { $_.Id }) } | ConvertTo-Json
    [IO.File]::WriteAllText((Join-Path (Get-Location) '.qa/ux-flow/production-results.json'),$taskSkipped,[Text.UTF8Encoding]::new($false))
    throw 'Existing uni-switch instance detected, including a versioned standalone executable; isolated startup check was not run'
}
# Process.MainWindowHandle can select the single-instance plugin's helper window.
# Inspect the actual Tauri UI window so hidden startup cannot pass or fail on it.
Add-Type -TypeDefinition @'
using System;
using System.Text;
using System.Runtime.InteropServices;
public static class UniSwitchProductionWindow {
    public delegate bool Callback(IntPtr window, IntPtr value);
    [DllImport("user32.dll")] static extern bool EnumWindows(Callback callback, IntPtr value);
    [DllImport("user32.dll")] static extern uint GetWindowThreadProcessId(IntPtr window, out uint pid);
    [DllImport("user32.dll")] public static extern bool IsWindowVisible(IntPtr window);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetWindowText(IntPtr window, StringBuilder text, int length);
    [DllImport("user32.dll", CharSet=CharSet.Unicode)] static extern int GetClassName(IntPtr window, StringBuilder text, int length);
    public static IntPtr Find(int processId) {
        IntPtr result = IntPtr.Zero;
        EnumWindows((window, value) => {
            uint owner;
            GetWindowThreadProcessId(window, out owner);
            if (owner != processId) return true;
            var title = new StringBuilder(256);
            var windowClass = new StringBuilder(256);
            GetWindowText(window, title, 256);
            GetClassName(window, windowClass, 256);
            if (title.ToString() == "uni-switch" && windowClass.ToString() == "Tauri Window") {
                result = window;
                return false;
            }
            return true;
        }, IntPtr.Zero);
        return result;
    }
    public static string Title(IntPtr window) {
        var title = new StringBuilder(256);
        GetWindowText(window, title, 256);
        return title.ToString();
    }
}
'@
$taskRoot = Join-Path (Get-Location) ('.qa/ux-flow/production/' + [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds())
$taskExe = (Resolve-Path -LiteralPath ('release/windows-x64/uni-switch_' + $Version + '_x64.exe')).Path
$env:UNI_SWITCH_DATA_DIR = Join-Path $taskRoot 'data'
$env:WEBVIEW2_USER_DATA_FOLDER = Join-Path $taskRoot 'webview'
$env:CODEX_HOME = Join-Path $taskRoot 'codex'
$env:CLAUDE_CONFIG_DIR = Join-Path $taskRoot 'claude'
$env:LOCALAPPDATA = Join-Path $taskRoot 'local'
New-Item -ItemType Directory -Force -Path $taskRoot,$env:CODEX_HOME,$env:CLAUDE_CONFIG_DIR,$env:LOCALAPPDATA | Out-Null
$taskProcess = $null
$taskSecond = $null
try {
    $taskProcess = Start-Process -FilePath $taskExe -ArgumentList '--background' -WindowStyle Hidden -PassThru
    $taskEnd = [DateTime]::UtcNow.AddSeconds(20)
    do {
        Start-Sleep -Milliseconds 200
        $taskProcess.Refresh()
        if ($taskProcess.HasExited) { throw 'Background process exited unexpectedly' }
        $taskWindow = [UniSwitchProductionWindow]::Find($taskProcess.Id)
    } until (((Test-Path -LiteralPath (Join-Path $env:UNI_SWITCH_DATA_DIR 'uni-switch.db')) -and $taskWindow -ne [IntPtr]::Zero) -or [DateTime]::UtcNow -gt $taskEnd)
    Start-Sleep -Milliseconds 600
    $taskProcess.Refresh()
    $taskBackgroundInvisible = $taskWindow -ne [IntPtr]::Zero -and -not [UniSwitchProductionWindow]::IsWindowVisible($taskWindow)
    $taskSecond = Start-Process -FilePath $taskExe -WindowStyle Hidden -PassThru
    if (-not $taskSecond.WaitForExit(10000)) { throw 'Second instance did not hand off' }
    $taskEnd = [DateTime]::UtcNow.AddSeconds(10)
    do {
        Start-Sleep -Milliseconds 200
        $taskProcess.Refresh()
        $taskWindow = [UniSwitchProductionWindow]::Find($taskProcess.Id)
    } until (($taskWindow -ne [IntPtr]::Zero -and [UniSwitchProductionWindow]::IsWindowVisible($taskWindow)) -or [DateTime]::UtcNow -gt $taskEnd)
    $taskSocket = [System.Net.Sockets.TcpClient]::new()
    $taskDebugPort = $false
    try { $taskConnect = $taskSocket.ConnectAsync('127.0.0.1',9223); $taskDebugPort = $taskConnect.Wait(500) -and $taskSocket.Connected } catch {} finally { $taskSocket.Dispose() }
    $taskResult = [ordered]@{
        version = $Version
        databaseInitialized = Test-Path -LiteralPath (Join-Path $env:UNI_SWITCH_DATA_DIR 'uni-switch.db')
        backgroundInvisible = $taskBackgroundInvisible
        repeatedOpenWakesWindow = $taskWindow -ne [IntPtr]::Zero -and [UniSwitchProductionWindow]::IsWindowVisible($taskWindow)
        secondProcessExited = $taskSecond.HasExited
        windowTitle = [UniSwitchProductionWindow]::Title($taskWindow)
        responding = $taskProcess.Responding
        qaDebugPortOpen = $taskDebugPort
        runDirectory = $taskRoot
    }
    $taskJson = $taskResult | ConvertTo-Json
    [IO.File]::WriteAllText((Join-Path $taskRoot 'results.json'),$taskJson,[Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path (Get-Location) '.qa/ux-flow/production-results.json'),$taskJson,[Text.UTF8Encoding]::new($false))
    $taskJson
    if (-not $taskResult.databaseInitialized -or -not $taskResult.backgroundInvisible -or -not $taskResult.repeatedOpenWakesWindow -or -not $taskResult.responding -or $taskResult.qaDebugPortOpen) { throw 'Production smoke failed' }
} finally {
    if ($taskSecond -and -not $taskSecond.HasExited) { Stop-Process -Id $taskSecond.Id }
    if ($taskProcess -and -not $taskProcess.HasExited) { Stop-Process -Id $taskProcess.Id }
}
