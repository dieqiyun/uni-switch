# Magpie 第三方客户端配置支持清单

日期：2026-11-07。适用版本：v0.5.28。

参考 [yetone/magpie@f13d5f16](https://github.com/yetone/magpie/tree/f13d5f16f474f9c3baa5081c6cae3934af291cfd) 的 `onMachine` 完整注册表，共 76 个基本入口。其间 74 个包含第三方连接配置，对应本项目 3 个主入口和「全部客户端」中的 71 个附加入口；配置档案和账户作为入口内的选择，不重复计数。文件哈希与注册表逐项映射见 [固定参考记录](magpie-clients-reference.json)。

第三方配置、路径、格式、写入、回读、恢复和本地转发由本项目 Go 核心实现。Magpie 不作为运行或构建依赖。这里的覆盖指客户端配置接入，不能解释为其所有原生功能、版本、操作系统都已实机验收。

## 逐项对照

| Magpie 客户端 / 源码 | 本项目入口 | 接入方式 | 客户端请求协议 |
| --- | --- | --- | --- |
| [Claude Code CLI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/claude.go#L618) | `claude_cli` | 原有主入口，由 Go 承接 | Responses / Messages；按上游转换 |
| [Claude Code 桌面端](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/claudedesktop.go#L117) | `claude_desktop` | 原有主入口，由 Go 承接 | Responses / Messages；按上游转换 |
| [Claude Science](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/claudescience.go#L370) | `claudescience` | 启动脚本 | Messages |
| [Codex 桌面端 / CLI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/codex.go#L93) | `codex` | 原有主入口，由 Go 承接 | Responses / Messages；按上游转换 |
| [Gemini CLI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/gemini.go#L23) | `gemini` | 配置文件 | Gemini GenerateContent |
| [Antigravity CLI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agy.go#L205) | `agy` | 启动脚本 | Gemini GenerateContent |
| [Antigravity Desktop](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/antigravity.go#L85) | `antigravity` | 配置文件 | Chat Completions |
| [OpenCode](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1262) | `opencode` | 配置文件 | Chat Completions |
| [OpenChamber](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/openchamber.go#L162) | `openchamber` | 配置文件 | Chat Completions |
| [MiMo Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1302) | `mimocode` | 配置文件 | Chat Completions |
| [DevEco Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1322) | `deveco` | 配置文件 | Chat Completions |
| [Pi](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1337) | `pi` | 配置文件 | Chat Completions / Messages / Responses |
| [StepCode](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/stepcode.go#L27) | `stepcode` | 配置文件 | Chat Completions / Messages / Responses |
| [MothX](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/mothx.go#L47) | `mothx` | 配置文件 | Chat Completions |
| [PenguinHarness](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/penguin.go#L118) | `penguin` | 配置文件 | Responses |
| [Aside](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/aside.go#L12) | `aside` | 配置文件 | Chat Completions |
| [OmO](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/omo.go#L20) | `omo` | 配置文件 | Chat Completions / Messages / Responses |
| [Prime Agent](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/primeagent.go#L25) | `primeagent` | 配置文件 | Chat Completions / Messages / Responses |
| [Goose](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1607) | `goose` | 配置文件 | Chat Completions |
| [Cursor CLI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1702) | 不属于第三方 endpoint 配置 | 原生账号模型设置 | — |
| [Cursor Private Inference](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/cursorlocal.go#L282) | `cursorlocal` | 启动脚本 | 目录自动选择 Messages / Responses / Chat Completions |
| [Zed](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/zed.go#L25) | `zed` | 配置文件 | Chat Completions |
| [VS Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/vscode.go#L128) | `vscode` | 配置文件 | Chat Completions |
| [VS Code Insiders](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/vscode.go#L130) | `vscodeinsiders` | 配置文件 | Chat Completions |
| [VSCodium](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/vscode.go#L132) | `vscodium` | 配置文件 | Chat Completions |
| [Zoo Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/zoocode.go#L60) | `zoocode` | 配置文件 | Chat Completions |
| [Copilot JetBrains](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/copilotjetbrains.go#L69) | `copilotjetbrains` | 配置文件 | Chat Completions |
| [JetBrains Air](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/air.go#L139) | `air` | 配置文件 | Chat Completions |
| [JetBrains IDE ACP](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/air.go#L170) | `airide` | 配置文件 | Chat Completions |
| [Copilot CLI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1740) | `copilot` | 配置文件 | Chat Completions |
| [Crush](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/agents.go#L1895) | `crush` | 配置文件 | Chat Completions |
| [DSH](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/dsh.go#L90) | `dsh` | 配置文件 | Messages / Chat Completions / Responses |
| [DS Bot](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/dsbot.go#L95) | `dsbot` | 配置文件 | Messages / Chat Completions / Responses |
| [Reasonix Studio](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/reasonix.go#L80) | `reasonix` | 配置文件 | Chat Completions |
| [Command Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/commandcode.go#L24) | `commandcode` | 配置文件 | Chat Completions |
| [fx](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/fx.go#L28) | `fx` | 配置文件 | Chat Completions |
| [Oh My Pi](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/omp.go#L142) | `omp` | 配置文件 | Chat Completions / Messages / Responses |
| [Devin](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/devin.go#L24) | 不属于第三方 endpoint 配置 | 原生账号模型设置 | — |
| [Hermes](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/hermes.go#L50) | `hermes` | 配置文件 | Chat Completions / Messages |
| [Mister Morph](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/morph.go#L34) | `morph` | 配置文件 | Responses |
| [Kimi Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/kimi.go#L234) | `kimi` | 配置文件 | Chat Completions |
| [Qwen Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/qwen.go#L150) | `qwen` | 配置文件 | Chat Completions |
| [Muse Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/muse.go#L33) | `muse` | 配置文件 | Responses |
| [Empryo](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/empryo.go#L33) | `empryo` | 配置文件 | Chat Completions |
| [Ante](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/ante.go#L87) | `ante` | 配置文件 | Chat Completions |
| [AstrBot](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/astrbot.go#L111) | `astrbot` | 配置文件 | Chat Completions |
| [QwenPaw](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/qwenpaw.go#L211) | `qwenpaw` | 配置文件 | Chat Completions |
| [OpenClaw](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/openclaw.go#L217) | `openclaw` | 配置文件 | Chat Completions / Messages / Responses |
| [jcode](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/jcode.go#L179) | `jcode` | 配置文件 | Chat Completions |
| [MiniMax Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/minimax.go#L83) | `minimax` | 配置文件 | Messages |
| [Droid](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/droid.go#L378) | `droid` | 配置文件 | Chat Completions / Messages / Responses |
| [Cline](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/cline.go#L55) | `cline` | 配置文件 | Chat Completions |
| [Qoder](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/qoder.go#L41) | `qoder` | 配置文件 | Chat Completions |
| [Qoder CN](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/qoder.go#L45) | `qodercn` | 配置文件 | Chat Completions |
| [Grok Build](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/grok.go#L163) | `grok` | 配置文件 | Chat Completions |
| [Grok App](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/grokapp.go#L170) | `grokapp` | 配置文件 | Chat Completions |
| [ZCode](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/zcode.go#L66) | `zcode` | 配置文件 | Messages / Chat Completions / Responses |
| [WorkBuddy](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/workbuddy.go#L48) | `workbuddy` | 配置文件 | Chat Completions |
| [WorkBuddy AI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/workbuddy.go#L59) | `workbuddyai` | 配置文件 | Chat Completions |
| [CodeBuddy](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/codebuddy.go#L39) | `codebuddy` | 配置文件 | Chat Completions |
| [Pencil](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/pencil.go#L34) | `pencil` | 配置文件 | Chat Completions / Messages / Responses |
| [OpenPencil](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/openpencil.go#L81) | `openpencil` | 配置文件 | Chat Completions / Messages |
| [OpenBot](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/openbot.go#L81) | `openbot` | 配置文件 | Chat Completions |
| [NewMax](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/newmax.go#L85) | `newmax` | SQLite 事务 | Messages |
| [LiveAgent](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/liveagent.go#L94) | `liveagent` | 配置文件 | Chat Completions / Messages / Responses |
| [Raycast](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/raycast.go#L52) | `raycast` | 配置文件 | Chat Completions |
| [Kiro Crew](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/kirocrew.go#L359) | `kirocrew` | 配置文件 | Chat Completions |
| [T3 Code](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/t3code.go#L81) | `t3code` | 配置文件 | Messages |
| [OpenMausBot](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/openmausbot.go#L130) | `openmausbot` | 配置文件 | Messages |
| [OpenHanako](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/hanako.go#L75) | `hanako` | 本地 API | Chat Completions |
| [AtomCode](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/atomcode.go#L52) | `atomcode` | 配置文件 | Chat Completions |
| [Snow CLI](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/snow.go#L56) | `snow` | 配置文件 | Chat Completions |
| [Alma](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/alma.go#L739) | `alma` | 本地 API | Chat Completions |
| [Cindy](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/cindy.go#L128) | `cindy` | 导入链接 | Messages / Chat Completions / Responses |
| [AionUi](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/aionui.go#L106) | `aionui` | 导入链接 | Chat Completions |
| [Lody](https://github.com/yetone/magpie/blob/f13d5f16f474f9c3baa5081c6cae3934af291cfd/internal/agent/lody.go#L106) | `lody` | 导入链接 | Messages / Chat Completions / Responses |

## 使用与边界

- 在侧栏「全部客户端」搜索名称，再选择已保存供应商进行配置。路径采用客户端目录和环境变量规则；配置页可手动指定目录。位置记录损坏只影响该客户端，可通过手动目录修复。
- Oh My Pi、Hermes、QwenPaw 和 Aside 支持发现配置档案 / 账户；接管后需先恢复再切换。VS Code 系列同时更新实际配置档案的设置和模型列表，遵循其继承默认设置标记。Hermes 只写所选档案，不会把供应商散布到其他档案。
- 普通客户端请退出后配置并重新打开。Alma 保持运行，使用本地 API；NewMax 先打开一次再退出，通过数据库行事务更新，不替换数据库。OpenHanako 需要已有 agent，加密资料通过运行中的 API 写入，不覆盖密文；恢复先切回可用的原生模型，再移除本应用供应商。
- Cindy、AionUi、Lody 生成导入链接，需要在客户端确认导入。状态保持“等待导入”，停用本地连接后还需在客户端删除已导入条目。
- Antigravity CLI、Claude Science、Cursor Private Inference 和 OpenChamber 通过生成的专用启动脚本使用新环境。Windows 运行 PowerShell 脚本，macOS / Linux 运行 shell 脚本。Claude Science 脚本执行时会停止旧 daemon 并以新配置启动；本应用及测试不自动重启这些真实进程。Cursor 不修改系统用户环境。
- Cursor Private Inference 通过 product.json 识别专用版本（Linux 同时识别专用 AppImage），不会默认启动普通 Cursor。Claude Science 检查常见安装位置及 PATH。便携安装可在运行脚本前设置 `UNI_SWITCH_CURSOR_LOCAL_BINARY` 或 `UNI_SWITCH_CLAUDE_SCIENCE_BINARY` 为程序绝对路径；找不到程序时脚本明确报错。JetBrains Air 优先使用 PATH 中的 OpenCode，再查安装器及 IDE / Zed 的 ACP 缓存。
- Alma 会把 API Key 作为不可回读的写入字段，回读冲突检查不包含密钥本身；其他可读字段仍校验。首次接管发现同名 `uni-switch` 供应商时拒绝覆盖，避免无法恢复其加密密钥。
- Zed 同时配置模型与系统凭据库；Linux 需要可用、已解锁的 Secret Service 和 secret-tool，macOS 需要可定位的 Zed.app。自动测试使用替身凭据库，没有读写真实系统凭据。
- Kiro Crew 复用已配置同一供应商的 Codex、Claude Code、OpenCode、Pi 或 Goose。T3 Code 分别生成 Claude Code 与 Codex 实例，Codex 使用专属模型目录和启动参数。
- fx、Muse Code、OpenBot、Empryo、Command Code、Ante、Lody、Zed 使用各客户端专用本地端口，只访问该客户端已应用快照；阻止 Origin 和跨客户端路由。切换供应商草稿不会改变已应用连接，恢复配置后相应路由停用。
- ZCode、DSH、WorkBuddy 保留原生直连规则；其他附加入口默认走 Go 本地网关，可把客户端协议转换为供应商的 Messages、Responses 或 Chat Completions。Gemini CLI / Antigravity CLI 使用 Gemini 入站协议，供应商仍沿用本项目已有上游类型。
- Gemini 支持文本、图片、函数工具、结构化输出、思考参数及流式响应。未支持的托管工具、文件 / PDF 等附件明确拒绝，不能当作无损覆盖所有 Gemini 扩展。
- 本轮没有增加 WSL 跨主机连接、Docker 内网地址投影、订阅 / OAuth 登录、图片生成端点或客户端自动安装。不同系统的默认路径在 Go 中实现，Linux / macOS 原生客户端仍需独立验收。

## 验证

- Go：全目录文件型客户端应用、重复应用和恢复；JSONC 注释、YAML、真实 TOML 数组表；OpenCode 2 格式；Oh My Pi 旧 JSON 模型迁移；配置档案路径。
- 启动器：三个系统的合成安装目录识别、普通 Cursor 排除、ACP 缓存选择；Windows PowerShell 实际执行合成脚本，验证中文路径、字面量参数和 stop / serve 顺序。T3 Code 启动参数经过独立分词后再次由 TOML 解析，包含引号、反斜杠和控制字符。
- 特殊资源：NewMax 真实临时 SQLite、Alma 本地模拟 API（服务端分配 ID、部分失败补偿）、OpenHanako 加密 catalog 与本地 API（原模型缺失回退、恢复顺序）、Zed 替身凭据库及写入失败回滚。
- 网关：Gemini 工具多轮与同名调用、图片和 schema、签名 / opaque 续接、UTF-8 分片和完成 / 断流；专用入口 token、Origin、跨客户端拒绝、已应用快照和独立停用。
- Cursor Private Inference 模型目录提供 `api_types` 和 `capabilities`，包含上下文、输出限制、视觉和推理档位；Claude 优先使用 Messages。其他客户端的模型目录保持自身格式。
- 前端：190 项、28 文件通过；TypeScript、Vite 构建和教程一致性检查通过。可搜索客户端选择器含键盘操作回归。
- Windows 桌面：隔离 IPC 验证 OpenCode、Pi、Gemini、T3 Code、Muse Code、fx、Claude Science、Cursor Private Inference、OpenChamber、Cindy、Lody 的应用、状态和恢复，包含 Pi / Gemini 到 Messages 上游的实际 HTTP 转发、Cursor 模型目录、导入 / 启动状态和选择器无障碍检查。所有测试使用隔离 `.qa` / 临时目录、合成凭据和本地模拟上游，没有安装或操作用户真实客户端。
- 最新桌面证据：`.qa/go-backend/1794051003049/results.json`，客户端选择器截图在同目录 `client-chooser.png`。这 11 个入口验证本应用的桌面 IPC 和模拟上游链路，没有实际启动这 11 个第三方程序。
- 全量 `go test ./...` / `go vet ./...`、Rust 164 项（另 1 项环境辅助 ignored）和桌面 Clippy 通过。Rust → Go → HTTP 合同通过。Go 核心的 Windows x64、Linux x64 / arm64、macOS x64 / arm64 均编译通过；Linux / macOS 的真实桌面、凭据库和第三方程序仍需本机验收。

覆盖检查：`python scripts/check-client-reference.py`；生成绑定检查：`python scripts/sync-client-catalog.py --check`。
