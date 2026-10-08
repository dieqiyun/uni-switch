param(
    [ValidatePattern('^(?:[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100})?$')][string]$Repository,
    [switch]$IncludeSource = $true
)
$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$OutputEncoding = [Text.UTF8Encoding]::new($false)
$taskWorkspace = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
function Write-Utf8([string]$Path, [string]$Text) {
    [IO.File]::WriteAllText($Path, $Text, [Text.UTF8Encoding]::new($false))
}
function Invoke-Git([string[]]$Arguments) {
    $taskOutput = & git -C $taskWorkspace @Arguments
    if ($LASTEXITCODE -ne 0) { throw 'Git源码核对失败' }
    return $taskOutput
}
$taskPackage = Get-Content -Raw -Encoding UTF8 (Join-Path $taskWorkspace 'package.json') | ConvertFrom-Json
$taskVersion = $taskPackage.version
if ($taskVersion -notmatch '^\d+\.\d+\.\d+$') { throw '版本必须为 x.y.z' }
$taskTauri = Get-Content -Raw -Encoding UTF8 (Join-Path $taskWorkspace 'src-tauri/tauri.conf.json') | ConvertFrom-Json
$taskCargo = Get-Content -Raw -Encoding UTF8 (Join-Path $taskWorkspace 'src-tauri/Cargo.toml')
if ($taskTauri.version -ne $taskVersion -or $taskCargo -notmatch ('(?m)^version = "' + [regex]::Escape($taskVersion) + '"')) {
    throw '前端、Rust与安装包的版本不一致'
}
if ($taskPackage.license -ne 'AGPL-3.0-only' -or $taskCargo -notmatch '(?m)^license = "AGPL-3.0-only"') { throw '项目开源许可不一致' }
$taskReleaseConfig = Get-Content -Raw -Encoding UTF8 (Join-Path $taskWorkspace 'release-config.json') | ConvertFrom-Json
if (-not $Repository) { $Repository = $taskReleaseConfig.githubRepository }
if (-not $Repository -or $Repository -ne $taskReleaseConfig.githubRepository) { throw '请先更新并提交release-config.json，再准备发布' }
$taskGitRoot = Invoke-Git @('rev-parse','--show-toplevel')
if ([IO.Path]::GetFullPath($taskGitRoot) -ne $taskWorkspace) { throw '发布必须从本项目Git工作区执行' }
if (Invoke-Git @('status','--porcelain')) { throw '源码包含未提交改动；请完成验证并提交后构建，以保证源码与程序对应' }
$taskRevision = Invoke-Git @('rev-parse','HEAD')
& corepack pnpm docs:check
if ($LASTEXITCODE -ne 0) { throw 'GitHub教程与软件内说明未同步，停止发布' }

