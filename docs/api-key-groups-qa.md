# API Key 分组验证记录

验证日期：2026-11-14 至 2026-11-15。候选版本：0.5.29。

当前候选源码包含此前工作区的 Go 源码整合和已记录的功能移除。
分组专项检查通过，但完整回归有失败，尚不符合正式发布条件。
不应把本记录解读为三平台 Release 已通过验收。

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
- Windows Rust 无桌面特性的测试命令编译成功；该目标当前没有运行时测试，不能代替桌面检查。
- Windows Tauri 生产安装包构建成功，Go 核心构建记录为 `qa:false`。
- Windows 桌面 Clippy `--all-targets -- -D warnings` 通过。

## 发布阻塞

完整命令 `node scripts/test-go.mjs -timeout=20m -p=2` 退出码为 1，
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
