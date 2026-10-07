param([string]$StageDirectory)
$ErrorActionPreference = 'Stop'
[Console]::OutputEncoding = [Text.UTF8Encoding]::new($false)
$OutputEncoding = [Text.UTF8Encoding]::new($false)
$taskWorkspace = [IO.Path]::GetFullPath((Join-Path $PSScriptRoot '..'))
$taskVersion = (Get-Content -Raw -Encoding UTF8 (Join-Path $taskWorkspace 'package.json') | ConvertFrom-Json).version
$taskRunRoot = Join-Path $taskWorkspace ('.qa/github-release/' + [DateTimeOffset]::UtcNow.ToUnixTimeMilliseconds())
$taskStage = Join-Path $taskRunRoot "release/github/v$taskVersion"
$taskScripts = Join-Path $taskRunRoot 'scripts'
New-Item -ItemType Directory -Path $taskScripts -Force | Out-Null
New-Item -ItemType Directory -Path (Join-Path $taskRunRoot 'release/github') -Force | Out-Null
if (-not $StageDirectory) { $StageDirectory = Join-Path $taskWorkspace "release/github/v$taskVersion" }
Copy-Item -LiteralPath $StageDirectory -Destination (Join-Path $taskRunRoot 'release/github') -Recurse
Copy-Item -LiteralPath (Join-Path $PSScriptRoot 'publish-github-release.ps1') -Destination $taskScripts
$taskManifestPath = Join-Path $taskStage 'manifest.json'
$taskManifest = Get-Content -Raw -Encoding UTF8 $taskManifestPath | ConvertFrom-Json
$taskManifest.repository = 'example/uni-switch'
[IO.File]::WriteAllText($taskManifestPath,($taskManifest | ConvertTo-Json -Depth 5),[Text.UTF8Encoding]::new($false))
[IO.File]::WriteAllText((Join-Path $taskRunRoot 'package.json'),(@{version=$taskVersion} | ConvertTo-Json),[Text.UTF8Encoding]::new($false))
[IO.File]::WriteAllText((Join-Path $taskRunRoot 'release-config.json'),' {"githubRepository":"example/uni-switch"}',[Text.UTF8Encoding]::new($false))
$taskMockGh = @'
$global:LASTEXITCODE = 0
$mockRoot = $env:UNI_SWITCH_QA_PUBLISH_ROOT
if ($args[0] -eq 'auth') { return }
if ($args[0] -eq 'repo' -and $args[1] -eq 'view') { Write-Output 'main'; return }
if ($args[0] -eq 'api') {
    $endpoint = $args[1]
    if ($endpoint -eq 'repos/example/uni-switch') { Write-Output '{"private":false,"default_branch":"main"}'; return }
    if ($endpoint -like '*/git/refs' -and $args -contains 'POST') {
        [IO.File]::WriteAllText((Join-Path $mockRoot 'source-tag-created.txt'),'source tag created',[Text.UTF8Encoding]::new($false)); return
    }
    if ($endpoint -like '*/commits/*' -or $endpoint -like '*/git/ref/heads/*' -or $endpoint -like '*/tags?per_page*' -or $endpoint -like '*/contents/LICENSE*') {
        $manifest = Get-Content -Raw -Encoding UTF8 (Join-Path $mockRoot 'manifest.json') | ConvertFrom-Json
        if ($endpoint -like '*/commits/*') { @{sha=$manifest.sourceRevision} | ConvertTo-Json -Compress; return }
        if ($endpoint -like '*/git/ref/heads/*') {
            $revision = if ($env:UNI_SWITCH_QA_SOURCE_MISMATCH -eq '1') { 'wrong-source' } else { $manifest.sourceRevision }
            @{object=@{sha=$revision}} | ConvertTo-Json -Compress; return
        }
        if ($endpoint -like '*/tags?per_page*') {
            if (-not (Test-Path -LiteralPath (Join-Path $mockRoot 'source-tag-created.txt'))) { Write-Output '[]'; return }
            ConvertTo-Json -InputObject @(@{name=('v'+$manifest.version);commit=@{sha=$manifest.sourceRevision}}) -Depth 4 -Compress; return }
        @{content=[Convert]::ToBase64String([Text.Encoding]::UTF8.GetBytes('GNU AFFERO GENERAL PUBLIC LICENSE'))} | ConvertTo-Json -Compress; return
    }
    if ($endpoint -like '*/git/trees/*') { Write-Output '{"truncated":false,"tree":[{"type":"blob","path":"README.md"}]}'; return }
    if ($endpoint -like '*/contents/README.md') {
        if ($args -contains 'PUT') { return }
        Write-Output '{"sha":"mock-readme","content":""}'; return
    }
    if ($endpoint -like '*/releases?per_page*') {
        if (Test-Path -LiteralPath (Join-Path $mockRoot 'uploaded.json')) {
            $manifest = Get-Content -Raw -Encoding UTF8 (Join-Path $mockRoot 'manifest.json') | ConvertFrom-Json
            ConvertTo-Json -InputObject @(@{tag_name=('v'+$manifest.version);draft=$true;prerelease=$false;assets=@($manifest.assets)}) -Depth 5 -Compress
        } elseif ($env:UNI_SWITCH_QA_RESUME_DRAFT -eq '1') {
            $manifest = Get-Content -Raw -Encoding UTF8 (Join-Path $mockRoot 'manifest.json') | ConvertFrom-Json
            $assets = foreach($asset in $manifest.assets) {
                @{name=$asset.name;size=$asset.size}
            }
            ConvertTo-Json -InputObject @(@{tag_name=('v'+$manifest.version);draft=($env:UNI_SWITCH_QA_PUBLIC_RELEASE -ne '1');prerelease=$false;assets=@($assets)}) -Depth 5 -Compress
        } else { Write-Output '[]' }
        return
    }
    if ($endpoint -like '*/releases/assets/123' -and $args -contains 'DELETE') {
        [IO.File]::WriteAllText((Join-Path $mockRoot 'removed-legacy.txt'),'legacy draft guide replaced',[Text.UTF8Encoding]::new($false))
        return
    }
    if ($endpoint -like '*/releases/tags/*') {
        $manifest = Get-Content -Raw -Encoding UTF8 (Join-Path $mockRoot 'manifest.json') | ConvertFrom-Json
        @{ assets = @($manifest.assets) } | ConvertTo-Json -Depth 5 -Compress
        return
    }
    if ($endpoint -like '*/releases/latest') {
        $manifest = Get-Content -Raw -Encoding UTF8 (Join-Path $mockRoot 'manifest.json') | ConvertFrom-Json
        @{ tag_name = ('v'+$manifest.version); html_url = 'https://github.com/example/uni-switch/releases/tag/test'; draft=$false; prerelease=$false } | ConvertTo-Json -Compress
        return
    }
}
if ($args[0] -eq 'release' -and $args[1] -in @('create','upload')) {
    $assets = $args[($args.Count-6)..($args.Count-1)]
    $allowed = (Get-Content -Raw -Encoding UTF8 (Join-Path $mockRoot 'manifest.json') | ConvertFrom-Json).assets.name
    foreach($asset in $assets) {
        if ($allowed -cnotcontains [IO.Path]::GetFileName($asset)) { throw 'Mock detected an unlisted upload' }
        Copy-Item -LiteralPath $asset -Destination (Join-Path $mockRoot 'uploads') -Force
    }
    [IO.File]::WriteAllText((Join-Path $mockRoot 'uploaded.json'),($assets | ConvertTo-Json),[Text.UTF8Encoding]::new($false))
    return
}
if ($args[0] -eq 'release' -and $args[1] -eq 'download') {
    $index = [Array]::IndexOf($args,'--dir')
    $destination = $args[$index+1]
    Get-ChildItem -LiteralPath (Join-Path $mockRoot 'uploads') -File | ForEach-Object { Copy-Item -LiteralPath $_.FullName -Destination $destination -Force }
    if ($env:UNI_SWITCH_QA_CORRUPT_UPLOAD -eq '1') {
        $first = Get-ChildItem -LiteralPath $destination -Filter '*.exe' | Select-Object -First 1
        [IO.File]::WriteAllText($first.FullName,'corrupted test file',[Text.UTF8Encoding]::new($false))
    }
    return
}
if ($args[0] -eq 'release' -and $args[1] -eq 'edit') {
    if ($args -contains '--draft=false') {
        [IO.File]::WriteAllText((Join-Path $mockRoot 'published.txt'),'mock published',[Text.UTF8Encoding]::new($false))
    }
    return
}
throw 'Unexpected mock GitHub command'
'@
$taskMockPath = Join-Path $taskRunRoot 'mock-gh.ps1'
[IO.File]::WriteAllText($taskMockPath,$taskMockGh,[Text.UTF8Encoding]::new($true))
$taskMockRoot = Join-Path $taskRunRoot 'mock-api'
New-Item -ItemType Directory -Path (Join-Path $taskMockRoot 'uploads') -Force | Out-Null
Copy-Item -LiteralPath $taskManifestPath -Destination (Join-Path $taskMockRoot 'manifest.json')
$taskOldMockRoot = $env:UNI_SWITCH_QA_PUBLISH_ROOT
$taskOldCorrupt = $env:UNI_SWITCH_QA_CORRUPT_UPLOAD
$taskOldResume = $env:UNI_SWITCH_QA_RESUME_DRAFT
$taskOldPublic = $env:UNI_SWITCH_QA_PUBLIC_RELEASE
$taskOldMismatch = $env:UNI_SWITCH_QA_SOURCE_MISMATCH
$taskChecks = @()
try {
    $env:UNI_SWITCH_QA_PUBLISH_ROOT = $taskMockRoot
    $env:UNI_SWITCH_QA_RESUME_DRAFT = $null
    $env:UNI_SWITCH_QA_PUBLIC_RELEASE = $null
    $env:UNI_SWITCH_QA_SOURCE_MISMATCH = $null
    $env:UNI_SWITCH_QA_CORRUPT_UPLOAD = $null
    $taskPublisher = Join-Path $taskScripts 'publish-github-release.ps1'
    & $taskPublisher -Repository 'example/uni-switch' -GhPath $taskMockPath
    if (-not (Test-Path -LiteralPath (Join-Path $taskMockRoot 'published.txt')) -or -not (Test-Path -LiteralPath (Join-Path $taskMockRoot 'source-tag-created.txt'))) { throw 'Mock publication did not create source tag or complete' }
    $taskChecks += 'Local fake GitHub flow uploaded only the six allowed assets, including corresponding AGPL source, created the exact source tag, verified downloaded checksums, then published'
    # Use a separate mock directory for the failure scenario; no deletion needed.
    $taskFailureRoot = Join-Path $taskRunRoot 'mock-api-corrupt'
    New-Item -ItemType Directory -Path (Join-Path $taskFailureRoot 'uploads') -Force | Out-Null
    Copy-Item -LiteralPath $taskManifestPath -Destination (Join-Path $taskFailureRoot 'manifest.json')
    $env:UNI_SWITCH_QA_PUBLISH_ROOT = $taskFailureRoot
    $env:UNI_SWITCH_QA_CORRUPT_UPLOAD = '1'
    $taskRejected = $false
    try { & $taskPublisher -Repository 'example/uni-switch' -GhPath $taskMockPath } catch { $taskRejected = $true }
    if (-not $taskRejected -or (Test-Path -LiteralPath (Join-Path $taskFailureRoot 'published.txt'))) { throw 'Corrupted upload was not rejected' }
    $taskChecks += 'Corrupted downloaded asset stops publication and leaves the release in draft'
    $taskResumeRoot = Join-Path $taskRunRoot 'mock-api-resume'
    New-Item -ItemType Directory -Path (Join-Path $taskResumeRoot 'uploads') -Force | Out-Null
    Copy-Item -LiteralPath $taskManifestPath -Destination (Join-Path $taskResumeRoot 'manifest.json')
    $env:UNI_SWITCH_QA_PUBLISH_ROOT = $taskResumeRoot
    $env:UNI_SWITCH_QA_CORRUPT_UPLOAD = $null
    $env:UNI_SWITCH_QA_RESUME_DRAFT = '1'
    & $taskPublisher -Repository 'example/uni-switch' -GhPath $taskMockPath -ResumeDraft
    if (-not (Test-Path -LiteralPath (Join-Path $taskResumeRoot 'published.txt'))) { throw 'Draft resume did not finish verification' }
    $taskChecks += 'Draft resume uploads and verifies the current six assets before publishing'
    $taskPublicRoot = Join-Path $taskRunRoot 'mock-api-public'
    New-Item -ItemType Directory -Path (Join-Path $taskPublicRoot 'uploads') -Force | Out-Null
    Copy-Item -LiteralPath $taskManifestPath -Destination (Join-Path $taskPublicRoot 'manifest.json')
    $env:UNI_SWITCH_QA_PUBLISH_ROOT = $taskPublicRoot
    $env:UNI_SWITCH_QA_PUBLIC_RELEASE = '1'
    $taskPublicRejected = $false
    try { & $taskPublisher -Repository 'example/uni-switch' -GhPath $taskMockPath -ResumeDraft } catch { $taskPublicRejected = $true }
    if (-not $taskPublicRejected -or (Test-Path -LiteralPath (Join-Path $taskPublicRoot 'uploaded.json'))) { throw 'Published release was overwritten' }
    $taskChecks += 'Draft resume refuses to overwrite an already public release'
    $taskMismatchRoot = Join-Path $taskRunRoot 'mock-api-source-mismatch'
    New-Item -ItemType Directory -Path (Join-Path $taskMismatchRoot 'uploads') -Force | Out-Null
    Copy-Item -LiteralPath $taskManifestPath -Destination (Join-Path $taskMismatchRoot 'manifest.json')
    $env:UNI_SWITCH_QA_PUBLISH_ROOT = $taskMismatchRoot
    $env:UNI_SWITCH_QA_PUBLIC_RELEASE = $null
    $env:UNI_SWITCH_QA_RESUME_DRAFT = $null
    $env:UNI_SWITCH_QA_SOURCE_MISMATCH = '1'
    $taskMismatchRejected = $false
    try { & $taskPublisher -Repository 'example/uni-switch' -GhPath $taskMockPath } catch { $taskMismatchRejected = $true }
    if (-not $taskMismatchRejected -or (Test-Path -LiteralPath (Join-Path $taskMismatchRoot 'uploaded.json'))) { throw 'Source mismatch was not rejected before upload' }
    $taskChecks += 'Remote source mismatch prevents any upload or publication'
    $taskResult = @{ version=$taskVersion; root=$taskRunRoot; checks=$taskChecks; externalRequests=0 }
    [IO.File]::WriteAllText((Join-Path $taskRunRoot 'results.json'),($taskResult | ConvertTo-Json -Depth 4),[Text.UTF8Encoding]::new($false))
    [IO.File]::WriteAllText((Join-Path $taskWorkspace '.qa/github-release/results.json'),($taskResult | ConvertTo-Json -Depth 4),[Text.UTF8Encoding]::new($false))
    $taskResult | ConvertTo-Json -Depth 4
} finally {
    $env:UNI_SWITCH_QA_PUBLISH_ROOT = $taskOldMockRoot
    $env:UNI_SWITCH_QA_CORRUPT_UPLOAD = $taskOldCorrupt
    $env:UNI_SWITCH_QA_RESUME_DRAFT = $taskOldResume
    $env:UNI_SWITCH_QA_PUBLIC_RELEASE = $taskOldPublic
    $env:UNI_SWITCH_QA_SOURCE_MISMATCH = $taskOldMismatch
}
