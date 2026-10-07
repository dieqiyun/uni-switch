# GitHub 程序发布

从 v0.5.19 起，按维护者要求只发布编译结果，本次及后续代码保留在本地，不推送新的源码，不上传 source.zip。仓库中原有源码和历史 Release 保留。

## 发布准备

更新前端、Rust、安装包版本和 CHANGELOG；完成前端、Rust、原生隔离验证。软件内教程与打包说明共用 tutorial.json，运行 `corepack pnpm docs:generate` 后以 `docs:check` 核对。

更新说明存为 `release/notes/版本.md`，完整使用说明存为 `release/notes/usage-版本.md`。两个文件均不进入 Git。提交本地改动以固定构建版本，不执行 git push。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/prepare-github-release.ps1 -Repository dieqiyun/uni-switch
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/qa-github-release.ps1 -StageDirectory release/github/v版本
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/publish-github-release.ps1 -Repository dieqiyun/uni-switch
```

默认固定上传五项：安装 EXE、独立 EXE、便携 ZIP、README-zh-CN.md、SHA256SUMS.txt。便携包只包含编译程序、说明、许可和第三方通知。manifest.json 保留本地构建提交与 includeSource=false，仅供本地核验，不上传。

发布脚本不执行源码推送。GitHub Release 标签只能指向仓库现有公开提交，因此平台自动生成的 Source code 是历史源码快照，不对应本次程序；更新说明明确标注这一点。程序校验以附件 SHA256 为准。

先建草稿、上传固定附件、下载回读并验证每项 SHA256，再公开和标记 Latest。失败可用 `-ResumeDraft` 继续未公开草稿，不覆盖公开版本。发布后确认最新版本、附件 digest / size，运行 `node scripts/qa-github-live.mjs` 和旧版本更新检测。

## 防止误传源码

准备和发布默认均不带源码。清单额外出现源码 ZIP 或其他非允许文件时停止；源码发布模式只在显式指定 `-IncludeSource` 且准备清单一致时允许，当前流程不使用此选项。

模拟发布测试覆盖五项清单、上传回读、损坏拒绝、草稿恢复、公开版本保护、无效远端目标拒绝和模式不一致拒绝，不访问真实 GitHub。参见 [QA 记录](qa-inventory.md)。

## 历史开源发布记录

v0.5.17 / v0.5.18 同时公开源码和程序，以下记录仅适用于当时版本。

## 0.5.17 发布结果

2026-10-07已公开发布v0.5.17并确认Latest。六项附件下载回读与SHA256一致，发布标签对应源码提交b7c9feece3c9051dbec003b5cbba1e2105751265。旧版0.5.16和新版0.5.17的真实桌面更新查询均通过，分别验证发现新版和当前版本。发布时GitHub暂时返回HTTP500；草稿和完整附件保留，服务恢复后成功公开。默认分支补上显式源码标签创建修订，未来发布不依赖GitHub草稿自动生成标签。


从 v0.5.18 起，使用说明共用 `src/content/tutorial.json`。更新教程后运行 `corepack pnpm docs:generate`，提交生成的 `docs/tutorial.md`；准备发布时会执行 `corepack pnpm docs:check`，不一致时阻止构建。便携包与 Release 的 `README-zh-CN.md` 同时包含完整入门与功能教程。赞助名单维护于 `README.md` 和 `docs/sponsors.md`。

2026-10-07已正式发布v0.5.18并确认Latest。6项真实附件的回读SHA256及公开digest/size一致，标签对应源码8971dbfa0a1c2049c9faa30c2014fe3a8d7593fe。新版0.5.18与旧版0.5.17均通过真实GitHub桌面更新查询。程序内教程、GitHub教程与赞助说明已同步，后续验证记录提交不更改发布标签与程序的对应关系。
