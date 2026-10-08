# Codex 默认模型与实际请求不一致：本地排查记录

日期：2026-10-08。对象：v0.5.20 问题复现与 v0.5.21 修复。结论基于隔离 uni-switch、真实 Codex CLI 0.160.0 的 app-server 和本地模拟 OpenAI 供应商，不使用真实密钥、用户会话或付费请求。

## 已确认的两个 uni-switch 问题

1. **重新应用、供应商编辑/切换会删除有效的用户思考强度。** 在供应商没有明确的 reasoningEffort 时，旧 adapter 删除 config.toml 的 model_reasoning_effort。原先的 xhigh 因而消失；本地真实请求验证显示，新会话没有发送 reasoning.effort。不能把未发送参数直接等同于 medium，后续取决于客户端或上游默认值及后台展示口径。
2. **列表仅显示上次应用快照，不反映 Codex 自行保存的选择。** 原代码接受 Codex 对已启用模型及合法强度的修改，保持 applied 状态，列表却仍从 applied_summary 读取供应商默认模型。因此供应商默认 gpt-6.1-sol 和配置文件中的 gpt-5.6-terra / medium 可以同时存在，用户没有差异提示或快捷重新应用入口。

## 真实请求证据

使用启用的 gpt-6.1-sol 和 gpt-5.6-terra ID，模拟供应商记录请求中的 model、reasoning.effort；它们在此次实验中仅作为供应商模型 ID，不证明模型的官方身份或后台实际计算使用的权重。

| 情景 | 请求 model | 请求 reasoning.effort |
| --- | --- | --- |
| 在 5.6-terra / medium 配置下启动会话 | gpt-5.6-terra | medium |
| 写入 6.1-sol 后继续运行同一个旧会话 | gpt-5.6-terra | medium |
| 修复前重新应用，启动新会话 | gpt-6.1-sol | 未发送 |
| 修复后重新应用，启动新会话 | gpt-6.1-sol | xhigh |
| 新进程恢复旧会话 | gpt-5.6-terra | medium |
| 在旧会话明确选择 6.1-sol / xhigh | gpt-6.1-sol | xhigh |

这里同时验证了：全局配置不是每个会话的实时设置。仅写配置或重启进程，不保证恢复的旧会话切换模型和强度。不能为修复这个问题自动修改或删除用户会话。

## 已完成修复

- 供应商未明确指定强度时保留 config.toml 的有效 model_reasoning_effort；供应商明确指定时应用其设置。
- 首次接管、重应用、编辑、切换和确认覆盖均保留有效选择；恢复仍返回接管前的原始强度，非法配置仍受现有保护。
- 新增仅包含模型/强度的受管配置回读，区分 appliedModel 与 configuredModel / configuredReasoningEffort，不读取凭据和会话内容。
- 全局文件选择不同于供应商默认时，列表显示「Codex 配置当前选择」并允许「重新应用」；供应商明确强度不同也提示。文件中的选择不称为实时请求或后台实际使用。
- 重新应用后继续提醒重启，并说明已有会话需要在 Codex 内确认模型及强度。模型刷新、上下文和 Fast 保持原有保留客户端选择的规则。

## 此用户反馈仍需核对的边界

复现了足以造成类似现象的本地问题，但没有该用户同一次请求的证据，不能断定他们那一次的完整原因。OpenAI 直接接入由 Codex 向供应商发请求，uni-switch 不代理这条路径。供应商可能还存在模型映射、路由或强度展示口径。

复测建议：确认实际配置目录；新建会话或在原会话显式选择 gpt-6.1-sol 与 xhigh，发送一个简单请求；以请求时间或请求 ID 核对客户端发出的 model、reasoning.effort 和上游记录。如果原始请求已是 6.1-sol/xhigh，后台仍记为 5.6-terra/medium，应进一步检查对应供应商的映射与日志口径。诊断只需要时间、请求 ID 和这两个字段，不要收集 API Key、提示词或完整对话。

## 验证入口

- Rust 回归：思考强度保留、供应商明确覆盖优先、首次快照恢复、状态回读不修改文件、外部冲突不伪装为有效选择。
- 前端回归：模型差异、强度差异、无回读的旧客户端兼容、重新应用后差异消失。
- 原生脚本：scripts/qa-model-routing.mjs；真实 Codex RPC 辅助：scripts/qa-codex-request-parameters.mjs。
- 修复前证据：.qa/model-routing/results-before-fix.json；修复后证据：.qa/model-routing/results.json。

以上修复包含在 v0.5.21，旧会话行为保持不变；切换后请新开对话或在客户端明确选择参数。
