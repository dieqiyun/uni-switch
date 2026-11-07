# Go 业务后端

日期：2026-11-07。适用版本：v0.5.28。逐项取舍见 [Magpie 能力对齐清单](magpie-capability-alignment.md)。

按维护者要求，底层采用我们自己的 Go 实现，参考 `yetone/magpie@665b9aff126e26eb5fa8931b7395feb6002185f3` 的已审阅逻辑。产品不导入、不下载、不启动 Magpie，不读取它的数据目录或调用它的管理接口。正常构建只需要本项目源码及锁定的依赖。

完整第三方客户端目录另外固定参考 `f13d5f16f474f9c3baa5081c6cae3934af291cfd`，共 74 个配置入口（3 个主入口、71 个附加入口）。对应源码、特殊配置方式和验收范围见 [客户端清单](magpie-client-support.md)。

## 当前调用链

```text
React / TypeScript：界面、草稿、显示
  → Tauri / Rust：窗口、系统集成、供应商表单、命令与进程通信
  → 继承 stdin/stdout 的版本化管道
  → 我们自己的 Go 核心 backend/cmd/uni-switch-core
      ├─ supplier：模型发现、认证识别、分页、余额
      ├─ modelmeta：实时资料、models.dev、用户覆盖、最终合并
      ├─ localgateway：localhost 入口、认证、路由和已应用快照
      ├─ gateway：双向推理转换、HTTP、SSE、工具、思考、压缩
      ├─ platform：四协议统一入口、API Key／订阅来源、图片／视频和原生透传
      ├─ accounts：OAuth／设备登录、账号切换、令牌刷新与私有存储
      ├─ claudesub：独立 Claude Code 订阅进程及认证 MCP 工具桥
      ├─ agent：主客户端及 71 个附加入口的路径、格式、模型目录投影与回读
      ├─ configuration：确认、状态、journal、修订、恢复和配置更新
      ├─ edit：原子写入、多文件应用和回滚
      └─ state：现有 SQLite 格式的实际 SQL 执行和事务
```

推理入口只监听 `127.0.0.1`。Go 负责端口绑定、令牌生成及保存、认证、拒绝 Origin，并从 SQLite 和目标快照共同校验已应用供应商；随后选取真实模型和上游接口，完成网络请求及响应转换。修改供应商草稿而未应用，不会改变当前路由。失败不回退到旧 Rust 转换器重发。

## 代码与保留边界

| 模块 | Go 负责 | Rust / TypeScript 仍负责 |
| --- | --- | --- |
| 供应商请求 | 模型发现、分页、认证、协议识别、余额 | 供应商表单、CRUD、命令输入及显示 |
| 模型资料和投影 | 最终合并、用户覆盖、三态能力、档位映射、Codex 目录与 Claude 模型列表 | DTO 校验、缓存失效及界面显示 |
| 本地网关与推理 | 本地监听、token/Origin、已应用快照、路由、双向转换、HTTP、SSE、工具、思考和取消 | 启动/健康 IPC 和子进程生命周期 |
| 客户端配置 | 主客户端及 71 个附加入口的路径/格式、受管字段、回读、确认、冲突检测、配置档案、数据库/API/凭据适配和恢复 | 目录选择 UI、原生进程线索和通用目录搜索 |
| 文件与状态 | 原子替换、备份/journal、多文件事务、修订、回滚、启动恢复及 SQLite SQL | 应用单实例锁、供应商表单/CRUD |
| 桌面 | 无 | 窗口、托盘、更新、系统集成及原生 IPC |

SQLite 数据格式保留以兼容旧用户数据；Magpie 使用 JSON 状态文件。Go `configuration` 兼容现有 `operations`、`config_operations`、baseline 与 applied snapshot，恢复中遇到外部修改时继续保护它。覆盖确认由 Go 持有，300 秒过期且每次尝试消耗，绑定供应商、目标状态及所有计划文件。

Go `edit/toml.go` 使用锁定的 TOML 解析器定位表达式并修改原文位置，保留无关字段、项目设置和个人注释；重复应用相同配置不改变文件字节或修订。Rust 的 TOML/YAML 库仅用于回归测试。已删除的 Rust 转换器不作为故障回退。

## 推理行为

