# 后端源码整合与验证

业务源文件直接位于 `backend/internal`、`backend/plugins`，编译成本应用的 Go 核心。固定来源提交、MIT 许可及逐文件原始 SHA-256 记录在 [源码清单](upstream-source.json)；版权信息保留在 `backend/LICENSE` 和第三方声明中。

## 实际运行链路

桌面窗口 → 本应用编译的 Go 核心 → 本地管理 API / 推理网关 → 上游服务。

Go 核心随桌面程序内嵌，启动时校验并部署到本应用数据目录的 `runtime/<内容哈希>/`。客户端辅助命令引用该持久位置，退出后不会因为临时目录删除而失效。Rust 仅负责提取核心、窗口、托盘、文件夹选择和生命周期；原 SQLite Store、协议桥接、客户端投影和 Go RPC 池已经移除。

程序不寻找另一款应用的安装文件，不启动它的 CLI / web 子命令，不连接其远程后台，也不使用 iframe 承载其管理网页。管理页面、样式和脚本均来自本仓库编译的本地资源。第三方扩展的安装、市场、认证、执行宿主、页面及管理接口已经删除。

## 业务范围

以下能力直接使用导入源码中的实现，没有保留原项目的第二套业务算法：

- 请求转发、认证、路由与故障回退；Chat Completions、Responses、Messages、Gemini 的协议处理。
- SSE 分帧、事件增量、工具调用、思考参数、上下文和多轮会话。
- 供应商、多个 API Key、订阅账号、登录与刷新、模型目录和资料合并。
- 客户端发现、模型映射、配置写入和配置事务回滚。
- 图片与视频 API、原生厂商接口透传。
- 路由组、用量、会话、资源库和脱敏。

客户端配置适配与订阅账号服务是不同能力。内置 API Key、Claude、ChatGPT、Google、Copilot 等接入继续使用原实现；依赖已删除扩展的订阅及第三方客户端模块不再提供。已保存的数据保留，不自动删除或迁移。网关、路由、用量、会话、资源库等静态内置模块继续编译，旧 plugins.yml 不再控制这些页面。详见 [扩展移除记录](extensions-removal.md)。跨设备同步、整机设置导入导出和自动备份也已从代码中移除；客户端安全写入与失败回滚保留，详见 [同步与备份移除记录](sync-backup-removal.md)。

## 本应用适配边界

- 供应商目录：移除商业合作伙伴的远程拉取、展示与统计；移除原有中转预设，中转只保留蝶祈云。官方厂商、订阅、本地服务与自定义入口保留；不删除已保存的供应商。地址与本地图标来源见 [蝶祈云资料](dieqiyun-brand.md)。
- 独立数据目录：桌面应用 local data 路径，或显式 `UNI_SWITCH_DATA_DIR`。不复用另一款应用的配置目录。
- 新数据默认推理端口 `18425`；管理 API 使用随机回环端口及每次启动生成的 HTTP-only Cookie。管理端口不复用推理路由。
- 视觉层保持浅灰侧栏、白底供应商卡片和黑色主按钮；客户端导航调用原有选择器与 API。其他能力在「更多功能」中访问。
- 安装、自更新、卸载和全局命令安装属于本产品的发布体系。原来源产品的安装器入口被阻止，不能将本程序更新成另一产品。本分支通过完整安装包更新。
- 旧版 `uni-switch.db` 不删除、不自动改写；旧客户端配置也不在启动时接管。升级前应保留完整数据目录，重新连接需要用户明确操作。

## 检查命令

```sh
corepack pnpm install --frozen-lockfile
corepack pnpm build
corepack pnpm test
corepack pnpm docs:check
node scripts/build-go.mjs
node scripts/test-go.mjs -timeout=20m
node scripts/test-go.mjs vet
node scripts/qa-source-directory.mjs
node scripts/qa-source-extensions.mjs
node scripts/qa-source-sync-backup.mjs
cargo clippy --locked --manifest-path src-tauri/Cargo.toml --all-targets -- -D warnings
```

Windows 需 Go 1.26.8+、Node 22+、Rust 和 MSVC；`scripts/with-msvc.ps1 -Action qa` 构建隔离 UI 验证程序。所有客户端验证使用 `.qa` / 临时 HOME 与合成凭据，不操作日常应用配置。原生 UI 检查串行使用 9223 端口，Cargo 命令串行运行。

QA 窗口编译 Go 核心时额外使用 `uniswitchqa` 标签：在 `MAGPIE_TEST_SANDBOX` 临时环境中禁用 Codex 进程枚举及 app-server 重启。Windows 无法从其他进程读取 HOME，仅隔离目录和 PATH 不足以隔离进程。正式构建不包含此 QA 扩展；构建缓存按标签分别校验，不能把 QA 核心误当成正式构建缓存。

本轮已执行检查和未解决项目见 [验证记录](source-integration-qa.md)。

原来源测试保留；移除已删除的商业合作伙伴、第三方扩展及同步备份功能专属测试；混合测试文件保留其余用例，并补充本应用目录、统计和功能移除回归。跨平台编译不能替代实机账号登录、刷新、客户端重启及协议验证；不把页面入口存在或旧测试通过当成本次实机验证结果。全量测试和三平台产物检查未全部通过前不得发布。
