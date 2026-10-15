# 影响使用的缺陷排查与修复（2026-10-16）

本轮基于 v0.5.25 源码提交 36eb2314d85623cb690aacdae01b8c1a1acf154f，工作分支为 codex/critical-usage-audit。以下为已确认并修复的问题，修复版本为 v0.5.26；对应源码与程序以该版本发布标签为准。v0.5.25 不包含本轮修改。

## 已确认的问题

| 优先级 | 触发条件与实际后果 | 修复 |
| --- | --- | --- |
| 高 | Claude 通过转换调用 OpenAI 供应商，接入地址含 /api/paas/v4、/backend-api/codex 等显式前缀。旧逻辑再加 /v1，推理和能力验证请求到错误路径，可能整条连接都无法使用。 | api_endpoint.rs 统一路径拼接，显式 OpenAI 前缀保留；只有裸站点默认加 /v1。完整资源地址可切换对应端点，Messages 保留既有版本路径规则。调用与验证共用实现。 |
| 高 | WorkBuddy 切换供应商或同步缩减模型时，旧模型行和 availableModels 未撤回，模型列表继续暴露旧供应商路由与旧密钥。选择残留项仍会使用旧连接，造成错用供应商或调用失败。 | 重新应用前只撤回能证明由上次应用写入、且未被修改的条目；恢复被同名受管模型遮住的个人条目，并同步可见列表。首次原文件仍完整保留用于恢复。 |
| 高 | WorkBuddy 新旧供应商使用同一模型 ID，新供应商没有声明上下文、图片或工具能力时，旧条目合并会留下上一家的能力。客户端可能发送当前供应商不支持的请求。 | 先撤回上一家受管条目，再合并新模型，避免继承旧供应商的 maxInputTokens、maxOutputTokens、supportsImages、supportsToolCall。 |
| 高 | 多文件配置写入失败或启动恢复时，倒序回滚先遇到一个被外部修改的文件，立即退出，其他本来可以恢复的文件也被留在半更新状态。 | writer::rollback 继续尝试同一操作内的所有文件；外部冲突文件保留，错误继续向上传递。任一恢复失败时，pending 日志和操作备份仍保留，不伪报完成。原有客户端和新增客户端都使用该逻辑。 |
| 高 | SSE 使用单 CR 换行、分块累计大量 data 行，或 Chat 已给出 finish_reason 后尾部事件被截断。旧逻辑可能无输出直到超时、累计事件无上限，或把不完整流误报成功；单个大网络块包含大量正常小事件时也会被误拒。 | 支持 LF / CRLF / CR、混合换行与首行 UTF-8 BOM；限制跨块累计的未完成行和事件数据；按事件而非整个网络块限额；EOF 检查未完成事件，截断时返回流错误。 |

WorkBuddy 的个人配置保护单独补充了归属记录：恢复基线、最后一次程序生成的模型变更和手动编辑后的文件快照分别保存。编辑器新增的个人模型、外部新增模型以及被外部修改的旧条目，连续切换和重启后仍保留。旧版已手动编辑且缺少归属证据的快照采取保留策略，不推测删除。

## 复现与回归证据

实现前新增的六项最小复现均失败，实现后通过；随后增加 API 端点兼容、真实 localhost HTTP 验证、BOM / 大批量事件 / 截断尾部、同名模型能力以及个人模型连续切换等边界覆盖。个人模型在后续切换时被误清理的问题也先复现失败，再补齐独立归属记录。

本轮新增 11 项 Rust 回归测试（名称带 usage_audit），并扩展现有双向转换 HTTP 测试中的自定义路径和截断场景。实际 HTTP 用例检查 Claude CLI 与 Desktop 两个目标、Responses 与 Chat 两种上游、JSON / SSE、system 历史和 token 计数；截断场景要求返回 error，且不得产生 message_stop。

完整检查结果：

- Rust：204 项通过，0 失败，另 1 项原有 Windows 环境辅助测试 ignored。
- 前端：26 个测试文件、182 项测试通过。
- Windows 常规桌面 Clippy：所有目标通过，使用 -D warnings。
- TypeScript 检查与 Vite 生产构建通过；仅有现有依赖注释和包体大小提示。
- 软件内教程与 GitHub 教程一致性检查通过。

复跑命令：

~~~powershell
cargo test --locked --manifest-path src-tauri/Cargo.toml --no-default-features
cargo test --locked --manifest-path src-tauri/Cargo.toml --no-default-features usage_audit -- --nocapture
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/with-msvc.ps1 -Action clippy
corepack pnpm test
corepack pnpm build
corepack pnpm docs:check
~~~

## 与 CC Switch 的对照

2026-10-16 通过上游 /releases/latest 核实，正式版为 2026-10-14 发布的 v4.0.12，标签源码提交为 51ac86f1c59abfefabadebd05ec5a5415b8ad72c，未把较旧搜索缓存中的 v4.0.2 / v4.0.7 当作最新版。

本轮只读对照其 streaming_responses.rs 与 sse.rs 的终止状态和分帧边界。v4.0.12 修复的「Responses 已完成后又转发连接错误」也纳入核对：uni-switch 已在收到有效终止事件后停止读取，不新增此类尾随错误；本次新增的 EOF 检查作用于尚未终止的流。显式 API 前缀按照 uni-switch 现有原生客户端导出的 SDK 语义统一，未直接复制上游代码或强行照搬不同的 Full URL 开关语义。

参考：

- [CC Switch v4.0.12 发布说明](https://github.com/farion1231/cc-switch/releases/tag/v4.0.12)
- [上游 Responses 流式处理](https://github.com/farion1231/cc-switch/blob/51ac86f1c59abfefabadebd05ec5a5415b8ad72c/src-tauri/src/proxy/providers/streaming_responses.rs)
- [上游 SSE 分帧](https://github.com/farion1231/cc-switch/blob/51ac86f1c59abfefabadebd05ec5a5415b8ad72c/src-tauri/src/proxy/sse.rs)

## 范围与限制

本轮重点是请求路由、供应商切换、流式转换和配置事务；完整回归同时覆盖已有模型 ID / 档位映射、认证、模型发现、余额、更新下载校验、配置目录发现和重启识别等行为。这是对已覆盖路径的验证，不能据此保证所有供应商和所有客户端版本均无缺陷。

所有新增测试仅使用隔离临时数据/配置目录、合成凭据和本地 HTTP 服务。没有修改、重启或停止用户真实 Codex、Claude、uni-switch；没有发送真实供应商的计费推理请求。本轮本机未重新运行原生 UI 全套，也未在 Linux / macOS 实机或真实 WorkBuddy agent 中验证；跨平台原生 CI 与正式附件检查另以版本发布记录为准，源码测试覆盖不等同于所有客户端的完整端到端验收。

旧配置须由用户在包含修复的版本中重新应用后才会更新，源码修改不会自动修复已安装版本或改写真实客户端配置。发生未解决的恢复冲突时仍保留日志并阻止后续写入，需要先处理冲突文件；修复扩大了可恢复文件的范围，没有解除这项保护。
