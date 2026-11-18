# uni-switch

在一个桌面应用里管理 AI 客户端、供应商和 API Key，并通过本机网关切换模型。支持同一供应商的多个命名 API Key 分组，每个分组独立维护模型列表。

客户端模型显示为「模型名 · 分组名 · 供应商名」，选中后使用对应分组的 Key。Codex、Claude Code 等客户端的连接与模型设置可以在客户端页面管理。

## 使用

1. 从 [GitHub Releases](https://github.com/dieqiyun/uni-switch/releases/latest) 下载对应系统的安装包。
2. 添加供应商，填写 API 地址、API Key 和分组名称，获取并选择可用模型。
3. 编辑供应商，使用「添加 API Key 分组」添加其他分组。
4. 在客户端页面连接软件并选择模型，重新打开客户端以加载更新后的模型列表。

详见 [中文使用指南](docs/tutorial.md) 和 [API Key 分组说明](docs/api-key-groups.md)。

新手可阅读带完整截图与箭头的 [蝶祈云配置教程（0.5.29）](docs/tutorials/dieqiyun-0.5.29/README.md)，包含 GPT、生图、Claude Key 分组和客户端接入。

网关只接受本机请求；专用 WSL 适配器保留独立的连接与 Key 契约。

## 从源码构建

需要 Node.js 22+、Go 1.26.8+、Rust 和目标系统的 Tauri 构建依赖。完整 Go 后端位于 `backend/`，桌面程序构建时会编译并嵌入核心程序。

```sh
corepack pnpm install --frozen-lockfile
corepack pnpm build
corepack pnpm test
node scripts/test-go.mjs -timeout=20m -p=2
node scripts/build-go.mjs
corepack pnpm dev
```

原生发行包通过 [三平台构建工作流](.github/workflows/build-desktop.yml) 生成。构建、验证与源码来源见 [源码整合说明](docs/source-integration.md)。

## 功能边界

保留本机网关、普通供应商路由、API Key／账号轮换、备用模型、会话和本地资料库。路由组、自动分组、组模板、第三方扩展运行时、跨机同步、远程共享、外部观测导出及用量报表已移除。API Key 分组用于区分同一供应商的凭据和模型列表。

侧栏版本号与更新入口合并，有新版本时才提示；折叠项再次点击即可收起。详见 [子系统与功能边界](backend/docs/subsystems/README.md)。

## 许可证与源码

本项目以 AGPL-3.0-only 发布。整合的 Magpie 上游源码基于提交 `30b7a17326ac1cad40ca349059c3348a2580938b`，其 MIT 许可证和版权声明保留在 `backend/`。

每个发行版本提供 Windows、Linux、macOS 程序、精确对应的源码归档、中文使用指南和 SHA256 校验和。源码清单见 [来源记录](docs/upstream-source.json)，第三方声明见 [THIRD_PARTY_NOTICES.md](THIRD_PARTY_NOTICES.md)、[backend/LICENSE](backend/LICENSE) 和 [backend/THIRD_PARTY_NOTICES](backend/THIRD_PARTY_NOTICES)。
