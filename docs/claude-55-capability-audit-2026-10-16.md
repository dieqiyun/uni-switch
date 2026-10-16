# Claude 5.5 与默认开放能力审计

日期：2026-10-16。适用范围：codex/claude-55-capabilities 工作分支，尚未发布；不属于已发布的 v0.5.26。

## 对照实现与依据

本轮核对 cc-switch 最新正式版 v4.0.13（2026-10-15 16:32:11 UTC 发布），以及 main 提交 0a8d064591461717947390715be4a8090fdc202d 中的 Claude Desktop 配置、Claude 原生配置、模型映射、Anthropic 转换和能力资料实现。参考其配置和路由行为独立修复，没有复制源码。

- [cc-switch v4.0.13](https://github.com/farion1231/cc-switch/releases/tag/v4.0.13)
- [Claude Desktop 配置与路由](https://github.com/farion1231/cc-switch/blob/0a8d064591461717947390715be4a8090fdc202d/src-tauri/src/claude_desktop_config.rs)
- [Claude 原生配置](https://github.com/farion1231/cc-switch/blob/0a8d064591461717947390715be4a8090fdc202d/src-tauri/src/services/provider/claude_direct.rs)
- [模型映射](https://github.com/farion1231/cc-switch/blob/0a8d064591461717947390715be4a8090fdc202d/src-tauri/src/proxy/model_mapper.rs)
- [Opus 5.5 概览](https://platform.claude.com/docs/en/models/opus-5-5/overview)与[迁移说明](https://platform.claude.com/docs/en/models/opus-5-5/migration-guide)
- [Sonnet 5.5 概览](https://platform.claude.com/docs/en/models/sonnet-5-5/overview)与[迁移说明](https://platform.claude.com/docs/en/models/sonnet-5-5/migration-guide)
- [对话中的系统消息](https://platform.claude.com/docs/en/build-with-claude/mid-conversation-system-messages)与[思考保留及签名绑定](https://platform.claude.com/docs/en/build-with-claude/preserved-thinking)

模型 ID、思考参数和限制以模型官方资料为依据，不把参考项目目前是否列出某个新型号当成协议支持的证明。Opus 官方概览同时明确 Fable 5.1 具有始终思考和强制工具限制，本轮一并补齐该档案。

## 发现并修复的问题

| 问题                           | 触发和影响                                                                                                                           | 修复                                                                                                                               |
| ------------------------------ | ------------------------------------------------------------------------------------------------------------------------------------ | ---------------------------------------------------------------------------------------------------------------------------------- |
| 5.5 缺少思考协议档案           | 内置目录只声明图片、并行工具；转换有 effort 的请求时会被当成未知思考格式拒绝。                                                       | 补齐 adaptive、五档 effort、默认档位、输出上限和协议约束，保留旧 Claude 4.5 的预算逻辑。                                           |
| 新模型参数不兼容               | Opus 不能关闭思考，Sonnet 的最低模式变为 between_tools，不能套用 disabled 或旧预算。                                                 | Sonnet none 转为 between_tools + low，Opus/Fable none 明确提示；adaptive 显式使用 summarized，排除不兼容采样参数。                 |
| 工具验证使用不受支持的强制选择 | Messages 合成工具验证总是指定 qa_echo，5.5 会拒绝。                                                                                  | 已知限制模型改用 auto，仍要求实际返回 qa_echo 和正确标记才记为验证通过。普通转换的 required/指定工具要求不被静默降级。             |
| 空思考流生成空/无效摘要        | 只有签名的块仍提前建立摘要，正文缺失时结束事件可能含 null；初始正文也缺少对应 delta。                                                | 有正文才建立摘要，处理初始正文和后续增量；保留完整分片签名，空增量不添加原本不存在的字段。                                         |
| 带上下文标记的已知型号丢失资料 | Claude ID 结尾 [1m] 未被资料匹配识别，可能退回未知格式。                                                                             | 前后端一致识别结尾标记；私有命名空间、任意自定义后缀仍不冒充官方型号。                                                             |
| Claude → OpenAI 丢失思考意图   | between_tools、disabled、只有 output_config.effort 的请求和 Adaptive 目标会漏发 reasoning；无显式 effort 的 adaptive 也会误套 high。 | 最低模式映射到目标最低声明档位，独立 effort 与有效默认值继续生效；未声明时保守 low，DeepSeek none 和手动无思考覆盖保持独立。       |
| 中途指令前移使签名失效         | 后续 system/developer 一律被合并进初始 system，改变先前思考签名绑定的上下文。                                                        | 新增 midConversationSystem 协议档案，对明确支持型号保留位置；检查 user → system → assistant / 末尾的合法顺序，旧型号维持既有格式。 |
| 自动续接消息在下一轮消失       | 桥接为避免 assistant prefill 而插入 user 指令，但客户端历史未包含它，下一轮可能丢失签名前缀。                                        | JSON/SSE 返回固定、带版本的不可见 reasoning 记录，下轮重建同一条 user；完整保留真实思考块，不保存额外服务端会话状态。              |
| 旧能力关闭值继续阻断使用       | 未知型号和旧 image/tool/structured_output false 可能在本地拦截上游实际可用的请求。                                                   | 按本轮要求默认开放，删除能力勾选及废弃 setter/CSS，保持上游资料原值供参考。                                                        |

原生 Claude 接入在本轮之前已保留真实模型 ID。本轮没有把既有正确行为当成新 bug，而是将 5.5、角色槽、上下文标记及恢复加入回归。原生接入不通过本地代理重写模型思考参数，实际参数由对应 Claude 客户端及上游决定。

上下文压缩也覆盖这套顺序约束：尾部 system 组仍置于最后的 user 和生成位置之间，摘要指令追加到对应 user 内。删除某个助手回合的全部思考后，如果它承接中途 system，则保留不含思考内容的占位文本，避免下一条 user 直接跟在 system 后。回归先复现了旧压缩请求被模拟上游以非法顺序拒绝的情况，再验证普通历史、尾部系统组和纯思考助手三种压缩均成功。

## 多轮历史修复的边界

midConversationSystem 仅为已有依据的 Opus 5.5、Sonnet 5.5、Fable 5.1、Fable 5、Haiku 5.5 启用，支持上游元数据和手动协议覆盖；未知私有后缀不会仅凭名字继承。初始指令继续作为顶层 system。中途消息不允许紧跟 assistant 或放在下一个 user 前，避免静默改写角色顺序。此处检查协议形状，不扩大模型能力勾选范围。

OpenAI 没有 between_tools 的完全等价模式，转换选择目标支持的最低 effort；缺少目标档位时用 low，不能承诺完全关闭。原生 Messages 接入由 Claude 客户端直接决定参数。

自动续接记录只代表固定续接文本，不承载任意系统指令。仅在历史以完整 assistant 结束、没有待返回工具结果时生成；版本或位置错误明确拒绝。普通与流式响应使用相同记录，流式输出索引整体偏移，空摘要不会显示成正文。旧的 thinking / compaction 编码继续兼容。客户端仍需完整保留这些 encrypted_content；已经丢失历史的旧会话无法凭空修复，换模型、账号或改早期历史仍可能需要新建会话。

## 默认开放的边界

图片输入和并行工具是原模型列表中的能力勾选；现在所有型号默认开启，旧手动关闭值不再生效。工具和结构化输出请求也不再仅因旧声明被本地阻止。Codex 目录、OpenAI → Claude 转换、WorkBuddy 图片/工具和 DSH 图片声明使用一致策略。模型启用选择、思考格式、接口类型以及付费验证同意仍保留。

客户端明确发送 parallel_tool_calls: false 或 disable_parallel_tool_use: true 时继续串行。未知接口格式和不支持的特殊内容块仍会明确报错；默认开放不是把所有协议参数都设成 true。模型上架与权限仍由供应商实际返回的列表及密钥决定。

除上游“不支持能力”错误外，还需考虑：

- 图片增加请求体积、上传耗时和可能的 Token 成本，开启声明本身不会主动上传图片。
- 并行工具可能增加同轮工具数量，涉及文件写入等操作时，先后依赖和副作用由客户端调度；显式串行约束不会被覆盖。
- 新模型可能默认消耗思考 Token，低档位也不等于完全关闭。默认隐藏的思考可能表现为长时间没有可见进度；转换采用 summarized 恢复可见摘要，原生客户端自行决定显示方式。
- 思考签名绑定模型、账号和早期历史。换模型、换账号、修改早期系统提示/工具/消息可能导致块被丢弃或 400；需要时新建会话。转换保留收到的原始思考块，但不能修复客户端改写的历史或跨账号签名。
- Claude 的托管工具、计算机使用、特定 beta 内容等仍需对应协议实现；开放能力声明不会自动实现这些专用功能。
- 验证仍限制为每次一个、最多 256 输出 Token 的合成请求。始终思考模型可能在输出答案前用完预算，此时保持待确认，不误判“不支持”，也不自动扩大费用或重试。

## 本轮验证

| 验证                    | 结果与覆盖                                                                                                                                                                                                                                                               |
| ----------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------ |
| 前端 Vitest             | 26 个文件，187 项通过。覆盖能力开关消失、取消不写入、保存清除旧覆盖、刷新保留思考格式、5.5 桌面名称边界及中途系统消息档案优先级。                                                                                                                                        |
| Rust 全套               | 214 项通过、1 项原有环境辅助用例忽略。覆盖参数矩阵、最低/默认 effort、旧预算格式、未知格式、空/初始思考、签名分片回放、中途系统消息顺序、续接记录校验、默认并行与显式串行、配置保护和恢复。                                                                              |
| 桌面 Clippy             | Windows 桌面默认 features、all-targets、-D warnings 通过。                                                                                                                                                                                                               |
| 生产前端                | TypeScript + Vite 构建通过；依赖注释及现有包体积提示不影响构建。                                                                                                                                                                                                         |
| 教程一致性              | 软件内 JSON 与生成 Markdown 一致。                                                                                                                                                                                                                                       |
| 真实隔离 Claude Code    | 6 组通过：两个完整 5.5 ID、opus/sonnet 角色别名，以及两个模型的空 thinking 签名 + Read 工具续接。实际产生 8 个本地模拟推理请求。                                                                                                                                         |
| HTTP 多轮签名回归       | 两组各五轮，交替 JSON/SSE，覆盖连续两轮助手续接、工具调用/结果、追加 system/developer 指令；模拟上游按真实历史前缀校验签名。删除续接记录后稳定拒绝，正常回放全部通过；流事件索引与最终输出一致；普通历史、末尾系统组和纯思考助手的 HTTP 压缩均通过，不额外插入生成续接。 |
| Claude Desktop 路由回放 | 从真实生成的配置获取模型名称，覆盖 Chat/Responses、JSON/SSE、模型 ID、旧别名和角色回退；带旧 false 声明的图片仍完整到达模拟上游。                                                                                                                                        |

源码审计覆盖当前工作树的 476 个源码文件，未发现凭据或私有/生成路径；使用临时 Git index，真实暂存区保持原样。Rust fmt、手工维护文件的 Prettier 和 git diff --check 通过。生成的教程由 docs:check 校验；其 Markdown 排版延续生成器格式（基线同样不满足 Prettier），没有为格式检查单独改写生成结果。

真实 CLI 验证入口为 [scripts/qa-claude-55.mjs](../scripts/qa-claude-55.mjs)，可通过 UNI_SWITCH_CLAUDE_BIN 指定已安装的 CLI。每轮在 .qa/claude-55-audit 下创建独立目录，清除继承的供应商变量，隔离 HOME/USERPROFILE/APPDATA/LOCALAPPDATA/CLAUDE_CONFIG_DIR/CODEX_HOME，只请求 127.0.0.1 模拟服务并读取本轮合成文件；仅结束本轮创建的进程。检查报告保存到该轮目录的 report.json。

本轮没有使用用户真实密钥，没有发起付费上游推理，没有修改或重启用户的真实客户端。Claude Desktop 完整 GUI 会话未验收，不能把生成配置和请求回放称为真实桌面会话通过。修复当前仅在工作分支，未创建新发布标签或发布附件。