& powershell -NoProfile -ExecutionPolicy Bypass -File (Join-Path $PSScriptRoot 'with-msvc.ps1') -Action build
if ($LASTEXITCODE -ne 0) { throw '正式构建失败，未准备发布文件' }
if (Invoke-Git @('status','--porcelain')) { throw '构建改变了源码或锁文件；请检查并提交后重新准备' }
$taskReleaseRoot = Join-Path $taskWorkspace 'release/windows-x64'
$taskExeName = "uni-switch_${taskVersion}_x64.exe"
$taskSetupName = "uni-switch_${taskVersion}_x64-setup.exe"
$taskZipName = "uni-switch_${taskVersion}_x64-portable.zip"
$taskSourceName = "uni-switch_${taskVersion}_source.zip"
$taskExe = Join-Path $taskWorkspace 'src-tauri/target/release/uni-switch.exe'
$taskSetup = Join-Path $taskWorkspace "src-tauri/target/release/bundle/nsis/$taskSetupName"
$taskGuide = Join-Path $taskWorkspace "release/notes/usage-$taskVersion.md"
foreach ($taskRequired in @($taskExe,$taskSetup,$taskGuide)) {
    if (-not (Test-Path -LiteralPath $taskRequired -PathType Leaf)) { throw "缺少交付文件：$taskRequired" }
}
$taskStage = Join-Path $taskWorkspace "release/github/v$taskVersion"
New-Item -ItemType Directory -Path $taskStage -Force | Out-Null
Copy-Item -LiteralPath $taskExe -Destination (Join-Path $taskStage $taskExeName) -Force
Copy-Item -LiteralPath $taskSetup -Destination (Join-Path $taskStage $taskSetupName) -Force
Copy-Item -LiteralPath $taskGuide -Destination (Join-Path $taskStage 'README-zh-CN.md') -Force
$taskPortable = Join-Path $taskStage ('portable-content-' + [Guid]::NewGuid().ToString('N'))
$taskLicenses = Join-Path $taskPortable 'licenses'
New-Item -ItemType Directory -Path $taskLicenses -Force | Out-Null
Copy-Item -LiteralPath $taskExe -Destination (Join-Path $taskPortable 'uni-switch.exe')
Copy-Item -LiteralPath $taskGuide -Destination (Join-Path $taskPortable '使用说明.md')
foreach ($taskNotice in @('LICENSE','NOTICE','THIRD_PARTY_NOTICES.md')) {
    Copy-Item -LiteralPath (Join-Path $taskWorkspace $taskNotice) -Destination (Join-Path $taskPortable $taskNotice)
}
Copy-Item -LiteralPath (Join-Path $taskWorkspace 'third-party/codex/LICENSE') -Destination (Join-Path $taskLicenses 'codex-LICENSE')
Copy-Item -LiteralPath (Join-Path $taskWorkspace 'third-party/dependency-licenses.txt') -Destination (Join-Path $taskLicenses 'dependency-licenses.txt')
Compress-Archive -Path (Join-Path $taskPortable '*') -DestinationPath (Join-Path $taskStage $taskZipName) -Force
if ($IncludeSource) {
    Invoke-Git @('archive','--format=zip',"--prefix=uni-switch-$taskVersion/",'-o',(Join-Path $taskStage $taskSourceName),$taskRevision) | Out-Null
}
$taskAssetNames = @($taskSetupName,$taskExeName,$taskZipName,'README-zh-CN.md','SHA256SUMS.txt')
if ($IncludeSource) { $taskAssetNames += $taskSourceName }
$taskHashLines = foreach ($taskAsset in ($taskAssetNames | Where-Object { $_ -ne 'SHA256SUMS.txt' })) {
    $taskHash = (Get-FileHash -LiteralPath (Join-Path $taskStage $taskAsset) -Algorithm SHA256).Hash.ToLowerInvariant()
    "$taskHash  $taskAsset"
}
Write-Utf8 (Join-Path $taskStage 'SHA256SUMS.txt') (($taskHashLines -join "`n") + "`n")
$taskManifestAssets = foreach ($taskAsset in $taskAssetNames) {
    $taskAssetPath = Join-Path $taskStage $taskAsset
    [PSCustomObject]@{ name = $taskAsset; sha256 = (Get-FileHash -LiteralPath $taskAssetPath -Algorithm SHA256).Hash.ToLowerInvariant(); size = (Get-Item -LiteralPath $taskAssetPath).Length }
}
$taskManifest = @{ version = $taskVersion; repository = $Repository; sourceRevision = $taskRevision; includeSource = [bool]$IncludeSource; license = 'AGPL-3.0-only'; assets = @($taskManifestAssets) }
Write-Utf8 (Join-Path $taskStage 'manifest.json') (($taskManifest | ConvertTo-Json -Depth 5) + "`n")
$taskNotesSource = Join-Path $taskWorkspace "release/notes/$taskVersion.md"
if (-not (Test-Path -LiteralPath $taskNotesSource)) { throw '缺少当前版本更新说明' }
Write-Utf8 (Join-Path $taskStage 'release-notes.md') ((Get-Content -Raw -Encoding UTF8 $taskNotesSource) + "`n")
New-Item -ItemType Directory -Path $taskReleaseRoot -Force | Out-Null
foreach ($taskAsset in $taskAssetNames) { Copy-Item -LiteralPath (Join-Path $taskStage $taskAsset) -Destination (Join-Path $taskReleaseRoot $taskAsset) -Force }
Write-Output "发布文件已准备：$taskStage"
Write-Output "本地构建提交：$taskRevision · 上传源码：$([bool]$IncludeSource)"
$taskManifestAssets | Format-Table name,size -AutoSize
