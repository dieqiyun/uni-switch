# 在 Claude Code 中使用 GPT

uni-switch 0.3.8 增加 OpenAI → Claude 协议转换，适用于 Claude Code 桌面端与 Claude CLI。两个目标可分别应用供应商，也可以同时使用同一个供应商。旧 Claude 配置默认保持原生 Messages 协议，升级不会自动改成 GPT 转换。

## 操作

1. 左侧选择 **Claude Code**，再选择 **桌面端** 或 **CLI**。
2. 添加配置，将 **API 协议** 改为 **OpenAI · 在 Claude 中使用 GPT**。
3. 填写 OpenAI 兼容地址与 API Key；等待自动同步模型，勾选需要的 GPT 模型并设置默认模型。
4. 点击 **保存并应用**。完全退出并重新打开 Claude 桌面端，CLI 开启新进程或新会话。

Claude 桌面端的模型列表写入 `{name, labelOverride}`：内部使用确定的 Claude 兼容 ID，显示名称保留真实 GPT ID。此格式与当前已安装的 Claude Desktop 2.9939.2.0 配置 schema 一致。请求到达本地服务后重新映射到真正的 GPT 模型，供应商不会收到伪装的 Claude 模型名。桌面端最多启用 200 个模型，默认模型在首位。

CLI 使用真实 GPT 模型 ID 作为 `ANTHROPIC_MODEL` 和默认模型，也支持 `--model gpt-...`。Sonnet / Opus / Haiku 档位默认映射到应用的 GPT。Claude CLI 的自定义模型列表能力与 Codex 的目录不同；这里不承诺 `/model` 自动列出所有启用 GPT，默认模型和显式模型参数已经验证。

## 转换链路

```text
Claude Code 桌面端 / Claude CLI
    Anthropic Messages 请求
        ↓
uni-switch 本机转换服务
    Messages → OpenAI Responses → Messages
    Responses 接口返回 404 / 405 时改用 Chat Completions
        ↓
供应商的 GPT 模型
```

服务只监听 `127.0.0.1`，使用独立本地令牌认证。Claude 配置中不会写入供应商真实密钥。端口和令牌跨重启保留，Codex、Claude 桌面端与 CLI 各自保留独立的已应用快照。只保存、尚未应用的供应商修改不影响正在工作的连接；切换或恢复会停用原目标路由，不影响另一目标。

**转换期间需要保持 uni-switch 运行。** 关闭主窗口会驻留托盘，从托盘选择“退出（停止协议转换）”才会停止服务。电脑重启后先打开 uni-switch，再启动 Claude。本版本没有自动注册开机启动。

## 已实现的转换

- 系统提示、文本、多轮历史、URL 与 base64 图片。
- Claude `tool_use` / `tool_result` 与 OpenAI function call / output 的双向转换；包括工具失败信息、多个调用、JSON 参数以及流式参数分片。
- 非流式回复、SSE 文本增量、工具调用结束、缓存及普通 Token 用量、截断与错误事件。
- Responses 思考摘要和 `encrypted_content` 保留在本地签名中，后续历史回传时恢复原 reasoning 项。签名只是编码容器，不是 uni-switch 自行加密。
- Claude thinking / effort 在 GPT 推理模型上转换为 OpenAI low / medium / high；超过 high 的 Claude 档位目前映射 high，不代表原档位效果相同。传统 GPT 不发送其不支持的 thinking 参数。
- JSON Schema 输出请求与工具选择；指定 `stop_sequences` 时直接使用支持 stop 的 Chat Completions，避免静默丢弃停止条件。
- 取消下游请求会释放上游；HTTP 与流式错误中的当前供应商密钥会隐藏。Responses 成功但响应无效、鉴权失败、限流时不重复发起 Chat 推理。

## 兼容范围

需要供应商开放 `/v1/models`，模型支持文本与工具调用。优先使用 Responses，只有 Responses 返回 404 或 405 才尝试 Chat Completions。地址可以是根路径、带代理前缀或以 `/v1`、`/responses`、`/chat/completions` 结尾的入口；请求使用 Bearer Key，不跟随重定向。

`/messages/count_tokens` 返回本地估算，不发送计费推理。该估算按请求序列化字节数计算并带 `x-uni-switch-token-count: estimated`，不是 GPT 的精确分词结果，图片和工具定义可能偏差较大。Claude 的上下文压缩时机因此也可能有偏差。

Chat Completions 不支持这里的工具结果图片，转换会明确报错；使用 Responses 可保留图片工具结果。Anthropic 托管搜索工具、PDF / 文档输入、音频、其他服务端专属内容尚未转换。Claude 原生 tool search 声明不向 GPT 发送，延迟函数工具会直接展开；CLI 转换时设置 `ENABLE_TOOL_SEARCH=false`，切回原生 Claude 或恢复配置时还原此前值。MCP 以普通函数形式转换，具体 MCP 服务未单独验收。

Claude 桌面端需要支持第三方推理的客户端；组织策略、启动参数或不同配置目录可能覆盖设置。真实 GPT 能力、权限与上下文容量由供应商决定，协议转换不会扩大模型容量。

## 验证

脚本 `scripts/qa-reverse-bridge.mjs` 使用独立 `.qa` 数据与客户端目录、假 Key、本地 OpenAI 模拟供应商。真实 Claude CLI **2.1.179** 完成文本回复、Read 读取和 Write 创建文件、工具结果回传、Responses reasoning 历史回放、会话恢复和 GPT 模型切换；Responses 404 后的 Chat Completions 也完成真实工具循环。

原生 uni-switch WebView 验证协议选择、模型同步、桌面显示名称及兼容 ID、两个目标独立应用、流式与非流式路由、令牌与 Origin 检查、计数估算、错误与截断、取消、未应用密钥隔离、托盘驻留、重启复用和事务恢复。表单 Axe 检查无违规。

验证范围是 **真实 Claude CLI + 本地模拟 OpenAI 服务**。Claude Desktop 已通过安装包源码核对模型配置格式，并验证隔离 profile 与转换请求；尚未在真实桌面聊天界面完成模型选择及推理验收。没有使用真实供应商密钥，没有更改用户现有配置或结束用户客户端进程。
