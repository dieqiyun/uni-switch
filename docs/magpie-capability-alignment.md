# Magpie 能力对齐与功能取舍

日期：2026-11-07。适用版本：v0.5.28。

参考版本固定为 [yetone/magpie · 665b9aff](https://github.com/yetone/magpie/tree/665b9aff126e26eb5fa8931b7395feb6002185f3)。结论针对实际审阅的源码及测试，不以 README 宣传或目录存在代替实现证据。参考文件的 Git blob SHA-1 与 SHA-256 见 [网关/配置参考记录](magpie-gateway-reference.json)；模型资料另见 [来源记录](magpie-modelmeta-reference.json)。哈希核对不表示所有分支都已精读或等价。

后续新增订阅登录／刷新、Gemini 原生双向与四协议网关、图片／视频和原生透传。实现与限制见 [统一网关说明](unified-gateway.md)，59 个固定参考文件见 [新增参考记录](magpie-platform-reference.json)。这些能力均由本项目 Go 实现；不把功能类别已实现当作 Magpie 全部分支等价。

本轮进一步核对 SSE、工具、推理、多轮压缩和模型映射的实际规则，修复迁移后仍存在的差异。具体入口、规则、自动源码对照与保留差异见 [五项核心行为对齐](magpie-core-alignment.md)，71 个核验文件见 [参考记录](magpie-core-behavior-reference.json)。这部分不能仅用“已经迁到 Go”代替行为验收。

## 用户所列 11 项

这 11 个主项在 Magpie 都有对应或等价实现，应保留主功能。主项下的独有机制需要单独取舍，不能把存储格式不同、尚未迁移或尚未测试当成“Magpie 没有”。

下表的上游路径均相对于固定提交；本地路径相对于本仓库。实现为本项目自己的 Go 代码，不把 Magpie 模块作为依赖。

| 主项                              | Magpie 实现及证据                                                                                                                                           | 我方当前处理与边界                                                                                                                                                                                           |
| --------------------------------- | ----------------------------------------------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 1. 双向推理协议转换               | **有**：`internal/gateway/{ir,anthropic,responses,chat}.go`，解析 → 统一表示 → 构造/输出；`translate_stream_test.go`                                        | 保留；`backend/internal/gateway/{parse,build,render}.go` 承接 Responses → Messages 与 Messages → Responses/Chat，Rust 旧转换器退出生产源树。不是其所有协议和原生扩展的完整复刻。                             |
| 2. 实际请求、鉴权、转换路由与响应 | **有**：`gateway.go`、`upstream.go`、`upstream_transport.go`                                                                                                | 保留；Go `http.go` / `recovery.go` 执行实际 HTTP、认证、接口选择、错误及有限回退；Go `localgateway` 负责 localhost listener、端口/令牌、Origin、路由和已应用快照选择；Rust 仅发送启动/健康 IPC。                                                         |
| 3. SSE、完成、断流和异常          | **有**：`sse.go`、`anthropic.go`、`responses.go`、`chat.go`、`prefill.go`、`stream_cut_test.go`                                                             | 保留；Go `sse.go` / `stream.go` / `recovery.go` 负责字节分帧、事件增量及结束状态。只有可靠 prefill、工具尚未开始等条件满足才最多续接两次；普通 Chat 不盲目拼接重答。                                         |
| 4. 工具调用与结果                 | **有**：`tool_use_ids.go`、`custom_tool.go`、`history.go`、`namespace_test.go`、`custom_tool_test.go`                                                       | 保留；Go 处理 ID、参数增量、多工具、namespace、自定义原始输入及工具结果配对。缺失结果补 interrupted/error，孤立结果保留用户数据，重复结果第一条生效。                                                        |
| 5. 思考与推理参数                 | **有**：`anthropic.go`、`responses.go`、`chat.go`、`thinking_signature.go`、`reasoning_carry.go`，`thinking_signature_test.go`                              | 保留；Go 处理 effort/budget/adaptive、签名与 opaque 归属。跨轮缓存按地址、Key、模型隔离；只有 Messages output_config.effort 不会自行开启 thinking。档位使用最终模型资料映射。                                |
| 6. 多轮、系统消息、续写与压缩     | **有**：`history.go`、`continuation.go`、`prefill.go`、`compacting.go`、`native_compaction.go`，`responses_system_turn_test.go`；`gateway/codex_backend.go` | 保留；Go `history.go` / `carry.go` / `compact.go`。中途 system/developer 原位置转 `<system-reminder>`；转换模型使用 compaction_trigger。独立 compact 接口及旧容器格式的改变见下表。                          |
| 7. 模型名与客户端兼容映射         | **有**：`provider/models.go`、`agent/claudedesktop_models.go`                                                                                               | 保留；Go `gateway/{models,catalog}.go` 处理真实 ID、品牌/非品牌兼容名称、旧 alias、上下文标记。不是把上游模型改名后发给供应商；供应商始终收到真实 ID。Go `agent/client.go` 同时生成 Codex 目录、Claude 档位与桌面模型列表并回读配置。                    |
| 8. 模型资料、目录和用户合并       | **有**：`catalog/{live,catalog,fresh,samemodel}.go`、`provider/{models,modelprefs,modelspec}.go`                                                            | 保留主功能；实时解析、models.dev 补全、用户覆盖、最终合并和档位映射均在 Go `modelmeta`。原自建签名资料权威链及四维能力认证没有同等机制，已退出运行路径。未知能力不自动支持。                                 |
| 9. 供应商与状态存储               | **有等价功能**：`provider/provider.go` 的载入、Save/Delete 等、`agent/applied.go` 的 applied.json、状态回查                                                 | 保留 CRUD、草稿/应用隔离、状态校验。Magpie 使用 JSON；我方为旧数据保留 SQLite schema，实际 SQL 与事务改到 Go `state`。供应商 CRUD 表单业务仍在 Rust；已应用快照、修订、确认、配置事务与恢复由 Go `configuration` 负责。                  |
| 10. 配置写入与恢复                | **有等价功能**：`edit/file.go` 的 WriteAtomic/Atomically、`agent/applied.go` 和各客户端接管/恢复逻辑，`edit/group_test.go`                                  | 保留；Go `edit` 负责原子文件、多文件 preflight、应用与回滚。Go `configuration` 负责路径/确认、journal、受管快照比较及恢复编排；Rust 只传入操作参数和返回 DTO。外部修改冲突仍保护；不因改语言而去掉这些功能。                                              |
| 11. 客户端适配                    | **有**：`agent/{codex,claude,claudedesktop,zcode,dsh,workbuddy}.go`                                                                                         | Codex 桌面/CLI、Claude 桌面/CLI、ZCode、DSH、WorkBuddy 均保留；Go `agent/{primary,extra}.go` 生成规则。Go `agent/{paths,client,discovery}.go` 负责配置路径、格式、投影和格式检查，`edit/toml.go` 按原文位置编辑并保留个人注释；Rust 仅保留原生进程线索与通用目录搜索。ZCode/DSH/WorkBuddy 不能列入“不支持而删除”。 |

主要源码入口：[网关](https://github.com/yetone/magpie/tree/665b9aff126e26eb5fa8931b7395feb6002185f3/internal/gateway)、[模型资料](https://github.com/yetone/magpie/tree/665b9aff126e26eb5fa8931b7395feb6002185f3/internal/catalog)、[客户端](https://github.com/yetone/magpie/tree/665b9aff126e26eb5fa8931b7395feb6002185f3/internal/agent)、[原子文件](https://github.com/yetone/magpie/blob/665b9aff126e26eb5fa8931b7395feb6002185f3/internal/edit/file.go)。

## 已删除或调整的细项

“未找到同等实现”限定于固定参考版本和本次审阅范围，不是对所有历史版本或插件的绝对否定。

| 原机制                                                          | 参考结论                                                                                                             | 当前处理                                                                                                                                                     |
| --------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 文本/图片/工具/流式四维、最多 256 输出 Token 的能力验证与观察库 | `provider/test.go` 的 tinyBody 是最多 16 输出 Token 的连接测试，没有同等四维能力认证流程                             | 删除四维入口、探测逻辑和观察文件写入；改为明确确认后单次 hi 可用性测试。成功不修改模型能力。旧磁盘观察文件不主动销毁。                                       |
| 每模型 midConversationSystem / inline-system 开关               | 固定参考使用历史归一化和原位置 system-reminder，不依赖此独有开关                                                     | 删除功能开关及导出字段；旧记录读入时只丢弃该字段，避免旧供应商/快照/恢复 journal 解析失败。按统一规则转换中途 system/developer，保留其顺序。                 |
| 自建官方资料、Ed25519 签名远程库作为最终权威                    | 固定参考的 API Key 资料链路采用实时资料、models.dev、用户偏好，没有我方同等签名权威链                                | 移除旧内置表、签名脚本和发布配置中的失效项；合并只由 Go modelmeta 决定。目录没有发布者签名，缓存 SHA 不冒充签名。保留历史维护文档并标注。                    |
| 自管转换模型独立 `/responses/compact` 及旧自建续接格式          | `gateway/codex_backend.go` 的自管模型拒绝独立 compact，转入 compaction_trigger；Magpie 并非完全没有 compact 相关处理 | 独立入口返回明确 400；保留 `/responses` 内 compaction_trigger、摘要及摘要后续聊。旧 Rust opaque 容器明确拒绝，涉及旧格式的会话需新建。                       |
| 旧 Rust 协议转换器及旧测试源                                    | 这是实现替换，不是 Magpie 缺少双向协议转换                                                                           | 删除生产源中的 convert/stream/reverse_http 等旧路径，Go 为唯一推理实现；失败不调用旧实现重发。旧测试数量减少不等于测试覆盖逐条保持，新增 Go 场景另行列证据。 |

没有删除供应商管理、配置恢复、多文件回滚、applied snapshot 或三个附加客户端：它们有对应功能。没有因为对方用 JSON 就删除用户 SQLite 数据。

## 仍是我们的实现

- 全部 Go 源码、管道协议和桌面集成属于本项目；Magpie 是固定版本的逻辑参考，非运行/构建依赖。参考和派生材料保留原 MIT 声明，本项目原始代码继续 AGPL-3.0-only。
- Rust/Tauri 的窗口、托盘、系统集成、更新、原生进程线索及通用目录搜索。
- Rust 的供应商表单校验、CRUD 和部分命令编排；保留 SQLite 数据格式以兼容旧安装。
- Go 为本项目自行实现的业务核心，包含网关、模型投影、客户端路径和格式处理、确认、journal、修订、回滚恢复，以及 Fast/思考菜单修复/连接同步。扩展目录覆盖固定新版 Magpie 的 74 个第三方配置入口，见 [完整清单](magpie-client-support.md)。
- React/TypeScript 的界面、表单草稿、显示和操作反馈。模型资料最终合并不再由前端另算。

用户此次指定的四项已进入 Go。此结论不代表全部 Rust 代码已移除，也不声称逐分支完整复刻 Magpie。`toml_edit` 和 `serde_yaml_ng` 仅为 Rust 测试依赖，生产配置解析和编辑由 Go 负责。

## 验收证据

所有 QA 使用隔离 `.qa`/临时数据、合成密钥和本地模拟供应商；没有操作真实客户端、停止真实进程或发送收费推理。

五项核心行为对齐的最新补充：Go 362 项与 vet、46 个固定源码核心向量、扩展的 Rust → Go HTTP 合同及 Windows 新内嵌核心 QA 均通过。核心合同与产品差异见 [专项说明](magpie-core-alignment.md)；当前 Windows 证据更新为 `.qa/go-backend/1794064791909/results.json`。下表较早的前端、Clippy 等记录是对应阶段的完整回归，本轮未改其代码、不重复声称已重跑。

| 检查             | 当前结果与范围                                                                                                                                                                                                                                                                                                                                                                 |
| ---------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| Go               | `go test ./...`、`go vet ./...` 通过，覆盖 localgateway、configuration、gateway、agent、edit、state、supplier、modelmeta、accounts、claudesub、platform、RPC。新增 Gemini 四协议 32 组 JSON/SSE HTTP、Code Assist 包装、Schema 属性保护、签名隔离、OAuth/并发刷新、Claude 工具续传和媒体路由回归。既有配置事务、恢复及 173 个模型资料纯函数对照向量仍运行；不把这些作为整个 Magpie 的等价证明。 |
| Rust             | 最近完整回归 164 项通过、0 失败、1 项环境辅助 ignored；桌面 Clippy 通过。删除的旧转换器测试不再计入运行数量。                                                                                                                                                                                                                                                                  |
| 前端             | 最近完整回归 194 项、29 文件通过；TypeScript、Vite 和教程生成一致性通过。                                                                                                                                                                                                                                                                                                      |
| Rust → Go → HTTP | `scripts/test-go-bridge.py` 通过：带模型资料的双向请求、7 字节 SSE 分片、中文工具参数、签名回放、opaque 多轮、Chat、压缩/续聊、取消释放上游、退出清理。                                                                                                                                                                                                                        |
| Windows 原生桌面 | 当前 QA 构建通过；扩展的 `scripts/qa-go-backend.mjs` 通过：保存/应用/实际 localhost 转发/恢复闭环、Go listener 专属健康标记、两方向带模型资料、SSE、token/Origin 拒绝、未应用 Key 隔离、恢复只停用本目标、客户端文件逐字恢复、两类内嵌核心正常退出清理；11 个附加客户端 IPC、Pi/Gemini 实际转发、Cursor 专用模型目录及选择器无障碍；未确认不发连接测试、一次最多 16 输出 Token、不生成旧观察文件及旧 inline-system 只读入丢弃。新增统一 Gemini 原生 IPC、来源管理和供应商登记、Key 不回显、图片/视频/原生路由及界面 Axe 零违规。最新证据：`.qa/go-backend/1794055571892/results.json`。 |

包含纯 Go SQLite 新依赖的 Windows x64、Linux x64/arm64、macOS Intel/Apple Silicon 核心均编译成功；Linux/macOS 未做本机运行验证。这里的通过不代表所有 Magpie 分支、所有原生客户端及真实付费供应商都验收完成。当前源码扫描记录见 `.qa/unified-platform-working-tree-audit.json`，范围包含未跟踪且未忽略的文件，不输出匹配的敏感值。

## 明确限制

- Linux/macOS 原生桌面、安装包和真实客户端端到端流程需要多平台 CI/独立验收；交叉编译不能代替这些验证。
- 本次没有重跑旧 Rust 阶段的全部真实 Codex/Claude CLI 流程，历史脚本记录已在对应文档中标注。
- Gemini 原生入站、上游和 Code Assist 包装已加入，支持四协议 JSON/SSE、工具、图片／文件、结构化输出和签名续传。订阅 OAuth／设备登录、刷新与 Claude 隔离 CLI 已实现；没有进行真实订阅授权或付费调用。媒体厂商及仍未复现的 Magpie 分支见 [统一网关范围](unified-gateway.md#媒体适配范围)。
- 模型、账号及历史变动可能使签名失效，旧 Rust 容器会话需新建。跨协议转换不是所有厂商扩展的无损通道。
- 供应商表单/CRUD、系统集成和通用目录搜索仍在 Rust；此次四项业务的 Go 迁移不等于整个桌面应用改写。保护与兼容逻辑保留。

实现细节见 [Go 后端](go-backend.md)、[模型资料](model-metadata.md)。本次不提交、不推送、不发布。
