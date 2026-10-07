param(
    [Parameter(Mandatory=$true)][ValidatePattern('^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9._-]{1,100}$')][string]$Repository,
    [switch]$ResumeDraft,
    [string]$GhPath
)
$ErrorActionPreference = 'Stop'
[Console]::InputEncoding = [Text.UTF8Encoding]::new($false)
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$OutputEncoding = [Text.UTF8Encoding]::new($false)
$taskWorkspace = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
if (-not $GhPath) {
    $taskGhCommand = Get-Command gh -ErrorAction SilentlyContinue
    if ($taskGhCommand) { $GhPath = $taskGhCommand.Source }
    else { $GhPath = Join-Path $taskWorkspace '.tools/github-cli/bin/gh.exe' }
}
if (-not (Test-Path -LiteralPath $GhPath -PathType Leaf)) { throw '未找到GitHub CLI，请先安装gh并执行gh auth login' }
function Invoke-Gh([string[]]$Arguments) {
    $taskResult = & $GhPath @Arguments
    if ($LASTEXITCODE -ne 0) { throw ('GitHub操作失败：' + ($Arguments | Select-Object -First 2) -join ' ') }
    return $taskResult
}
Invoke-Gh @('auth','status','--hostname','github.com') | Out-Null
$taskVersion = (Get-Content -Raw -Encoding UTF8 (Join-Path $taskWorkspace 'package.json') | ConvertFrom-Json).version
$taskStage = Join-Path $taskWorkspace "release/github/v$taskVersion"
$taskManifest = Get-Content -Raw -Encoding UTF8 (Join-Path $taskStage 'manifest.json') | ConvertFrom-Json
$taskConfig = Get-Content -Raw -Encoding UTF8 (Join-Path $taskWorkspace 'release-config.json') | ConvertFrom-Json
if ($taskManifest.repository -ne $Repository -or $taskConfig.githubRepository -ne $Repository -or $taskManifest.version -ne $taskVersion -or $taskManifest.license -ne 'AGPL-3.0-only' -or $taskManifest.sourceRevision -notmatch '^[0-9a-f]{40}$') {
    throw '发布文件尚未绑定此仓库、许可或源码提交，请先准备发布文件'
}
$taskAllowlist = @("uni-switch_${taskVersion}_x64-setup.exe","uni-switch_${taskVersion}_x64.exe","uni-switch_${taskVersion}_x64-portable.zip","uni-switch_${taskVersion}_source.zip",'README-zh-CN.md','SHA256SUMS.txt')
if (@($taskManifest.assets).Count -ne $taskAllowlist.Count) { throw '发布清单必须包含程序、对应源码、说明与校验文件' }
$taskAssetPaths = foreach ($taskName in $taskAllowlist) {
    $taskEntry = @($taskManifest.assets | Where-Object { $_.name -ceq $taskName })
    if ($taskEntry.Count -ne 1) { throw "缺少或重复发布资产：$taskName" }
    $taskPath = Join-Path $taskStage $taskName
    if ((Get-FileHash -LiteralPath $taskPath -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskEntry[0].sha256 -or (Get-Item -LiteralPath $taskPath).Length -ne $taskEntry[0].size) { throw "资产校验失败：$taskName" }
    $taskPath
}
Add-Type -AssemblyName System.IO.Compression.FileSystem
$taskPortableArchive = [IO.Compression.ZipFile]::OpenRead((Join-Path $taskStage "uni-switch_${taskVersion}_x64-portable.zip"))
try {
    $taskZipAllowlist = @('uni-switch.exe','使用说明.md','LICENSE','NOTICE','THIRD_PARTY_NOTICES.md','licenses/','licenses/codex-LICENSE','licenses/dependency-licenses.txt')
    $taskZipNames = @($taskPortableArchive.Entries | ForEach-Object { $_.FullName.Replace('\','/') })
    foreach ($taskName in $taskZipNames) { if ($taskZipAllowlist -cnotcontains $taskName) { throw '便携包包含清单以外的文件，停止发布' } }
    foreach ($taskName in ($taskZipAllowlist | Where-Object { -not $_.EndsWith('/') })) { if (@($taskZipNames | Where-Object { $_ -ceq $taskName }).Count -ne 1) { throw '便携包缺少程序或许可文件' } }
} finally { $taskPortableArchive.Dispose() }
$taskSourceArchive = [IO.Compression.ZipFile]::OpenRead((Join-Path $taskStage "uni-switch_${taskVersion}_source.zip"))
try {
    $taskSourcePrefix = "uni-switch-$taskVersion/"
    $taskSourceNames = @($taskSourceArchive.Entries | ForEach-Object { $_.FullName })
    foreach ($taskName in $taskSourceNames) {
        if (-not $taskName.StartsWith($taskSourcePrefix,[StringComparison]::Ordinal) -or $taskName -match '(^|/)(\.git|\.qa|\.tools|node_modules|target|release|output|\.codex|\.claude)(/|$)|(^|/)\.env|\.(exe|db|db-shm|db-wal|pem|pfx|p12)$|(^|/)\.\.(/|$)') { throw '源码包包含本地配置、凭据、构建目录或不安全路径，停止发布' }
    }
    foreach ($taskRequired in @('LICENSE','NOTICE','package.json','pnpm-lock.yaml','src/App.tsx','src-tauri/Cargo.toml','src-tauri/Cargo.lock','scripts/with-msvc.ps1','third-party/dependency-licenses.txt')) {
        if (@($taskSourceNames | Where-Object { $_ -ceq ($taskSourcePrefix + $taskRequired) }).Count -ne 1) { throw "源码归档不完整：$taskRequired" }
    }
} finally { $taskSourceArchive.Dispose() }
$taskRepo = (Invoke-Gh @('api',"repos/$Repository") | Out-String) | ConvertFrom-Json
if ($taskRepo.full_name -and $taskRepo.full_name -ne $Repository) { throw 'GitHub仓库已经更名，请使用当前地址重新构建' }
if ($taskRepo.private) { throw '更新检测需要公开发布仓库' }
# The complete source must already be public, and match the exact build commit.
$taskSource = (Invoke-Gh @('api',"repos/$Repository/commits/$($taskManifest.sourceRevision)") | Out-String) | ConvertFrom-Json
$taskHead = (Invoke-Gh @('api',"repos/$Repository/git/ref/heads/$($taskRepo.default_branch)") | Out-String) | ConvertFrom-Json
if ($taskSource.sha -ne $taskManifest.sourceRevision -or $taskHead.object.sha -ne $taskManifest.sourceRevision) { throw '请先推送完整源码到默认分支；发布提交必须与构建提交一致' }
$taskRemoteLicense = (Invoke-Gh @('api',"repos/$Repository/contents/LICENSE?ref=$($taskManifest.sourceRevision)") | Out-String) | ConvertFrom-Json
$taskLicenseText = [Text.Encoding]::UTF8.GetString([Convert]::FromBase64String(($taskRemoteLicense.content -replace '\s','')))
if ($taskLicenseText -notmatch 'GNU AFFERO GENERAL PUBLIC LICENSE') { throw '公开源码的AGPL许可未确认' }
$taskTag = "v$taskVersion"
$taskExisting = Invoke-Gh @('api',"repos/$Repository/releases?per_page=100") | Out-String | ConvertFrom-Json
$taskMatching = @($taskExisting | Where-Object { $_.tag_name -eq $taskTag })
if ($taskMatching.Count -and (-not $ResumeDraft -or $taskMatching.Count -ne 1 -or -not $taskMatching[0].draft -or $taskMatching[0].prerelease)) { throw '该版本已有Release；仅可继续未公开草稿，不能覆盖已发布版本' }
if ($ResumeDraft -and -not $taskMatching.Count) { throw '未找到要继续的发布草稿' }
if ($taskMatching.Count -and @($taskMatching[0].assets | Where-Object { $taskAllowlist -cnotcontains $_.name }).Count) { throw '草稿包含清单以外的附件，未修改' }
$taskRemoteTags = Invoke-Gh @('api',"repos/$Repository/tags?per_page=100") | Out-String | ConvertFrom-Json
$taskTagMatches = @($taskRemoteTags | Where-Object { $_.name -ceq $taskTag })
if ($taskTagMatches.Count -and ($taskTagMatches.Count -ne 1 -or $taskTagMatches[0].commit.sha -ne $taskManifest.sourceRevision)) { throw '发布标签指向其他提交，停止发布' }
if (-not $taskTagMatches.Count) {
    # GitHub does not create the tag while a release is still a draft.
    # Create a lightweight source tag explicitly, never overwrite an existing tag.
    Invoke-Gh @('api',"repos/$Repository/git/refs",'--method','POST','-f',"ref=refs/tags/$taskTag",'-f',"sha=$($taskManifest.sourceRevision)") | Out-Null
}
Write-Output "上传程序与对应源码：$Repository / $taskTag"
if ($taskMatching.Count) {
    Invoke-Gh @('release','edit',$taskTag,'--repo',$Repository,'--title',"uni-switch $taskVersion",'--notes-file',(Join-Path $taskStage 'release-notes.md'),'--target',$taskManifest.sourceRevision,'--draft=true') | Out-Null
    $taskAssetsToUpload = foreach ($taskAsset in $taskManifest.assets) {
        $taskAlready = @($taskMatching[0].assets | Where-Object { $_.name -ceq $taskAsset.name -and $_.size -eq $taskAsset.size -and $_.digest -ceq ('sha256:' + $taskAsset.sha256) })
        if ($taskAlready.Count -ne 1) { Join-Path $taskStage $taskAsset.name }
    }
    if (@($taskAssetsToUpload).Count) { Invoke-Gh (@('release','upload',$taskTag,'--repo',$Repository,'--clobber') + @($taskAssetsToUpload)) | Out-Null }
} else {
    Invoke-Gh (@('release','create',$taskTag,'--repo',$Repository,'--target',$taskManifest.sourceRevision,'--title',"uni-switch $taskVersion",'--notes-file',(Join-Path $taskStage 'release-notes.md'),'--draft') + @($taskAssetPaths)) | Out-Null
}
$taskVerifyDir = Join-Path $taskStage ('verify-upload-' + [Guid]::NewGuid().ToString('N'))
New-Item -ItemType Directory -Path $taskVerifyDir | Out-Null
Write-Output '下载草稿附件并核对SHA256，成功后公开Release。'
Invoke-Gh @('release','download',$taskTag,'--repo',$Repository,'--dir',$taskVerifyDir) | Out-Null
foreach ($taskEntry in $taskManifest.assets) {
    if ((Get-FileHash -LiteralPath (Join-Path $taskVerifyDir $taskEntry.name) -Algorithm SHA256).Hash.ToLowerInvariant() -ne $taskEntry.sha256) { throw '回读校验失败，Release保留草稿，未公开' }
}
$taskDraftList = Invoke-Gh @('api',"repos/$Repository/releases?per_page=100") | Out-String | ConvertFrom-Json
$taskDrafts = @($taskDraftList | Where-Object { $_.tag_name -eq $taskTag })
if ($taskDrafts.Count -ne 1 -or -not $taskDrafts[0].draft -or $taskDrafts[0].prerelease -or @($taskDrafts[0].assets).Count -ne $taskAllowlist.Count) { throw '无法确认草稿或附件数量，停止发布' }
foreach ($taskName in $taskAllowlist) { if (@($taskDrafts[0].assets | Where-Object { $_.name -ceq $taskName }).Count -ne 1) { throw 'GitHub附件名称与清单不一致，未公开' } }
$taskFinalTag = Invoke-Gh @('api',"repos/$Repository/tags?per_page=100") | Out-String | ConvertFrom-Json
if (@($taskFinalTag | Where-Object { $_.name -ceq $taskTag -and $_.commit.sha -eq $taskManifest.sourceRevision }).Count -ne 1) { throw '发布标签与对应源码不一致，未公开' }
Invoke-Gh @('release','edit',$taskTag,'--repo',$Repository,'--draft=false','--latest') | Out-Null
$taskLatest = Invoke-Gh @('api',"repos/$Repository/releases/latest") | Out-String | ConvertFrom-Json
if ($taskLatest.tag_name -ne $taskTag -or $taskLatest.draft -or $taskLatest.prerelease) { throw '正式发布已提交，但latest查询尚未确认' }
Write-Output "已发布：$($taskLatest.html_url)"
Write-Output "对应源码：$($taskManifest.sourceRevision) · AGPL-3.0-only"
