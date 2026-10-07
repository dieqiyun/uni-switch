# 在 Codex 中使用 Claude 模型

0.3.7 在 Codex 供应商表单中增加「API 协议」。选择 **Claude Messages · 在 Codex 中使用 Claude**，填写供应商的 Claude 接入地址和 API Key，等待模型自动同步，勾选要使用的模型并保存、应用。完全退出并重新打开 Codex 后，桌面端的模型菜单和 CLI `/model` 可以选择已启用的 Claude 模型。0.3.8 增加相反方向：[在 Claude Code 中使用 GPT](reverse-protocol-bridge.md)。

供应商本身已经提供 OpenAI Responses 兼容接口时，继续选择 OpenAI Responses 即可。只有 Chat Completions 的接口仍不支持。

## 请求链路

```text
Codex 桌面端 / CLI
  OpenAI Responses 请求、流式回复和本地工具调用
      ↓
uni-switch 内置的本机 HTTP 转换服务
  Responses → Anthropic Messages → Responses
      ↓
供应商的 Claude Messages API
```

服务仅监听 `127.0.0.1`。初次启动分配空闲端口，端口和独立本地令牌写入当前用户私有的数据目录，后续启动复用，因此重启 uni-switch 无需重新应用 Codex 配置。Codex 配置里的地址是本地服务，密钥是独立的本地令牌；供应商的实际地址、密钥与认证方式保存在 uni-switch 中，不写进 Codex 配置。

实际生效的上游配置使用独立快照，与 Codex 文件一并进行事务写入。仅保存、尚未应用的地址或密钥修改不会改变正在使用的请求。切换为其他供应商或恢复原配置后，旧转换路由停止接受请求。模型自动写入仍保留当前 Codex 的有效模型与思考强度选择。

## 使用体验

- 选择 Claude 协议后默认使用 `x-api-key`，原生 Anthropic 与兼容网关都可使用。供应商要求 Bearer 时，在更多选项中修改认证方式。所有 Messages 请求包含 `anthropic-version: 2023-06-01`。
- 地址支持站点根路径、代理前缀或以 `/v1` 结尾的地址。自动请求相应 `/v1/messages` 或 `/messages`，不改变真实模型 ID。
- 仍自动查询供应商模型列表与余额。余额取决于供应商是否开放兼容接口；Anthropic 官方没有这里使用的第三方余额接口，查询失败不会阻止使用模型。
- 每个模型的上下文长度仍默认 256k，可独立调整。此值不能增加模型实际上下文容量，应按供应商支持长度设置。
- 关闭主窗口时，如果正在使用协议转换，会驻留系统托盘。双击托盘图标或选择「打开 uni-switch」显示窗口。托盘菜单「退出（停止协议转换）」会真正结束程序。
- **使用 Claude 转换时必须保持 uni-switch 运行。电脑重启后，先打开 uni-switch，再打开 Codex。** 本版本没有自动注册开机启动。

## 支持范围

请求转换包括系统与开发者指令、用户与助手消息、完整多轮历史、URL/base64 图片、函数工具及结果、命名空间、自定义工具的原始输入（包括 `apply_patch`）。连续同角色消息合并为 Anthropic 内容块；Codex 请求不依赖 `previous_response_id`。

回复转换包括非流式 JSON、SSE 文本增量、工具 JSON 增量、工具调用完成事件、思考内容与签名保留、缓存与普通 Token 计数。网络数据按完整字节行解析，避免中文字符被网络分片截断。取消 Codex 请求会释放上游连接；上游 HTTP/流式错误和截断不会被报告为成功，错误中的当前供应商密钥会被隐藏。

思考强度以兼容映射发送：识别到 Claude 4.6 / 4.7 使用 adaptive thinking，其余模型使用预算方式；low / medium / high 对应不同档位，xhigh 映射 high，max / ultra 在 Opus adaptive 模式映射 max。兼容网关和模型仍需支持相应 thinking 参数；修复显示列表不代表各强度拥有独立效果。

`/responses/compact` 使用 Claude 生成接续摘要，并返回本地可识别的 compaction 项。此记录用于后续请求重建摘要，工具历史转换为文本资料，避免摘要请求调用工具。摘要与思考签名保存在编码的 opaque 字段中，不宣称加密。真实 Codex 在当前验证环境中使用了本地压缩；单独验证了转换服务的 compact 接口和摘要回放。

## 边界

- 转换模式自动关闭 Codex 内置网页搜索；OpenAI 托管搜索/电脑操作等工具、音频、文件输入和仅靠 `previous_response_id` 的服务端续聊尚未转换。Codex 本地工具与 MCP 暴露的函数工具可以经过函数转换链路，具体 MCP 服务未在本次验收中测试。
- 不将 OpenAI `priority` / Fast 映射为 Anthropic 服务等级，表单禁用 Fast，并对外部发送的 priority 请求明确报错。切换回直接连接或恢复原配置时，会还原网页搜索设置。
- 与供应商之间不跟随重定向；请填最终接入地址。监听端口被其他进程占用时应用提示错误，不会自动更换端口导致已写入的 Codex 地址失效。
- API 权限、真实模型 ID、模型上下文、服务可用性和网关 thinking 兼容性仍由供应商决定。

## 验证记录

验证脚本：`scripts/qa-protocol-bridge.mjs`，使用独立数据目录、Codex 配置和假密钥、本地模拟 Anthropic 供应商，不访问用户实际配置。真实运行的 Codex 为 **0.160.0**。

已验证原生 UI 协议选择、模型发现及写入、两个 Claude 模型识别、流式和非流式回复、工具调用、思考签名回放、同一会话模型切换、压缩后继续对话。编码工具确实读取了隔离目录文件，`apply_patch` 确实创建了文件，供应商收到了工具结果。

独立运行真实 Codex CLI `exec` 完成 Claude 回复，并验证 high 思考强度转换成 Anthropic adaptive thinking + high effort。关闭窗口后转换仍可用，重启 uni-switch 后无需重写 Codex 配置即可继续请求。

另外验证本地授权与 Origin 拒绝、中文 SSE、命名空间与自定义工具、HTTP 429、流式错误、流截断、取消释放上游、未应用密钥不影响活跃连接、恢复原配置停用路由。前端和 Rust 回归、Clippy、构建及原生表单 Axe 检查均为验收内容。

这是 **真实 Codex 运行时 + 本地模拟 Anthropic 供应商** 的验证；没有使用真实 Claude 密钥，也没有对用户正在运行的 Codex 桌面端会话做调用验收。
