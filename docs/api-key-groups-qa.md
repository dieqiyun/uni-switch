# API Key 分组验证记录

验证日期：2026-11-14 至 2026-11-15。候选版本：0.5.29。

当前候选源码包含此前工作区的 Go 源码整合和已记录的功能移除。
分组专项检查通过，但完整回归有失败，尚不符合正式发布条件。
不应把本记录解读为三平台 Release 已通过验收。

## Codex 登录与请求错误修复

- 新增回归先在旧代码上实际失败：无登录、API Key 登录和强制 API 登录仍写入
  `requires_openai_auth = true`；四协议解析器接受部分非对象输入或透出 Go 解析错误。
- 认证开关改为读取实际 Codex 配置目录中的已知登录态，覆盖自定义 CODEX_HOME、
  登录、登出、失效和无法读取的状态；自动修复采用同一个预期值。
- 四协议解析器与实际 HTTP 路由统一检查空请求、截断 JSON、尾随数据和类型错误。
  Gemini 保留数字错误码和状态，Anthropic 保留外层错误类型；流式入口在开始流之前
  返回 JSON 400。Gemini 下划线字段转换不再掩盖无效尾随数据。
- Chat 工具格式转换也增加同样的完整 JSON 检查：新增用例在修复前实际以
  `invalid envelope accepted: <nil>` 失败，修复后通过，正常工具格式转换继续通过。
- `scripts/qa-source-key-groups.mjs` 增加编译后核心的未登录配置检查，以及各协议的
  空请求错误和不发送上游请求检查，继续使用隔离目录和合成凭据。
- Windows 工作流原来的多命令步骤可能由最后一个成功命令掩盖前一个失败退出码，
  已把 Go 测试、vet、源码审计、文档检查和类型检查拆开。此前 Windows 的绿色结果
  不能视为全量 Go 测试已通过；Linux 和 macOS 原生构建均因回归失败而停止。
- 认证、桌面配置保留、失效登录、请求解析/HTTP 错误和 Key 分组的组合回归在
  `internal/agent`、`internal/gateway`、`internal/provider`、`internal/gui` 四个包通过。
- 更新后的编译核心浏览器验证全部通过：Chromium/WebKit × 五种语言，共 10 组。
  实际生成的未登录 Codex 配置写入 `requires_openai_auth = false`，实际推理仅使用
  选中分组 Key；Chat、Responses、Messages、count_tokens 与 Gemini 流式入口的
  空请求均返回结构化 JSON 400，且未发送上游请求。该轮检查配置与网关，CLI 检查见下。
- 另外通过 `scripts/qa-codex-key-groups.mjs --codex <CLI 程序路径>` 启动实际安装的
  Codex CLI，新进程使用全新的 HOME/CODEX_HOME、文件型凭据存储和本地拒绝外连代理。
  没有 `auth.json`，也不继承真实 API Key。实际 `codex exec` 成功结束并收到预期回复，
  本地模拟上游恰好收到一次请求，Key 为选中分组的合成凭据，模型为原始上游 ID。
  测试前后均未生成登录态；现有 Codex 桌面进程和配置不参与该检查。

## 分组功能

- `node scripts/test-go.mjs -run TestAPIKeyGroup -count=1 -timeout=5m -p=2` 通过。
  覆盖每个 Key 独立模型目录、空目录、命名、稳定标识、刷新、停用、未知 Key、
  普通模型标识兼容，以及限流时不跨组重试。
- `TestAPIKeyGroupsSaveCustomAndPreset` 通过自定义供应商和预设供应商的实际保存接口，
  验证首个分组名称、分组开关，以及随后普通保存对它们的保留。
  临时去掉预设字段复制后，该测试以 `saved provider lost named key group` 失败；恢复后通过。
- 临时去掉 `KeysFor` 的分组 Key 过滤后，网关回归实际记录到跨组凭据请求而失败；
  恢复过滤后，供应商、网关和保存接口的分组专项测试重新通过。
- `node scripts/qa-source-key-groups.mjs` 在 Chromium 和 WebKit 下，
  分别通过简体中文、繁体中文、英文、日文、德文，共 10 组检查。
  使用编译后的 Go 核心、合成 Key、本地模拟供应商和隔离客户端目录。
  检查实际生成的 Codex `config.toml` 和 `uni-switch-models.json`，
  确认选中模型使用分组标识、显示名称一致，且模型目录不包含 API Key。
  新增分组表单的 Axe 标签与 ARIA 检查通过。
- 相同的 10 组浏览器检查还从写入 Codex 配置的真实网关地址发起推理请求，
  确认本地模拟上游仅收到所选分组的 Key、原始模型 ID 和一条对应请求，
  客户端收到该分组的成功回复。没有真实付费请求。

## 构建与源码

- `pnpm typecheck`、`pnpm build`、`node --test scripts/tests/*.test.mjs` 通过，
  前端及源码边界测试共 21 项。
