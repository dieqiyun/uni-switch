# 订阅账号与统一网关

日期：2026-11-07；适用版本：v0.5.28。

固定参考 [Magpie 665b9aff](https://github.com/yetone/magpie/tree/665b9aff126e26eb5fa8931b7395feb6002185f3)。按其“入站解析 → 统一消息表示 → 上游协议构造 → 响应解析／输出”的结构，用本项目的 Go 代码实现。Gemini 已有原生入站和上游请求，不再仅靠兼容 Chat 接口调用 Gemini 模型。Magpie 不是构建或运行依赖；实际参考文件及哈希见 [参考记录](magpie-platform-reference.json)。哈希核对只证明参考版本，不能证明所有行为等价。

## 使用

1. 打开「设置 → 订阅账号与统一网关」。
2. 使用订阅时，在「订阅账号管理」选择服务，添加并登录，再打开授权页面。Google / Antigravity 须先填写已获对应服务授权的桌面应用 OAuth Client ID 与 Client Secret；程序不内置第三方客户端注册信息。Copilot 显示设备验证码；浏览器未自动回调时，可提交完整回调地址。Claude 可提交登录代码。
3. 在「网关来源」添加来源，选择 API Key、某服务的当前账号或固定账号。API Key 来源填写地址和协议；Gemini 示例地址为 `https://generativelanguage.googleapis.com/v1beta`。
4. 每行填写一种模型，如 `chat gemini-2.5-pro`、`image gemini-image-model`、`video sora-model`。模型 ID 必须属于账号或 Key 的实际可用模型。
5. 点击「添加到供应商列表」，再按现有流程选择客户端并应用。该供应商通过统一网关转到所选来源。

「显示本地网关连接」提供本机地址及遮罩 Key。HTTP 客户端使用此本地 Key；上游 Key 和订阅令牌由 Go 核心保管，不作为模型列表或账号列表字段返回。

网关来源独立于客户端的已应用快照。编辑此来源会立即影响通过它发送的新网关请求；原供应商的客户端配置仍遵守现有确认／应用流程。绑定“当前账号”的来源随账号切换变化，绑定固定账号的来源保持账号选择。删除或停用账号后，使用它的请求会明确报错。

## 订阅路径

| 服务 | 登录与续期 | 推理路径 |
| --- | --- | --- |
| ChatGPT / Codex | OAuth PKCE、state、本机回调；刷新令牌原子持久化 | Codex Responses 后端；账号头、强制流式、`store:false` 及参数裁剪 |
| Google / Gemini CLI | Google OAuth PKCE；访问令牌续期、Code Assist 项目获取或开通 | Code Assist `v1internal` 包装；共享 Gemini 构造／解析 |
| Google / Antigravity | 独立 OAuth 客户端与账号；可填写 Cloud 项目 | Antigravity Code Assist 上游 |
| GitHub Copilot | GitHub 设备授权；按需换取并刷新 Copilot 短期会话 | 会话返回的 API 地址与所选原生协议 |
| Claude | 在本应用独立目录运行已安装的原生 Claude Code；CLI 管理续期 | 隔离 CLI 的 stream-json；声明工具通过带认证的本机 MCP 桥回传给调用者执行 |

刷新采用账号管理器串行更新，防止并发覆盖轮换后的 refresh token。账号切换、启停、移除和 Google 项目设置均由 Go 处理。登录可取消，流程最长十分钟；手动刷新最长二十五秒，失败保留此前凭据。Claude 未安装时有明确提示；不会接管用户原有 Claude 配置。

Google OAuth 注册信息随账号保存在本机私有账号目录，只用于该账号的授权交换和刷新，不通过账号列表回显。缺失配置时在打开登录监听端口前拒绝；旧测试账号没有保存注册信息时须重新登录。普通自建 OAuth 客户端不一定获准调用 Code Assist 或 Antigravity，仍取决于 Google 的服务授权；没有合适注册信息时可使用 Gemini API Key 来源。PKCE、state 和刷新流程沿用固定参考逻辑，客户端注册值改由用户配置，这是与参考默认值的明确差异。

Claude 工具轮支持并行调用；下一次请求须带回本轮全部工具结果。桥接进程按账号、模型和工具 ID 定位，空闲十五分钟回收，最多三十二个活动进程。Claude 的本机文件／Shell 工具不向模型开放。普通历史以带角色的会话数据提供给 CLI；没有复刻 Magpie 全部持久房间、分支和 CLI 安装管理。

## HTTP 接口

沿用同一个仅监听 `127.0.0.1` 的 Go listener，要求本地 Bearer Key，并拒绝带 Origin 的请求。Messages 也接受 `x-api-key`；Gemini 接受 `x-goog-api-key` 或 `?key=`。

| 路径 | 用途 |
| --- | --- |
| `GET /v1/models` | 聊天模型；`kind=image,video` 或 `kind=all` 可包含媒体模型 |
| `GET /v1beta/models` | Gemini 模型目录 |
| `POST /v1/chat/completions` | Chat Completions |
| `POST /v1/responses` | Responses |
| `POST /v1/messages` | Anthropic Messages |
| `POST /v1/messages/count_tokens` | 本地估算 |
| `POST /v1beta/models/{来源ID}/{模型ID}:generateContent` | Gemini JSON |
| 同上，后缀 `:streamGenerateContent?alt=sse` | Gemini SSE |
| 同上，后缀 `:countTokens` | 本地估算 |
| `POST /v1/images/generations`、`/v1/images/edits` | 图片生成与编辑 |
| `POST /v1/videos` | 创建视频任务 |
| `GET /v1/videos/{id}`、`GET /v1/videos/{id}/content` | 查询与下载 |
| `DELETE /v1/videos/{id}` | 转交厂商删除操作，厂商须支持 |
| `/v1/native/{来源ID}/{厂商原生路径}` | 指定 API Key 来源的原生 HTTP／SSE 透传 |

模型列表返回 `来源ID/真实模型ID`，避免不同来源同名歧义。兼容 `/v1/magpie/native/` 与 `/v1/uni-switch/native/` 前缀；这里的 Magpie 名称仅为路径兼容，不连接 Magpie 服务。

Gemini 与其余三种协议之间共享 Go 消息、文本、图片／文件、函数调用／结果、JSON Schema、推理参数、结束状态和用量。Google Code Assist 使用同一构造器，在传输层增加 model/project/request 包装。工具结果按名称 FIFO 与 ID 配对并归入 user 轮；协议字段归一化不改调用参数、结果及 Schema 属性名。Gemini 3+ 思考档位与旧模型 budget 分开映射。

Gemini 工具签名保存在按地址、凭据、模型隔离的本机缓存中，跨协议的工具 ID 仅携带短引用；回传时恢复原 ID 和签名。没有有效签名的历史函数调用采用参考中的 `skip_thought_signature_validator`。原生 Gemini 文本签名保留在原字段，不送入 Anthropic 的签名字段。跨协议转换不保证所有厂商扩展无损；同协议 API Key 请求尽量原样转发，完整厂商字段可通过原生路径发送。

## 媒体适配范围

| 厂商 | 图片 | 视频 |
| --- | --- | --- |
| OpenAI 兼容 API | generations／multipart edits，保留文件及附加字段 | 异步创建、状态、下载、删除透传 |
| Google Gemini | generateContent 图片输出、JSON／multipart 参考图片；HTTPS 图片下载不附带凭据 | 厂商原生透传 |
| 阿里 DashScope | Qwen 多模态图片请求及结果归一化、参考图片、尺寸转换 | 厂商原生透传 |
| ModelScope | 单 Key 队列、异步提交和轮询、多张顺序生成、429 等待重试 | 厂商原生透传 |
| OpenRouter | 图片模型走 images；Gemini/GPT-5 Image 走 Chat 图片输出 | 厂商原生透传 |
| 火山 Ark | 兼容图片接口、原生路径保留 API base | 视频任务及首尾帧／参考图参数 |
| MiniMax / Grok | 兼容图片接口或原生透传 | 独立视频路径、任务状态归一化 |

图片数量限定 1–10；Gemini 多张按单次生成顺序请求。厂商适配不接受其不支持的 mask；OpenAI 兼容编辑保留 mask。视频任务记录绑定创建来源与凭据指纹，更换 Key 后拒绝混用旧任务。厂商返回的 HTTPS 下载地址不会收到上游或本地认证头。

这些统一媒体端点当前使用 API Key 来源；没有加入 Magpie 的全部订阅绘图、插件绘图、Vertex／Imagen 专用适配、账号失败自动轮换、媒体界面及厂商特殊参数。此范围和限制须与产品说明保持一致，不能宣称 Magpie 全部媒体分支均已复现。

## 验收

`gateway/gemini_native_test.go` 覆盖四协议的 32 组 JSON／SSE HTTP 路径、任意字节分片、工具签名隔离、Schema 属性名保护、Code Assist 包装输出及断流拒绝。`accounts/accounts_test.go` 使用 localhost OAuth／设备授权模拟，验证 PKCE、state、取消、并发刷新与令牌持久化。`claudesub/bridge_test.go` 用本测试创建的模拟 CLI 验证两工具并行及续传，不运行真实客户端。

`platform` 测试覆盖认证、来源选择、原生透传、Gemini 与 OpenAI multipart 编辑、ModelScope 异步轮询、OpenRouter／DashScope、视频任务持久化及换 Key 拒绝。前端测试覆盖来源保存、账号设置、登录取消和供应商登记；Windows 隔离桌面脚本为 `scripts/qa-go-backend.mjs`。

本轮 Go test/vet、前端 194 项／29 文件、TypeScript/Vite、Rust 164 项（另 1 项 ignored）、桌面 Clippy 和教程检查通过；Windows x64、Linux x64/arm64、macOS x64/arm64 五个 Go 核心编译目标通过。最新 Windows 原生证据为 `.qa/go-backend/1794055571892/results.json`，包含统一 Gemini IPC、来源界面与供应商登记、图片／视频／原生路由、认证隔离、界面无障碍及正常退出清理。另检查 1120px／390px 窗口和键盘焦点，表单未出现横向溢出。

本轮没有真实订阅登录或付费推理；Google/Copilot/Claude/Codex 实际服务的模型权限、版本和额度仍须用维护者自己的账号验证。Linux/macOS 仅做 Go 交叉编译，不能替代原生桌面验收。
