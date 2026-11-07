# 五项网关核心行为对齐

日期：2026-11-07。适用版本：v0.5.28。

本轮不是再次改语言。此前这五项已迁到 Go、旧 Rust 转换器已退出；本轮重新核对具体行为，先用回归测试复现差异，再修改 Go 实现。固定参考为 `yetone/magpie@665b9aff126e26eb5fa8931b7395feb6002185f3`。源码身份与对照向量见 [参考记录](magpie-core-behavior-reference.json)，实现不依赖 Magpie Go 模块或可执行程序。

## 处理规则与代码入口

| 用户指定项 | 按参考落实的规则 | 本地实现 |
| --- | --- | --- |
| SSE、增量、结束与异常 | 字节分片后按事件解析，多行 data 合并；识别裸 JSON 错误，不能当作断流去续接；原生转发也检查协议终止，EOF 不能自动当完成；保留未修改的事件原文、注释及扩展字段；Gemini 保留结束后的用量尾帧 | `backend/internal/gateway/sse.go`、`relay.go`、`recovery.go`、`stream.go` |
| 工具调用、结果与多工具 | Chat 历史缺失 ID 按位置、名称、原参数稳定生成，无 ID 结果按缺失调用 FIFO 配对；流式缺失 ID 首次补齐，后续空 ID 不覆盖；同索引新 ID 是新调用；重复名称不拼接；翻译流及整包解码过滤无名称的 Chat 调用，原生整包也过滤；保留参数字符串空白；自定义原始文本增量转义为统一参数；Messages 重复 ID 可逆加盐、回传恢复；namespace 和结果配对继续使用统一工具表示 | `tool_ids.go`、`parse.go`、`sse.go`、`render.go`、`relay.go`、`custom.go`、`types.go` |
| effort、budget、adaptive、签名与 opaque | 不向上游发送未知 effort；按模型档位取最近值、同距向上，ultra 转 max；预算受客户端输出上限约束，低上限不开预算；仅历史 configuration_update 不会自行开启思考；Responses reasoning 正文优先于摘要；签名与 opaque 只回到所属协议，无法保存 opaque 的客户端通过工具 ID 携带本地记录键恢复 | `types.go`、`parse.go`、`build.go`、`render.go`、`carry.go` |
| 历史、多轮、续写与压缩 | Responses 原生构造保留客户端 system/developer 的角色和位置；跨协议时前置指令归并，中途指令转原位置 reminder；仅明确 prefill 拒绝做有限重试，末尾工具调用不算 assistant prefill；Gemini 补 user 结尾，摘要请求补明确摘要要求；压缩清除普通、托管及搜索工具，拒绝工具调用或不完整答案生成 checkpoint；支持 checkpoint 后续聊 | `types.go`、`history.go`、`build.go`、`gemini_upstream.go`、`recovery.go`、`compact.go` |
| 真实模型、Claude 档位、别名与上下文 | FNV 稳定数字别名；能力资料变化前后的合法别名均可精确反查；哈希错误的别名不能靠包含 opus/sonnet 兜底；档位只接受 Claude 正式形态或纯档位选择；Desktop 展示版本用横线拼写；去除客户端 `[1m]` 标记再向供应商发送真实 ID；配置生成复用同一档位规则 | `gateway/models.go`、`catalog.go`、`agent/primary.go` |

参考重点文件：`gateway/{sse,chat_call_ids,chat,tool_use_ids,custom_tool,ir,anthropic,responses,reasoning_carry,prefill,compacting,desktop,desktop_list}.go` 及对应测试。71 个文件的哈希是身份清单，不代表逐分支全部等价。

## 验证方式

- `magpie_alignment_test.go`：针对本轮差异的协议、历史、预算、别名及本地 HTTP 回归；包括工具参数在上游放行结束前已抵达客户端的门控测试，以及原生转发字段保留和重复 ID 两轮恢复。
- `core_oracle_test.go`：46 个期望值来自固定源码纯函数的实际执行，覆盖 effort、预算、预算反查、历史 ID、FNV 数字。生成器 `scripts/check-core-reference.py` 先核对 Git blob SHA-1 与 SHA-256，参考程序仅放在忽略的 `.qa/core-oracle` 下。
- 现有模型资料 173 个纯函数对照向量继续运行；网关既有签名、opaque、namespace、断流续接、压缩回放及四协议 HTTP 矩阵继续运行。
- `scripts/test-go-bridge.py`：Rust IPC → Go → 本地替身上游，新增自由格式输入转义、原生重复工具 ID 还原及截断流导致 Rust 响应体失败的检查。
- `scripts/qa-go-backend.mjs`：使用新内嵌核心验证 Windows 隔离桌面、实际 localhost 请求、配置恢复及退出清理。

最终运行结果记录在 [QA 清单](qa-inventory.md)。所有自动验证只用隔离目录、合成凭据和 localhost 替身，不修改或重启用户真实客户端。

本轮结果：Go 362 项及 vet 通过；20 个规则／源码对照测试包含 46 个核心向量。Rust 164 项通过、1 项环境辅助 ignored。扩展的 Rust IPC 合同与 Windows 新内嵌核心桌面隔离验证通过；Windows、Linux x64/arm64、macOS x64/arm64 核心编译通过。证据分别为 `.qa/core-alignment/1794064737770/results.json`、`.qa/go-backend/1794064791909/results.json`，源码审计为 `.qa/unified-platform-working-tree-audit.json`。跨平台编译不替代其他系统的实机验收。

## 明确保留的项目差异

- 别名品牌和本地续接封装仍使用 uni-switch；格式承担相同职责，不与 Magpie 共用数据目录。
- SSE 单事件限制保持 4 MiB，整体内容限制保持 16 MiB；参考扫描上限为 32 MiB，未为追求相同数字扩大本地输入限制。
- effort 合法值额外去除首尾空白，避免参考函数对部分合法名称保留空白的边缘行为；参考向量不把该源码瑕疵作为目标。
- opaque 记录继续按地址、Key、模型隔离并定期清理，避免不同账号之间回放。
- 不声明复刻 Magpie 的整个桌面会话路由、插件、供应商学习策略或所有版本 UI 的专门分支；本轮契约以表中已核对规则和实际测试为准。
