# Claude 原生协议的 Base URL 与 404

Claude Code 的 Anthropic 客户端会在 Base URL 后追加 `/v1/messages`；模型发现和 token 计数分别追加 `/v1/models`、`/v1/messages/count_tokens`。因此供应商接入地址如果已经以 `/v1` 结尾，不能原样写入 Claude 桌面 Gateway profile 或 CLI 的 `ANTHROPIC_BASE_URL`，否则实际推理路径会变成 `/v1/v1/messages`。

这种路径错误可能在 Claude 桌面端显示为 **Model isn't available**。模型已存在于供应商目录、协议选择正确，也不能避免此错误。须展开错误详情或查看诊断日志，区分 `Invalid URL`、模型不存在、密钥权限不足及服务端故障，不能把所有 404 都判定为模型下架。

## 修复范围

- 仅在生成 Claude 桌面和 CLI 配置时，去除明确的末尾 `/v1` 与尾斜杠。
- 保留自定义网关前缀、域名、协议、端口和其他路径。例如 `https://gateway.example.test/custom/anthropic/v1` 写入为 `https://gateway.example.test/custom/anthropic`。
- 不修改数据库内的上游 Base URL、模型 ID、启用列表、认证方式、模型档位或上下文设置。Codex 的配置和本地协议转换地址继续沿用原有逻辑。
- 配置写入仍使用原有备份、冲突确认和恢复机制；恢复时还原接管前的完整受管字段，不对原始地址再做整理。
- 已写出的旧配置不会因更新源码立即改变。包含本修复的 uni-switch 会将旧的受管地址标为待更新，用户点击“更新配置”后才写入；原有外部冲突保护仍然生效。按提示完全退出并重开客户端，再开启新会话或重试。

## 验证

Rust 回归覆盖根地址、尾斜杠、版本后缀、自定义路径及端口、Bearer/API Key、模型列表不变、上游地址不变和原值恢复。`scripts/qa-claude-native-base.mjs` 使用隔离 uni-switch、合成密钥和本地模拟网关，验证桌面 profile 的请求路径，并可通过 `UNI_SWITCH_CLAUDE_BIN` 验证实际 Claude Code CLI 的请求；不发送真实会话或计费推理。

依据：[Anthropic SDK Messages 路径](https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/resources/messages/messages.ts)、[SDK URL 拼接](https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/client.ts)、[Claude Code 网关配置](https://code.claude.com/docs/en/llm-gateway-connect)。