- `node scripts/check-upstream-source.mjs --index` 通过，核对原始源码、
  已记录修改、移除项及 Git 源码归档覆盖范围。
- `python scripts/audit-source.py` 未发现未豁免凭据或私有路径。
  上游合成测试资料按固定内容哈希审查；测试目录、缓存、生成的程序和用户客户端配置不进入源码。
- Windows `go vet` 和 `GOOS=linux CGO_ENABLED=0` 的交叉 `go vet -tags=nogui` 通过。
- `GOOS=darwin GOARCH=arm64 CGO_ENABLED=0` 的交叉 `go vet -tags=nogui` 通过。
  检查先发现一个旧 AppKit 托盘测试缺少与渲染器一致的 `darwin && cgo` 编译条件，
  导致禁用 CGO 时引用不存在的 `trayImagePNG`；补齐条件后通过，原生 CGO 测试保留。
- Windows Rust 无桌面特性的测试命令编译成功；该目标当前没有运行时测试，不能代替桌面检查。
- Windows Tauri 生产安装包构建成功，Go 核心构建记录为 `qa:false`。
- Windows 桌面 Clippy `--all-targets -- -D warnings` 通过。

## 发布阻塞

未解决的完整回归与发布条件跟踪于
[GitHub #13](https://github.com/dieqiyun/uni-switch/issues/13)。

继续检查回归后修复了以下测试整合问题：

- `internal/gateway` 的隔离初始化函数已存在但没有 `TestMain` 调用。
  补回入口并注册保留的轮换、搜索、限额服务；没有恢复已移除的扩展运行时。
  `TestRunsAsAnIsolatedTestBinary` 原先报告 `UNI_SWITCH_TEST_SANDBOX unset`，
  Codex HTTP 桩收到压缩数据，轮换和限额回归失败；修复后五项代表性测试通过。
- 旧图像目录测试还引用已移除的 `auto/` 路由分组并发生越界崩溃。
  改为验证保留的 API Key 分组图像能力在 OpenCode、Pi、Codex 目录中的传递，
  并在缺少条目时给出明确断言；分组专项测试通过。
- GLM 协议测试对已移除预设的空指针崩溃，改为显式自定义端点，保留 Responses、
  Chat、Messages 各走原生协议、对应路径和上游模型 ID 的断言；测试通过。
- 模块导入边界接受真正的核心程序入口，并只要求运行时源码模块注册，
  不再要求历史测试资料目录作为模块导入；`internal/pluginrt` 全包通过。
- 修复旧图像测试崩溃后，`internal/agent` 全包能够执行完成，仍有 84 项顶层失败，
  大多涉及已调整的上下文默认值和已移除路由分组等旧预期，尚未完成契约对齐。
- 恢复隔离入口并修复 GLM 协议测试后，`internal/gateway` 再次全包执行，在
  `TestZhipuCodingSearchAddress` 对已移除预设解引用时停止，之前记录到 108 项顶层失败。
  该包仍未通过；不能把修复前后不同停止位置的失败数量当作通过率。

在认证与请求错误修复提交 `764d7fc` 上重新执行完整命令
`node scripts/test-go.mjs -timeout=20m -p=2`，退出码仍为 1，
18 个包失败，输出 350 个顶层失败测试记录。
历史导入的完整浏览器测试套件尚未通过验收；当前浏览器通过范围仅为上面列出的专项检查。
已确认的例子包括：

- `internal/agent/TestAlmaToldReasoning` 等测试仍预期原来的模型上下文，当前整合源码默认使用 256000。
- `internal/cli/TestAgentFieldNameShowsIt` 等测试仍调用已经移除的 `group add`。
- `internal/gui/TestZenAddAnotherKeyEmptyAddsPublic` 等测试仍依赖已经移除的供应商预设。
- `internal/browsermcp/TestImageFromLeavesTheRest` 存在 Windows 路径预期失败。
- `internal/claudetui/TestShimFirstMessageWaitsForTools` 未在等待期限内收到测试消息。

本机生产桌面隔离启动检查提前退出，尚未通过。机器上有用户正在使用的同标识应用实例，
不能为测试停止或重启它；需要在干净的 CI 环境再次验证。

未使用真实付费 Key 或真实订阅进行请求，未对外公布候选版本。
必须先解决完整回归，再通过 `.github/workflows/build-desktop.yml` 的 Windows、Linux、
macOS 原生构建、隔离启动和附件校验，最后生成对应源码、使用指南和校验和并公开 Release。

## GitHub 推送保护

首次推送被 GitHub 拦截：导入源码仍携带两组第三方 Google OAuth 注册常量，
此前凭据审计的上游内容豁免未将它们拒绝。已删除运行时常量和相应豁免，
收紧审计规则，要求生产源码中的 Google OAuth 凭据始终报告为发现。
用户提供的 OAuth 配置和新账号续期保留行为见 [说明](google-oauth.md)。
相关 provider Google、Gemini、Antigravity 测试在本地模拟服务下通过。