- 请求先解析到统一消息、内容、工具、思考和用量表示，再编码到目标协议；流式按事件增量转换。支持 UTF-8 任意字节分片、CR/LF/CRLF、BOM、多行 data、工具参数增量及明确完成状态。
- 工具名称与 ID 可逆，支持命名空间和自由格式输入；Chat 缺失 ID 稳定补齐、Messages 重复 ID 可逆防重。缺失结果补 interrupted/error，孤立结果作为用户数据保留，重复结果取第一条。
- 跨协议时中途 system/developer 留在原位置并转成 user 的 `<system-reminder>`；Responses 原生构造保留原角色与位置。不再提供每模型 inline-system 开关；旧记录的该字段只读入丢弃，不再输出。
- effort/budget/adaptive 依据最终档案映射。只有 `output_config.effort` 不会自行开启 Messages thinking。上游签名及 opaque reasoning 按所属协议保留，跨轮缓存按 URL、Key、模型隔离。
- opaque 缓存最多 1024 项、64 MiB，保留七天；它不是我们重新加密上游资料。旧 Rust `uni-anthropic-continuation:1` / `uni-openai-reasoning:` 记录明确拒绝，涉及这些记录的旧会话需要新建。
- 转换模型的独立 `/responses/compact` 返回 400；通过 `/responses` 的 `compaction_trigger` 产生摘要并续聊。没有 `previous_response_id` 服务端会话存储。
- 只有明确的接口拒绝语义才允许 Responses 改用 Chat；模型不存在、鉴权失败、429、5xx 不触发盲目回放。断流续接最多两次，只限已确认可靠的 prefill 上游且工具尚未开始；普通 Chat 不拼接重新作答。
- `/messages/count_tokens` 按解析内容和工具的字节大小除四估算，不发送推理、不要求 Responses 支持，也不是精确分词。
- 模型连接测试仿照 `provider.tinyBody`：明确确认后只发送一次 `hi`，最多 16 输出 Token。结果只表示本次可用性，不再认证文本/图片/工具/流式四维能力，也不写旧验证观察库。

Gemini 已同时支持原生入站与上游构造／解码，并与 Messages、Responses、Chat 共享统一表示；Google 订阅使用 Code Assist 包装。新增订阅与媒体实现范围、使用步骤和剩余差异见 [统一网关说明](unified-gateway.md)。未覆盖或未验收的分支不能归类成“Magpie 没有”。

本轮进一步按固定 Magpie 源码核对五项核心行为，包含原生 SSE 完成守卫、工具参数实时增量、历史指令位置、预算及别名反查，见 [五项核心行为对齐](magpie-core-alignment.md)。不是只改为 Go 就认定行为等价。

## 进程与传输

- 本次源码构建的核心嵌入桌面文件，解压到本应用创建的随机私有临时目录；不执行 PATH 或主程序旁未知来源的同名文件。用户不需安装 Go。
- 普通异步查询最多四个工作进程，每个串行处理并复用 HTTP 连接。同步元数据/SQL/配置边界使用一个串行、可复用的 Go worker，避免每行查询启动进程。
- 普通 RPC 最大输入 32 MiB、最大输出 4 MiB，单次同步交换上限 35 秒。版本和请求 ID 必须匹配；Go HTTP 推理请求上限 16 MiB。独立推理管道保留为传输回归入口。
- 生产推理由 Go HTTP listener 直接执行，下游断开通过请求 context 取消上游；不再逐个请求启动推理子进程。只回收本次应用创建的子进程。凭据走继承管道，不放命令行。
- 异常、超时或无效响应会丢弃对应 worker，晚到结果不供后一个请求使用；不自动重放写入操作。正常退出先回收 worker，再清理所持临时目录。
- Go 不开放通用网络管理端口；账号管理走原管道。登录期间有临时 loopback OAuth 回调，Claude 工具轮使用临时认证 MCP listener，完成／取消／超时后关闭。缺失核心或版本不符时明确报错。父进程强杀可能遗留临时文件，正常退出清理有隔离测试。

## 构建、依赖与验收

需要 Node.js 22、Go 1.26+ 和原有 Rust/Tauri 工具链；CI 固定 Go 1.26.8。`UNI_SWITCH_GO` 只在构建时指定编译器。Go 依赖由 `backend/go.mod` / `go.sum` 锁定，包含 TOML、YAML 和纯 Go SQLite；许可见 [第三方声明](../THIRD_PARTY_NOTICES.md)。

```sh
go -C backend test ./...
go -C backend vet ./...
node scripts/build-go.mjs
cargo test --locked --manifest-path src-tauri/Cargo.toml --no-default-features
cargo build --locked --manifest-path src-tauri/Cargo.toml --no-default-features --example go_core_fixture
python scripts/test-go-bridge.py
corepack pnpm test
corepack pnpm build
corepack pnpm docs:check
cargo clippy --locked --manifest-path src-tauri/Cargo.toml --all-targets -- -D warnings
```

Windows Cargo 需加载 MSVC，使用 `scripts/with-msvc.ps1` 串行执行。Windows 原生隔离验收运行 `node scripts/qa-go-backend.mjs`，占用 CDP 9223，不与其他原生 UI QA 并行。测试只使用 `.qa`、临时目录、合成凭据和本地 HTTP 模拟，不操作真实客户端或发送收费请求。

当前测试证据和平台限制集中记录在 [能力对齐清单](magpie-capability-alignment.md#验收证据)。Go 跨平台编译成功不等于 Linux/macOS 原生桌面、安装包或真实供应商验收；发布仍需完整多平台 CI 和源码审计。
