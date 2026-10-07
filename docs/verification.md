# uni-switch 0.1.0 验证记录

验证日期：2026-10-06。构建平台：Windows x64。

已生成可运行的 Tauri 桌面程序及中文 NSIS 安装包。此次所有写入测试均使用临时或 `.qa` 下的隔离目录，没有修改用户真实 Codex 或 Claude 配置，没有使用真实 API Key，也没有自动发送计费请求。

## 交付文件

| 文件                                                 | 用途                                             |
| ---------------------------------------------------- | ------------------------------------------------ |
| `release/windows-x64/uni-switch_0.1.0_x64-setup.exe` | 当前用户安装包                                   |
| `release/windows-x64/uni-switch_0.1.0_x64.exe`       | 可直接运行的桌面程序，使用相同的本地应用数据目录 |
| `release/windows-x64/SHA256SUMS.txt`                 | 交付文件校验值                                   |

构建原始产物位于 `src-tauri/target/release/`。安装包约 2.22 MiB，独立程序约 7.85 MiB。发布构建未启用 `qa-webview`，不开放测试用 WebView 调试端口；此次产物未配置代码签名。

发布程序已在隔离应用数据目录启动，窗口正常响应，SQLite 与应用数据初始化成功。未运行安装向导，也未构建或验收 macOS / Linux 产物。

## 已完成的检查

| 检查                       | 结果与覆盖                                                                              |
| -------------------------- | --------------------------------------------------------------------------------------- |
| TypeScript / Vite 生产构建 | 通过                                                                                    |
| Vitest                     | 7 个测试通过：输入校验、表单提交、编辑时空密钥保留                                      |
| Rust 核心测试              | 9 个测试通过：各目标写入/恢复、配置保留、输入拒绝、冲突、进程中断恢复、Windows 文件占用 |
| Cargo Clippy               | 所有目标通过，`-D warnings`，包含桌面构建功能                                           |
| Rustfmt / Prettier         | 通过                                                                                    |
| 浏览器 UI                  | 8 组检查通过，无页面错误；1120、760、390 宽度无横向溢出                                 |
| Axe 可访问性               | 初始页面及已打开的表单均为 0 个违规项；另外检查 Escape、Tab、焦点返回                   |
| 原生 Tauri WebView         | 8 组检查通过，真实 IPC 与隔离文件写入，无页面错误                                       |
| Codex CLI 0.160.0          | 无登录与模拟已有登录两种状态均完成本地模拟 Responses 流式请求                           |
| Windows 发布构建           | 原生程序和 NSIS 安装包生成成功，发布程序启动检查通过                                    |

核心文件操作测试覆盖：Codex TOML 注释、MCP、项目字段与登录文件保留；Claude 桌面端四文件写入和恢复，保留 MCP 及后来新增的无关项；Claude CLI 认证切换与 hooks 保留；无效 JSON 时不写任何目标文件；外部修改阻止覆盖，导入后可接受当前状态；部分文件写入后中断的回滚，以及文件已写完后的数据库提交恢复；中文及空格目录、Windows 文件被占用时原文件保持完整。

原生桌面检查操作了 uni-switch 本身的真实 WebView，验证了添加、编辑、导入、应用、删除保护、目录绑定、认证切换和恢复。它验证的是配置工具及写入文件，未启动 Claude 桌面客户端进行模型调用。

## Codex CLI 请求验证

`scripts/qa-codex-cli.mjs` 从 uni-switch 后端生成的操作备份中读取目标 TOML，只将测试地址替换为本机 HTTP 服务。使用独立 `CODEX_HOME` 运行已安装的 Codex CLI 0.160.0，并验证：

1. 请求路径为 `/v1/responses`。
2. HTTP Bearer 认证使用所选的合成测试 Key。
3. 请求模型使用所选 `test-model`。
4. CLI 正常接收模拟流式回复，进程成功退出。
5. 无登录状态及模拟已有登录状态均通过；已有登录时配置由真实后端生成 `requires_openai_auth = true`，原合成 `auth.json` 保持不变，第三方推理请求仍使用所选 Key。

测试只对其子进程排除继承的代理环境变量，避免 localhost 请求走代理；不改变用户系统代理。已有登录验证使用合成 token，不代表真实订阅登录的桌面 UI 流程已验证。未验证真实供应商响应、工具调用与长会话。

默认结果记录在 `.qa/codex-cli-results.json`；模拟已有登录的结果在 `.qa/codex-auth/codex-cli-results.json`。其余结果在 `.qa/browser-results.json` 和 `.qa/desktop-results.json`。这些测试文件被 Git 忽略。

## 兼容性与待验收项

- **Codex 桌面端**：已实现现代 provider 配置写入。桌面端与 CLI 使用同一配置目录时共享配置；没有完成桌面客户端重新启动后的实际推理验收。不能用 CLI 0.160.0 的结果推定所有桌面内置引擎均兼容。
- **Codex 认证**：provider 使用 `experimental_bearer_token`。`keyring` / `auto` 凭据存储模式、覆盖 model/provider 的活动 profile 会拒绝应用；旧版认证模板和自动客户端版本检测尚未实现。此次 CLI 请求验证使用 `file` 模式。
- **Claude Code 桌面端**：按照 cc-switch 桌面适配与 Anthropic Gateway 文档管理部署模式、第三方 profile 和配置库元数据。需要支持第三方推理的客户端版本及可识别的完整 Claude 模型 ID。尚未验证真实桌面端重启、模型选择与推理；组织管理策略可能覆盖本地设置。
- **Claude CLI**：用户级配置写入/恢复与认证字段选择已验证；尚未通过真实 Claude CLI 进程发送模拟或真实请求。项目设置、进程环境变量、启动参数和组织策略可能影响最终配置。
- **协议**：Codex 要求 Responses API，Claude 要求 Anthropic Messages API，不提供协议转换。
- **平台**：仅 Windows x64 构建与运行经过验证；跨平台路径代码不等于跨平台发行验收。

界面“已写入配置”表示配置文件校验通过，不表示供应商可连通、账户有额度或模型可用。软件应用配置不会触发自动测试请求。

## 复现入口

```powershell
corepack pnpm install
corepack pnpm build
corepack pnpm test
& .\scripts\with-msvc.ps1 -Action test
& .\scripts\with-msvc.ps1 -Action clippy
& .\scripts\with-msvc.ps1 -Action build
```

浏览器流程：先启动 `corepack pnpm dev:web`，再执行 `node scripts/qa-browser.mjs`。

原生流程：构建时显式启用 `qa-webview`，使用 `UNI_SWITCH_DATA_DIR` 与 `WEBVIEW2_USER_DATA_FOLDER` 指定新的隔离目录，启动测试程序，再执行 `node scripts/qa-desktop.mjs`。该脚本重写自己的隔离 fixture，每次完整验证应使用新的应用数据目录。发布构建不应启用该功能。

CLI 流程：完成原生 Codex 配置应用后，指定 `UNI_SWITCH_CODEX_BIN` 为待验证的客户端可执行文件，执行 `node scripts/qa-codex-cli.mjs`。可用 `UNI_SWITCH_QA_DATA_DIR` 选择原生操作备份目录，`UNI_SWITCH_QA_OUTPUT_DIR` 指定结果目录，`UNI_SWITCH_QA_AUTH_FILE` 指定合成登录 fixture。默认操作备份目录为 `.qa/native-data2`。

界面截图使用隔离测试配置，见 [Claude 桌面目标截图](screenshots/claude-desktop.png)。
