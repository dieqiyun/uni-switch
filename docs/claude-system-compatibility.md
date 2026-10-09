# Claude CLI system 消息兼容排查

截图中的完整报错为 API Error: 400 Claude 消息 role 必须为 user 或 assistant。该文本来自 src-tauri/src/bridge/reverse.rs 的请求角色校验，发生在本地 Claude → OpenAI 转换阶段，尚未发出上游推理请求。

## 已确认与尚未确认

- 使用包含 role: system 的 Claude Messages 请求，旧代码会返回与截图完全相同的错误。
- Anthropic 官方 Python SDK 的 MessageParam 和 TypeScript SDK 的 MessageParam 已声明 user / assistant / system 三种角色。BetaMessageParam 还声明 clear_at 和逐消息 output_config.effort。
- 当前安装的 Claude Code 2.1.179 在隔离的首次 print 请求中只发送 user。回归脚本使用它的实际请求，在隔离中继中追加官方格式的 system 消息，验证新协议兼容性。
- 未取得报错用户的脱敏请求体和实际配置，不能仅从截图断定具体消息的完整结构、供应商是否配置错协议，或上游实际提供哪些接口。

官方类型依据：

- [Python MessageParam](https://github.com/anthropics/anthropic-sdk-python/blob/main/src/anthropic/types/message_param.py)
- [TypeScript MessageParam](https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/resources/messages/messages.ts)
- [TypeScript BetaMessageParam](https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/resources/beta/messages/messages.ts)

## 转换行为

顶层 system 仍映射为 Responses instructions。历史中的 system 文本保持原位置和角色，转换为 Responses input 中的 system message；Chat Completions 也保持该位置和角色，不把它提升到开头或降为 user。原有工具调用和工具结果的先后关系保持不变。

clear_at 为 next_user_message 时，已有更晚的 user 消息会使该条 system 的文本失效；never、null 或缺省保持文本。该字段只控制文本，逐消息 effort 按最后一个非空设置应用到当前生成，沿用模型既有的思考强度映射。

system 只转换文本，不允许把工具调用、工具结果、图片或思考块当成 system 指令。未知角色、无效 clear_at / effort 和无法转换的逐消息配置仍返回 Anthropic 格式的 400，不发送上游、不回显消息正文或未知角色原文。

messages 与 messages/count_tokens 共享校验和转换逻辑；计数仍为本地估算，不调用收费推理。此修改不改变供应商协议探测，也不按模型名推断上游协议。

## 隔离验证

后端回归覆盖 Responses / Chat Completions、JSON / SSE、CLI / Desktop、工具历史、system 字符串与文本数组、临时指令、逐消息 effort、token 计数和非法请求。

原生 QA 脚本：scripts/qa-claude-system.mjs。设置 UNI_SWITCH_CLAUDE_BIN 为已安装的 Claude 可执行文件路径，然后运行 node scripts/qa-claude-system.mjs。默认读取 src-tauri/target/debug/uni-switch.exe，需先使用 qa-webview feature 构建，并提供已构建的前端 dist。

若要对旧二进制复现，另设置 UNI_SWITCH_QA_EXE 为旧 QA 可执行文件，UNI_SWITCH_QA_EXPECT_LEGACY_ROLE_ERROR=1。脚本验证相同请求返回截图中的原错误且未调用上游。

所有数据、设置、客户端工作目录和 WebView 数据均位于 .qa/claude-system；仅使用本地合成服务器和合成密钥，不修改或停止真实客户端。测试报告保存在 .qa/claude-system/before-report.json 和 after-report.json。这些文件包含合成请求，不进入发布源码。
