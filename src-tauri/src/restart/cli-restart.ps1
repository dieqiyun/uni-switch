$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [System.Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$OutputEncoding = [System.Text.UTF8Encoding]::new($false)
$Host.UI.RawUI.WindowTitle = 'uni-switch · 重启 Claude CLI'
try {
    $taskPid = [int]$env:UNI_SWITCH_CLI_PID
    $taskStarted = [long]$env:UNI_SWITCH_CLI_STARTED
    $taskImage = $env:UNI_SWITCH_CLI_IMAGE
    $taskWorking = $env:UNI_SWITCH_CLI_WORKING
    $taskArguments = @(ConvertFrom-Json $env:UNI_SWITCH_CLI_ARGUMENTS)
    $taskClient = Get-Process -Id $taskPid -ErrorAction SilentlyContinue
    Write-Host '新配置已保存。此终端将在旧 Claude CLI 退出后恢复会话。' -ForegroundColor Cyan
    if ($taskClient) {
        $taskActualStart = ([DateTimeOffset]$taskClient.StartTime.ToUniversalTime()).ToUnixTimeMilliseconds()
        if ($taskActualStart -ne $taskStarted -or $taskClient.Path -ne $taskImage) {
            throw '原 CLI 进程已发生变化，请从 uni-switch 重新发起重启。'
        }
        Write-Host '请回到原 Claude CLI，输入 /exit 正常退出。' -ForegroundColor Yellow
        Write-Host '旧终端和其他任务会保留。关闭本窗口可以取消等待。'
        $taskClient.WaitForExit()
    }
    if (-not (Test-Path -LiteralPath $taskWorking -PathType Container)) {
        throw '原工作目录已不可用，请手动选择项目目录后启动 Claude CLI。'
    }
    Set-Location -LiteralPath $taskWorking
    [Environment]::CurrentDirectory = $taskWorking
    foreach ($taskVariable in @('UNI_SWITCH_CLI_PID', 'UNI_SWITCH_CLI_STARTED', 'UNI_SWITCH_CLI_IMAGE', 'UNI_SWITCH_CLI_WORKING', 'UNI_SWITCH_CLI_ARGUMENTS')) {
        [Environment]::SetEnvironmentVariable($taskVariable, $null, 'Process')
    }
    Write-Host '正在原工作目录加载新配置并恢复会话…' -ForegroundColor Cyan
    & $taskImage @taskArguments
    if ($LASTEXITCODE -ne 0) {
        Write-Host 'Claude CLI 已退出。若没有可恢复的历史会话，可以直接启动新会话：' -ForegroundColor Yellow
        if ($taskArguments.Count -gt 0 -and $taskArguments[0] -like '*.js') {
            Write-Host ('& "' + $taskImage + '" "' + $taskArguments[0] + '"')
        } else {
            Write-Host ('& "' + $taskImage + '"')
        }
        Read-Host '按回车关闭此窗口'
    }
} catch {
    Write-Host ('重启未完成：' + $_.Exception.Message) -ForegroundColor Red
    Write-Host '新配置已保存。请在原工作目录重新运行 claude --continue，或运行 claude 开始新会话。'
    Read-Host '按回车关闭此窗口'
}
