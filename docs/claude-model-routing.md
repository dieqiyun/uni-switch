# Claude 模型 ID 与档位映射修复

日期：2026-10-15。适用版本：v0.5.25。

## 对照依据

本次核对 cc-switch 最新正式版 v4.0.12（2026-10-14 发布），以及 2026-10-15 main 提交 `09237ebd4aebd4c8f1695fab3cc348850333de2c` 中的 Claude Desktop 模型校验、模型路由和 Claude Code 配置投影。参考其行为独立实现，没有复制源码。

- [正式版 v4.0.12](https://github.com/farion1231/cc-switch/releases/tag/v4.0.12)
- [Claude Desktop 配置及路由](https://github.com/farion1231/cc-switch/blob/09237ebd4aebd4c8f1695fab3cc348850333de2c/src-tauri/src/claude_desktop_config.rs)
- [Claude Code 配置投影](https://github.com/farion1231/cc-switch/blob/09237ebd4aebd4c8f1695fab3cc348850333de2c/src-tauri/src/live/project/claude.rs)

## 确认的问题与修复

1. **OpenAI 接口下的 Claude 模型也被改成哈希路由。** 旧 `model_alias` 无条件生成 `claude-sonnet-4-6-uni-<hash>`，包括可直接作为桌面菜单 ID 的 Sonnet、Opus、Haiku、Fable 及 `anthropic/claude-*`。现在保留这些真实 ID，只对桌面无法识别的模型（例如 GPT）生成本地路由，转发时解析回真实上游 ID。数据库与应用快照保留供应商原始模型 ID。
2. **档位请求错误落到默认模型。** 旧解析对未精确匹配的 `claude-*` 和 sonnet/opus/haiku 一律返回默认模型；带发布日期的 Haiku、Opus 等请求会调用默认 Sonnet。现在优先精确匹配，再匹配启用的同档模型；Fable 缺失时优先使用已启用的 Opus。已停用的明确模型、未知档位和失效哈希路由返回错误。默认模型没有 Claude 档位时，缺少对应档位的内部请求继续使用默认模型；混合模型列表中已启用的同档 Claude 模型仍优先。
3. **CLI 未管理 Fable，旧版模型名不能正确匹配档位。** 新增 `ANTHROPIC_DEFAULT_FABLE_MODEL` 的写入、冲突检查和恢复；现代名称、`anthropic/` 前缀和 `claude-3-5-haiku-*` 等旧版官方名称使用统一档位识别。停用某档后不会遗留上一配置的档位值。
4. **客户端上下文标记混入桌面模型名。** 将结尾 `[1m]` / `[1M]` 转为 profile 的 `supports1m: true`，不把标记发送给上游。相同基础模型的普通和 1M 变体合并为一个桌面菜单项；profile 与本地模型列表接口保持一致。内部夹带标记等无效原生名称由前后端一致阻止。
5. **回读受管 profile 可能把内部路由当成真实模型。** 仅当 `labelOverride` 与生成哈希能够互相验证时，将旧内部路由恢复成真实模型 ID；任意显示标签不视为模型 ID。上下文能力回读为本地配置标记。

`-uni-` 路由本身并不证明它被发送到了上游：旧 OpenAI 转换服务也会做反向映射。本次复现并修复了不必要的改名及相邻路由错误，没有使用用户的真实请求或密钥来断定某一次失败的全部原因。原生 Anthropic 接入继续直接调用上游；模型列表返回格式也不被当作实际推理协议可用性的证明。

## 旧配置生效

旧受管投影会显示“更新配置”。用户更新后，客户端完全退出再重新打开，加载新的 profile/环境配置。读取状态不写配置，已有外部修改仍需经过原有冲突处理。旧 hash 请求仍可从启用模型恢复，减少客户端重启前的兼容问题。

验证过程未修改、停止或重启用户的真实 Codex、Claude、uni-switch；所有验证使用临时数据目录或 `.qa` 目录与合成密钥。正式发布遵循 [三平台发布流程](github-release.md)。

## 验证入口

- Rust：`src-tauri/src/store/tests.rs` 覆盖 ID 保留、旧别名回读、档位优先级、停用模型、1M、旧投影更新、外部冲突和恢复。
- Rust HTTP：`src-tauri/src/bridge/reverse_http.rs` 从实际生成的 profile 获取路由，分别对 Responses / Chat Completions 的 JSON 与 SSE 请求检查本地模拟上游收到的 `model`，同时检查旧别名和日期后缀路由。
- 前端：`src/lib/useConnectionDiscovery.test.tsx` 检查桌面原生模型边界。
- 原生 QA：`scripts/qa-claude-native-base.mjs` 现在同时覆盖原生 Messages 与两类 OpenAI 转换。可设置 `UNI_SWITCH_CLAUDE_BIN`，启动隔离的真实 Claude Code CLI 完成流式调用。结果保存在 `.qa/claude-native-base/results.json`。桌面推理由生成 profile 的请求回放验证，不声称真实 Claude Desktop 已完成会话验收。

验证结果：前端 182 项通过，Rust 193 项通过（另 1 项环境辅助用例 ignored）；TypeScript/Vite 构建、Windows 桌面 Clippy 和 QA 构建、格式及教程一致性检查通过。扩展后的原生 QA 六组检查通过，真实隔离 Claude Code 分别完成原生 Messages、Responses 和 Chat Completions 流式调用，profile 回放核对生成路由、日期后缀和旧 hash 对应的上游模型；前端运行错误为 0。
