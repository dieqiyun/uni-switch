# uni-switch

在本机管理 AI 客户端、订阅账号与自定义 API，通过本地网关切换供应商和模型。

## 开始使用

1. 在客户端页面选择需要配置的客户端。
2. 添加订阅账号，或填写自定义 API 地址与 API Key，然后获取并选择模型。
3. 应用配置后，根据客户端提示重新打开客户端；需要时新建对话。
4. 在设置中的「网关」查看本地地址、连接状态和请求记录。

从 [GitHub Releases](https://github.com/dieqiyun/uni-switch/releases) 下载已发布的程序。当前源码的发布验收状态见 [0.5.29 验证记录](docs/releases/0.5.29-qa.md)。

## Codex / ChatGPT 桌面默认值

连接和同步受管配置时，uni-switch 强制设置简体中文，并将其 Codex 供应商的 `requires_openai_auth` 设为 `true`。旧的其他语言或 `false` 会被替换。这需要可用的 ChatGPT 登录；账号套餐、额度、工作区权限与官方工具的服务端限制继续由官方决定。详见 [配置说明](backend/docs/subsystems/agent-wiring.md#codex-desktop-defaults)。

## 开发

业务源码位于 `backend/`，编译后的 Go 核心嵌入 Tauri 桌面程序。用户使用发行包时无需安装 Go。

```sh
corepack pnpm install --frozen-lockfile
corepack pnpm build
corepack pnpm test
node scripts/build-go.mjs
corepack pnpm dev
```

构建需要 Node 22+、Go 1.26.8+、Rust 及目标系统的桌面开发依赖。完整流程见 [源码整合说明](docs/source-integration.md) 和 [GitHub 发布说明](docs/github-release.md)。

## 当前功能边界

保留客户端配置、订阅与自定义 API、本机网关、普通供应商路由、密钥和账号轮换、备用模型、请求诊断、会话与资源库。已移除第三方扩展市场和执行宿主、跨设备同步和配置包备份、局域网共享与远程管理、请求脱敏规则、外部观测导出、用量报表和路由组。旧数据不因此自动删除。

## 许可与来源

本项目以 AGPL-3.0-only 发布。导入的 Magpie 源码固定在提交 `30b7a17326ac1cad40ca349059c3348a2580938b`，其 MIT 许可与版权声明保留。原始散列、本地适配与移除记录见 [来源清单](docs/upstream-source.json)，第三方声明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)。
