# 参与 uni-switch 开发

欢迎提交问题报告、文档修订和代码改进。贡献通过 **Fork → 分支开发 → Pull Request → 维护者审核 → 合并** 完成；最终由 [@dieqiyun](https://github.com/dieqiyun) 决定是否合并，CI 通过不会自动合并。

## 提交前

- 缺陷报告请包含系统、uni-switch / 客户端版本、复现步骤、预期行为和脱敏后的错误信息。
- 较大的功能或架构改动先开 Issue 沟通范围；小修复可以直接提交 PR。
- 一个 PR 聚焦一件事，避免夹带无关重构、版本升级或生成文件。请保留 AGPL-3.0-only 和第三方许可声明。
- 不要提交真实 API Key、Access Token、用户 ID、客户端配置、数据库、日志或私有截图。

## 环境与启动

项目使用 Tauri 2、Rust、React、TypeScript、Vite 和 SQLite。

- Node.js **22**、Corepack / pnpm **10.12.3**（由 package.json 指定）。
- Rust stable，最低 **1.89**；安装 Clippy。
- Windows：Visual Studio C++ Build Tools、Windows SDK、WebView2。
- Linux：WebKitGTK 4.1 / GTK 和构建工具，参考下方命令；CI 使用 Ubuntu 22.04。
- macOS：Xcode Command Line Tools；CI 使用 macOS 15。

Fork 本仓库后，克隆自己的 Fork 并创建功能分支：

```bash
git clone https://github.com/YOUR_USERNAME/uni-switch.git
cd uni-switch
git remote add upstream https://github.com/dieqiyun/uni-switch.git
git switch -c fix/short-description
corepack enable
corepack pnpm install --frozen-lockfile
```

Linux 桌面依赖：

```bash
sudo apt-get update
sudo apt-get install -y libwebkit2gtk-4.1-dev build-essential libssl-dev libayatana-appindicator3-dev librsvg2-dev patchelf libxdo-dev
```

仅预览界面：`corepack pnpm dev:web`；浏览器预览不会写入真实客户端配置。桌面开发入口为 `corepack pnpm dev`，包含真实配置写入能力，验证配置行为时应使用下述隔离 QA 方式。

## 本地验证

所有 PR（包括文档）都运行同一套检查，不按文件路径跳过必需检查：

```bash
corepack pnpm docs:check
corepack pnpm typecheck
corepack pnpm test
corepack pnpm build
cargo test --locked --manifest-path src-tauri/Cargo.toml --no-default-features
cargo clippy --locked --manifest-path src-tauri/Cargo.toml --all-targets -- -D warnings
```

Windows 如终端未加载 MSVC 环境，使用仓库脚本（分别执行，Cargo 不要并发）：

```powershell
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/with-msvc.ps1 -Action test
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/with-msvc.ps1 -Action clippy
```

Windows PowerShell 5 读写中文文件时显式使用 UTF-8：读取用 `Get-Content -Raw -Encoding UTF8`；写入用 `[IO.File]::WriteAllText(..., [Text.UTF8Encoding]::new($false))`。完整规则见 [AGENTS.md](AGENTS.md)。

## 修改教程、配置或协议

- 软件内教程的内容源位于 `src/content/tutorial.json`。修改后运行 `corepack pnpm docs:generate`，再提交源文件与生成的 `docs/tutorial.md`；不要单独修改生成页。
- 配置写入应保留无关字段、处理外部修改和失败回滚；使用记录要区分全局默认配置与会话实际请求。
- 协议转换要验证工具调用、流式响应、图片、错误、取消和认证；全部使用模拟供应商和虚构凭据，常规测试不调用付费服务。
- 涉及布局和交互时提供截图，验证键盘操作、窄窗口和可访问性。

原生 UI QA 当前主要在 Windows 执行。先构建带隔离能力的 QA 程序，再按改动选择脚本，例如：

```powershell
corepack pnpm build
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/with-msvc.ps1 -Action qa
node scripts/qa-model-dialog.mjs
node scripts/qa-app-update-remote.mjs
```

这些脚本负责建立 `.qa` 隔离目录、模拟供应商和测试配置。不要重启、关闭或修改真实 Codex / Claude / uni-switch。UI 脚本共享 CDP 端口 9223，必须串行；若端口被其他进程占用，请先结束自己启动的上一轮 QA，不能结束用户进程。测试范围和已知限制见 [验证清单](docs/qa-inventory.md)。

## PR 和审核

1. 提交并推送功能分支，向上游 `main` 创建 PR；尚未完成可以先开 Draft。
2. 按 PR 模板说明问题、改动、验证、影响范围和未完成项；不要勾选没有执行的验证。
3. 等待 `Frontend`、三平台 `Rust` 检查及汇总 `PR checks` 成功。首次从 Fork 贡献可能需要维护者批准运行工作流。
4. 维护者 @dieqiyun 审核。新增修改会使先前批准失效，需要重新审核；审核讨论需处理完成。
5. 根据提示将分支更新到最新 `main`，再通过检查；维护者决定是否合并。

Fork PR 的工作流只有只读仓库权限，不使用发布凭据，也不发布 Release。当前不开放外部贡献者直推主分支。主分支要求通过 PR 修改，禁止强推和删除。

## 合并与发布

PR 合并不等于程序发布。版本、正式标签和 Release 由维护者决定，仍使用三平台原生发布工作流；三平台构建及附件校验成功后才公开，源码与程序必须对应同一提交。详见 [发布说明](docs/github-release.md)。

维护者审核与分支规则的具体设置见 [维护者操作指南](docs/maintainer-workflow.md)。
