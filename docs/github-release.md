# GitHub 开源与程序发布

自 v0.5.17 起，`dieqiyun/uni-switch` 同时公开完整源码和 Windows 程序，项目许可为 AGPL-3.0-only。旧的“仅分发程序、不推送源码”流程已被替换。

## 终端用户

启动时检查 GitHub 最新正式版；左下角版本入口可手动检测。更新检查不使用供应商认证，过滤草稿及预发布，按数值比较版本。下载入口打开对应发布页，目前由用户下载并安装，不在后台替换程序。

GitHub 公开仓库、源码标签、Release 中的对应源码 ZIP 均可获取本版本源码。程序设置提供源码、完整许可、版权和无保证声明；安装版和便携版附带许可与第三方通知。

## 发布准备

先更新 `package.json`、`src-tauri/Cargo.toml`、`src-tauri/Cargo.lock`、`src-tauri/tauri.conf.json` 的版本，更新 `CHANGELOG.md`。更新来源在 `release-config.json`，必须在提交构建前绑定实际仓库。

将中文使用说明保存到 `release/notes/usage-版本.md`，更新说明保存到 `release/notes/版本.md`。这两个暂存文件不进入 Git；正式说明同时记录在 `CHANGELOG.md`，说明随 Release 分发。安装依赖后可运行 `python -X utf8 scripts/collect-dependency-licenses.py` 更新依赖许可清单，清单本身需要提交。

完成测试、检查公开文件清单，运行 `python -X utf8 scripts/audit-source.py` 扫描已暂存文件中的常见凭据格式，然后提交完整源码。该扫描不打印匹配值，也不能替代人工检查。发布脚本要求干净工作区，构建不会接触用户真实客户端配置。

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/prepare-github-release.ps1 -Repository dieqiyun/uni-switch
```

准备脚本从当前提交构建并用 `git archive` 生成对应源码 ZIP，检查构建是否改变源码或锁文件。输出在 `release/github/v版本/`。

附件固定为六项：安装 EXE、独立 EXE、便携 ZIP、对应源码 ZIP、`README-zh-CN.md`、`SHA256SUMS.txt`。源码 ZIP 包含完整源码、锁文件、构建脚本、资源和许可；排除凭据、数据库、客户端配置、缓存、测试运行数据及程序包。

## 发布

使用 GitHub CLI 登录自己的账号，将完整源码推送到仓库默认分支。发布提交必须与准备时的提交一致。

```powershell
git push -u origin main
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/publish-github-release.ps1 -Repository dieqiyun/uni-switch
```

脚本核对仓库、远端源码提交、AGPL 许可、源码归档、便携包和六项附件的 SHA256。先显式创建对应源码提交的标签（GitHub 草稿自身不会创建标签），再创建草稿并指定源码提交，上传后下载所有附件核对校验值，检查标签确实指向对应源码，再公开并标记 Latest。不会覆盖已公开的同版本程序。

失败可用 `-ResumeDraft` 继续同版本未公开草稿。只恢复清单内的附件，校验失败继续保留草稿。版本发布成功后应从公开 API 确认最新版本、源码标签和附件；再运行 `node scripts/qa-github-live.mjs` 验证软件的真实更新查询。

## 本地验证

`scripts/qa-github-release.ps1` 使用模拟 GitHub CLI，验证正常发布、损坏附件拒绝、草稿恢复、已发布版本拒绝，以及缺少对应源码时拒绝发布。无真实外部写入。

软件的原生隔离模型、协议与更新验证说明见 `docs/qa-inventory.md`。历史的 v0.5.16 仅程序分发记录保留在更新记录中，不覆盖旧版附件。

## 0.5.17 发布结果

2026-10-07已公开发布v0.5.17并确认Latest。六项附件下载回读与SHA256一致，发布标签对应源码提交b7c9feece3c9051dbec003b5cbba1e2105751265。旧版0.5.16和新版0.5.17的真实桌面更新查询均通过，分别验证发现新版和当前版本。发布时GitHub暂时返回HTTP500；草稿和完整附件保留，服务恢复后成功公开。默认分支补上显式源码标签创建修订，未来发布不依赖GitHub草稿自动生成标签。


从 v0.5.18 起，使用说明共用 `src/content/tutorial.json`。更新教程后运行 `corepack pnpm docs:generate`，提交生成的 `docs/tutorial.md`；准备发布时会执行 `corepack pnpm docs:check`，不一致时阻止构建。便携包与 Release 的 `README-zh-CN.md` 同时包含完整入门与功能教程。赞助名单维护于 `README.md` 和 `docs/sponsors.md`。
