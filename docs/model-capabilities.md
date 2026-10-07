# 模型能力与图片输入修复

适用版本：v0.5.19。返回 [教程](tutorial.md#models) 或 [项目首页](../README.md)。

0.5.18 及之前生成的 Codex 目录按接口协议决定输入模态：OpenAI 协议一律只写 text，Claude 协议一律写 text + image。这会让有视觉能力的 GPT 在请求发送前就被 Codex 拒绝。0.5.19 改为按每个模型的实际能力写入。

## 如何使用

1. 点击供应商行的「管理模型」，等待自动同步。
2. 每个模型下方都有「图片输入」；Codex 还显示「并行工具调用」。官方型号通常自动匹配，未知型号显示「待确认」。图片输入指理解图片，不是图片生成。
3. 需要覆盖时直接勾选或取消；出现「手动」标记。点击「恢复自动」清除该模型的所有手动能力覆盖。
4. 点击「保存并应用」，按提示重启 Codex，并用新会话测试。未使用供应商只保存，下次使用时生效。

![模型能力配置](screenshots/model-capabilities-0.5.19.png)

## 自动识别的顺序

| 优先级 | 来源 | 说明 |
| --- | --- | --- |
| 1 | 用户手动覆盖 | 支持明确开启或关闭；刷新同一个模型不会覆盖 |
| 2 | 上游明确元数据 | 识别 input_modalities、modalities.input、OpenRouter architecture.input_modalities、Anthropic capabilities.image_input.supported，以及明确的 image / parallel 布尔字段；false 也有效 |
| 3 | 经核实的内置资料 | 当前包含 121 个 ID / 官方别名，前后端共用 [同一份目录](../src/content/model-capabilities.json)，核实日期 2026-10-07 |
| 4 | 无法识别 | 不凭协议、任意名称或未来型号猜测，显示待确认，允许手动配置 |

匹配仅使用明确收录的型号及官方快照，允许 openai/、anthropic/、google/、deepseek/ 命名空间。不把私有映射、任意自定义后缀或未发布型号自动认成官方能力。同步只使用供应商当前返回的模型，内置资料不会向可用列表添加模型。

当前资料覆盖 GPT-6 Astra、GPT-6.1 Sol、GPT-6 Sol / Luna、GPT-5.6 各档、GPT-5.5 / 5.4、GPT-4o / 4.1、多个历史 GPT / o 系列以及 Claude Fable 5.1、Opus / Sonnet 5.5、4.5 / 4.6 等。也收录已核实的 Gemini 和 DeepSeek Flash。保留 GPT-3.5、原始 GPT-4、o3-mini、o1-mini 等纯文本例外；不会声称所有 GPT 都支持图片。

未知能力默认关闭不是已证实不支持。供应商明确返回仅文本时优先采用该结果；确认网关实际支持后可手动覆盖。未核实的并行工具能力同样可手动设定。内置资料随应用版本维护，未来模型可以通过上游元数据或手动设置使用，无需等待新增型号发布。

## 升级与兼容

启动时只修复仍与上次应用快照相符的受管模型目录，用原有事务、备份和配置修订机制落盘；不改 API Key、当前模型、上下文或其他目录元数据。真实写入会触发 Codex 重启提示，无变化不反复提示。未使用供应商在下次应用时采用新规则。

存在外部修改、损坏文件或冲突时跳过自动修复，保留原有冲突处理；不会强制接管文件。Fast、模型保存和思考强度修复都按同一能力规则生成或修复目录，避免旧 text-only 标记回来。恢复原配置继续使用最初的受管快照。

Claude 原生客户端的菜单和输入由自身及上游管理。图片能力元数据在供应商间、客户端间复用；OpenAI → Claude 的本地转换服务发布能力信息并尊重明确关闭的图片能力。两方向转换保留图片内容，但不能使真实纯文本模型获得视觉能力。

## 官方依据

每条目录记录保存其官方来源：

- [OpenAI 模型目录](https://developers.openai.com/api/docs/models/all)、各型号页面的 Input modalities / Snapshots，以及 [工具调用说明](https://developers.openai.com/api/docs/guides/function-calling)。
- [Codex 官方模型目录](https://github.com/openai/codex/blob/main/codex-rs/models-manager/models.json)，包含 input_modalities 和 supports_parallel_tool_calls。
- [Claude 模型比较](https://platform.claude.com/docs/en/models/overview)、各型号 Overview 中的 Text and images → text，以及 [并行工具说明](https://platform.claude.com/docs/en/agents-and-tools/tool-use/parallel-tool-use)。
- [Gemini 图片理解](https://ai.google.dev/gemini-api/docs/image-understanding)。
- [DeepSeek Vision](https://api-docs.deepseek.com/guides/vision/)。

验证结果与限制见 [QA 记录](qa-inventory.md)。Codex 验证使用真实独立 app-server 和本地 mock 上游，包含图片请求；不使用用户真实密钥，不产生付费调用，也不声称测试过每家真实中转站。
