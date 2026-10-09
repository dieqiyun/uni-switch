# 多客户端配置与文件编辑

左侧新增 ZCode、DSH（DeepSeek Harness）、WorkBuddy。一键配置使用现有供应商记录，不需要重复保存密钥。

| 客户端 | 默认文件 | 原生协议 |
| --- | --- | --- |
| Codex | $CODEX_HOME/config.toml、auth.json、uni-switch-models.json | 沿用现有配置入口与协议转换 |
| Claude Code CLI | $CLAUDE_CONFIG_DIR/settings.json | 沿用现有入口 |
| Claude Code 桌面端 | 当前部署配置与 configLibrary 下应用的 UUID 档案 | 沿用现有入口 |
| ZCode | ~/.zcode/v2/provider_config.json | Messages、Chat Completions、Responses |
| DSH | $DSH_HOME/cordis.patch.yml、.credentials.yaml | Messages、Chat Completions、Responses |
| WorkBuddy | ~/.workbuddy/models.json | Chat Completions |

ZCode 遵循 ZCODE_DATA_BASE_DIR 和 ZCODE_PERSONAL_PROVIDER_CONFIG_FILE。DSH_HOME 缺省为 ~/.dsh。现有 Codex/Claude 的路径发现机制保留。所有页面都显示实际文件路径，可选择自定义配置目录。WorkBuddy 的官方文档同时提到旧 ~/.codebuddy/models.json 的兼容性；如客户端仍使用旧文件，选择对应目录。项目级配置、启动环境变量和 DSH profile 设置可能影响实际选择。

## 原生写入

- ZCode 使用 schemaVersion 1 个人供应商规则，管理 uni-switch 提供方，保留其他提供方、排序及模型规则，设置 defaultModelSelection。不修改内置供应商配置。
- DSH 在保留原文本的管理区块中插入 uni-switch-llm，多实例使用独立供应商路由；更新 agent-default-model，凭据引用写入 version 1 的 refs。旧的平铺凭据格式不会自动迁移。管理区块外的注释及 !!js 标记不会执行或重写。DSH 的 Anthropic SDK 地址去除终端 /v1，避免重复路径；OpenAI 地址保留 /v1。
- WorkBuddy 合并 models 列表和已存在的非空 availableModels，保留其他模型。采用完整 /chat/completions 地址，能力字段仅写入已有依据的值。客户端默认模型需要在自身选择器中选用。
- 每个客户端可选择原生协议；显式标记不支持的模型以及跨协议且没有支持依据的模型在写入前报错。不根据模型名称猜测协议。

## 文件编辑

路径由后端允许列表决定，不接受任意 IPC 文件路径。页面先展示路径列表，只有主动选中文件才读取内容，默认只读；点击「开始编辑」后可保存。内容不进入 localStorage 或概览响应。JSON 顶层须为对象，TOML/YAML 使用解析器校验，解析错误不回显文件片段或密钥。单文件上限 4 MiB。

保存校验绑定目录、文件标识和内容摘要。发现外部变化拒绝写入并保留编辑草稿。符号链接或 Windows 重解析点拒绝访问。写入私有备份后进入事务日志，原子替换并回读；多文件中断会在下次打开时恢复，恢复不覆盖外部新内容。备份包含配置与凭据，保存在应用数据目录 backups/client-config-UUID.json。

手动保存不会把文件中的密钥反向采纳为供应商。已接管客户端标记手动修改，自动重用供应商不得静默覆盖；用户可查看当前文件后确认重新应用。已有 Codex/Claude 的手动修改也不会被启动时的模型迁移改写。

真正写入后提示完全退出并重新打开客户端、新开对话；WorkBuddy 还提示选择自定义模型。无变化、只查看、取消、失败不提示。本功能的通用提示不会自动停止进程；原有 Codex/Claude 一键重启行为继续由原入口提供。

新客户端恢复首次接管前的完整文件，所以接管后新增的手工设置可能被恢复替换，界面会明确确认。若有未确认的外部修改则拒绝自动恢复。

## 验证

- Rust 的 store/client_configs/tests.rs 验证三种官方格式、认证、中文、保留字段、幂等、格式拒绝、目录绑定、冲突确认、备份与事务中断恢复。
- ClientConfigEditor.test.tsx 验证主动读取、只读/编辑、保存、草稿保留、放弃确认、无变化不提示与三客户端入口。
- node scripts/qa-client-config.mjs 在 .qa 下创建独立配置、应用数据库和 WebView，用合成密钥检查真实桌面界面。脚本仅停止它创建的进程，CDP 9223 与其他原生验收串行。

## 官方格式参考

- ZCode: https://github.com/zai-org/ZCode （packages/provider-node、packages/provider/src/config）。
- DSH: https://github.com/deepseek-ai/deepseek-harness （llm-pi-ai、credentials-local、agent-default-model）。
- WorkBuddy: https://www.workbuddy.cn/docs/workbuddy/From-Beginner-to-Expert-Guide/Function-Description/Model 。
- 兼容 models.json 字段: https://www.codebuddy.cn/docs/cli/models 。
