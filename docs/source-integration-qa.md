# 源码整合验证记录

验证日期：2026-11-08。当前为开发分支，不是发布验收通过记录。

## 本次供应商目录调整

- 删除商业合作伙伴的远程 feed、定时刷新、GUI／CLI／TUI 推荐、提醒与统计上报；旧缓存不能恢复列表。删除 18 个原有中转内置预设，中转仅注册蝶祈云。已保存的供应商配置和自定义入口不删除。
- 官网为 `https://www.dieqiyun.top`，API 根地址为 `https://cfapi.dieqiyun.top`。Chat／Responses 使用 `/v1` base，Messages 使用根地址。无凭据请求对应注册路由返回 API_KEY_REQUIRED，不存在的路径返回 404；未用真实 Key 验证模型推理。
- 使用官网的 32×32 PNG 图标并内嵌到本地页面。来源与摘要见 `docs/dieqiyun-brand.md`。
- `TestUniSwitchProviderDirectory` 在修改前实际失败（仍列出 18 个旧中转），修改后通过。`TestUniSwitchNoPartnerTelemetry` 验证旧统计缓存不再上报。相关 `TestUniSwitch*`、stats 包现有相关测试、受影响包的 Go vet、前端构建和三项源码整合检查通过。
- 编译后管理 API 验证：只有蝶祈云中转，合作伙伴统计路由返回 404，图标本地返回 200／PNG；未接入的假客户端配置未变化。
- `node scripts/qa-source-directory.mjs`：Chromium 与 WebKit，简体中文、繁体中文、英语、日语、德语共 10 组检查通过。覆盖启动后立即添加、默认地址、官网链接、图标解码、取消不保存、搜索已移除中转和自定义入口。无页面脚本异常；1120×780 与 760×600 目录布局无横向溢出。
- 检查发现本地顶部“添加供应商”可早于列表加载完成并触发空对象错误，已改为等待首次列表加载；上述测试会主动延迟列表响应覆盖此情况。
- 本次没有重跑全套来源测试。部分旧测试仍依赖已删除的内置中转预设，后续全量验收前需改用独立测试夹具；不将本次定向检查当作全套通过，也未进行三平台发布。

## 桌面验收清单

| 范围 | 功能检查 | 视觉检查与证据 |
| --- | --- | --- |
| 本地源码入口 | 新编译桌面程序启动本仓库 Go 核心，无外部安装依赖 | 启动窗口尺寸、位置、标题；无 iframe |
| 首页与导航 | 五个常用客户端、全部客户端、账号与 API、更多功能 | 灰色侧栏、白底列表、黑色按钮；1120×780 与最小窗口 |
| 供应商 | 合成地址与 Key 新建、获取模型、编辑保存、取消 | 卡片内容、模型资料、表单反馈 |
| 客户端模型 | 使用按钮、选择模型、写入隔离 Codex 配置、断开恢复 | 选择器不消失、状态更新 |
| 订阅与 API 共存 | 订阅入口与 API 供应商并列可达 | 登录入口清晰；不执行真实账号登录 |
| 全部功能入口 | 客户端注册表、插件页面、设置 | 页面可达、无本次脚本异常；不等同于业务全部实测 |
| 原生窗口 | 最大化、还原、隐藏、重新显示、退出 | 控件可用、无溢出 |
| 异常路径 | 空/无效输入；取消编辑；断开时保留原配置 | 不出现虚假成功，错误可读 |

所有操作在 QA 临时 HOME、假客户端和合成凭据中进行，不读取或修改日常客户端配置。截图及机器日志写入忽略的 `.qa/`。浏览器交互通过持久 Node REPL 和 Playwright 连接当前 QA 原生窗口，9223 端口串行使用。

## 自动检查

- 来源：固定提交 `30b7a17326ac1cad40ca349059c3348a2580938b`；7,411 个导入文件，7,386 个原始文件字节未改，17 个已登记适配，8 个合作伙伴功能源文件／专属测试与夹具已登记移除。源码校验与暂存区凭据审计通过。
- 前端构建、3 项源码整合检查、Go vet、`TestUniSwitch*`、Rust Clippy 通过。
- 编译核心进程检查通过：认证 Cookie、未认证/跨站拒绝、资源加载、分发安装器入口禁用、管理端口不暴露推理、版权声明、未连接客户端不改写。
- 原生操作已验证：合成供应商新建与自动获取模型，128k 资料展示，编辑保存，取消后确认放弃，选择模型写入隔离 Codex 配置，断开确认与恢复原设置（保留来源实现的供应商表以兼容旧会话）。注册表返回 81 项客户端记录。更多功能页面和设置可达，新增控件的局部 Axe 检查无违规；这不等于全部导入界面的完整无障碍验收。
- 原生窗口首次为 1120×780、位置 (1168, 307)，没有 iframe。已验证最大化/还原、隐藏/显示、最小化后经宿主入口恢复，以及正常退出码 0。最终编译版本在 760×600 的 WebView 视口模拟下无横向溢出，全部八个主导航按钮完整可见；这不是通过操作系统拖拽窗口得到的最小尺寸测试。截图为 `.qa/source-final-provider.png` 与 `.qa/source-final-minimum.png`。
- 最终桌面后台启动 smoke 通过：创建独立设置、禁用来源自更新、未连接 Codex 哨兵配置保持原样。它验证启动，不验证全部插件和推理协议。
- 首次原生应用模型的 QA 发现来源实现会越过临时 HOME 枚举并结束真实 Codex app-server，导致验证会话中断。已增加仅 QA 编译的进程边界和回归检查；补好边界后的配置应用、断开均未再出现结束宿主 Codex 的日志。不能把之前的测试环境称为完整隔离。
- 来源表单允许把 `not-a-url` 保存为地址，未暴露模型。没有为此另写第二套供应商规则；严格地址校验和错误反馈仍属待处理项。
- 完整 Go 测试未通过，日志在网关包中途停止，不能作为完成记录。已观察到 35 个失败事件（含子测试），涉及 7 个包；未修改来源源码的定向基线也复现了多项失败。部分失败受 Windows 路径、假进程与分配次数断言影响，尚需单独处理，不能跳过后算通过。
- 未验证真实订阅账号的授权/刷新，未验证所有客户端与第三方插件，未完成 Linux/macOS 原生运行与三平台发布验收。

## 复现

```sh
corepack pnpm build
corepack pnpm test
node scripts/build-go.mjs
node scripts/test-go.mjs --log .qa/go-integration.log -timeout=3m -run '^TestUniSwitch' ./internal/appdir ./internal/gui
node scripts/test-go.mjs --log .qa/go-vet.log vet
node scripts/qa-source-core.mjs
```

Windows 原生 QA 构建：`powershell -NoProfile -ExecutionPolicy Bypass -File scripts/with-msvc.ps1 -Action qa`。`node scripts/qa-source-core.mjs` 会构建 `.qa/source-qa.exe` 启动器。交互验证在 Node REPL 用 `spawn` 启动该启动器，参数为新编译桌面程序的绝对路径，再用 Playwright 连接 `http://127.0.0.1:9223`。当前 Node REPL 不允许导入 `node:process`，所以不在其中直接导入终端 QA 脚本。只关闭该调用创建的 QA 进程。

## 2026-11-08：移除第三方扩展

- 删除市场、安装更新、Bun 宿主、第三方认证/执行、页面桥接、JS 网关模块和客户端扩展 VM，以及相关 GUI/API/CLI 入口。存量数据保留；仅留拒绝执行的兼容类型层。
- 静态核心模块继续运行；旧 plugins.yml 不再控制其启停。仅依赖扩展的订阅不再提供。
- 已通过：前端构建、4 项来源/架构测试、所有 Go 包的测试编译及 TestUniSwitch 定向回归、全后端 go vet、来源 SHA 校验和临时索引源码审计（findings=[]）。未把定向回归称为完整运行全部历史测试。
- UI：Chromium/WebKit × en/zh/zh-TW/ja/de 共 10 组通过；检查扩展导航/接口失效、旧地址回退、蝶祈云添加、内置页面、设置及最小窗口宽度。补充交互检查确认仍提供 Claude、ChatGPT、ChatGPT API、Copilot、Gemini CLI 和 Antigravity 原生订阅入口。
- compiled-core 契约检查通过，隔离客户端配置未改变。独立开发版已重新构建和启动，网关为 127.0.0.1:18426。未使用真实账号进行登录或推理验证，未生成三平台发布产物。

## 2026-11-08：移除同步与备份

- 删除设置分页、搜索和导航、WebDAV/S3/GitHub 同步、手动配置包导入导出／恢复、自动备份、后台任务、管理 API、CLI 命令及专属脚本、翻译和测试。进一步删除登录凭据携带、资源库打包恢复、远端排序镜像、配额历史合并、用量传输写入及机器本地恢复覆盖等专用辅助代码。
- 保留客户端原子写入、配置安全副本、失败回滚、断开恢复和资源库本地应用。历史同步设置与备份文件不删除；旧用量文件仅保留读取兼容。移除边界见 [同步与备份功能移除](sync-backup-removal.md)。
- 新增两项移除守卫在修改前实际失败，修改后通过。旧 GUI/API 路径及脚本资源的 GET/POST 返回 404；合成历史设置与备份文件保持原样。
- 已通过前端构建、全部 Go 包测试编译、`TestUniSwitch` 及受影响配置写入／本地资源库定向回归。补充清理后再次通过全包编译及配额历史、路由分组排序、用量、设置、网关模式和快捷键等定向回归。上述记录不等于运行了全部历史 Go 测试。
- 编译核心 UI：Chromium/WebKit × en/zh/zh-TW/ja/de 共 10 组通过；旧 `tab=sync` 和保存的同步分页回到常规设置，已删除入口不可见且无旧接口请求，其他核心页面可达，没有新建自动备份或同步状态文件，隔离 Codex 哨兵配置保持原样。
- 现有设置分页 fixture（Chromium/WebKit × en/zh）和侧栏 fixture（两引擎、五语言及窄屏状态）通过。移除旧同步／扩展入口对应的过时预期；保留其余设置、导航和网关状态检查。截图和机器日志在忽略的 `.qa/` 目录中。
- 最终检查通过：6 项源码／架构测试、全后端 Go vet、编译核心启动契约、来源散列核对及临时 Git 索引源码审计。来源清单保留 7,411 个原始文件散列，其中 6,750 个未修改、222 个登记本地适配、439 个登记移除；审计 7,343 个待发布源文件，`findings=[]`，未改动真实暂存区。
- 本轮没有重启日常应用实例，没有使用真实订阅账号或 API Key，没有提交、推送或生成三平台发布产物。

主要复现入口：

```sh
corepack pnpm build
corepack pnpm test
node scripts/test-go.mjs --log .qa/sync-final-regressions.log -tags=nogui,uniswitchqa -timeout=5m -run '^TestUniSwitch' ./...
node scripts/test-go.mjs --log .qa/sync-vet-final.log vet ./...
node scripts/qa-source-sync-backup.mjs
node scripts/qa-source-core.mjs
node scripts/check-upstream-source.mjs
```

## 2026-11-08：从源码移除局域网共享

- 删除局域网共享开关、IP／CIDR 白名单、共享／公网地址发现与展示、代理信任、相关管理 API、CLI 参数和专属实现／测试。设置名称改为“网络”，浏览器访问只保留“关闭／本机”。原有网页来源 CORS 和界面风格保留。实现边界见 [局域网功能移除](lan-removal.md) 和 [本机网络契约](../backend/docs/subsystems/local-only.md)。
- 网关和管理页只接受回环监听地址；显式非本机地址在监听／部署前报错，旧的不安全环境地址回退本机。远程 peer、带转发头的请求及管理端口上的推理请求拒绝访问，已有命名密钥不能绕过。历史 `mode: "lan"` 不会启动网页监听，也不会自动删除用户旧文件。
- 保留本机客户端连接、命名密钥归因／限额／模型限制、本机浏览器扩展和 WSL 专用适配器。移除旧的全网卡／WSL 子网来源信任推断；WSL 只有经专用监听和密钥验证后才被视为本机。本机浏览器扩展使用独立推理端口，命名密钥在回环地址上继续保持身份、撤销和会话隔离。
- 两项源码移除守卫在旧实现上实际失败，删除后通过。最终前端构建和 8 项源码／架构测试通过；受影响的 access、agent、cli、gateway、gui、settings 定向回归通过，daemon 参与编译但该筛选下没有测试。全包 Go 测试曾完成编译；Windows 和 Linux 的全包 vet 通过。
- 编译核心 UI：Chromium／WebKit × en／zh／zh-TW／ja／de 共 10 组通过。使用真实核心的 API 创建并选中本机命名密钥、验证连接示例，验证旧共享接口返回 404、状态不再暴露共享字段、真实回环网关拒绝带转发头的有效密钥请求。隔离的 Codex 哨兵配置保持原样。
- 相关 UI fixture 共 64 项通过，覆盖本机浏览器访问、密钥创建／改名／轮换／停用／删除、用量过滤、本机连接地址及窄屏菜单。另行检查的连接示例（含本机 Git Bash 执行）、设置分页及 CORS 功能通过。CORS 删除测试先让读者滚动到按钮，再检查请求后的位置，并允许内容缩短的自然高度裁剪；已删除地址选择器的旧测试退休，保留密钥菜单的滚动交互。
- Linux／macOS 的 `nogui` Go 核心交叉构建通过；这不是原生三平台桌面或 WSL 的运行验收。macOS 的 `nogui` 交叉 vet 未通过，错误为 `traywindows_darwin_test.go: undefined: trayImagePNG`，不能把该检查记录为通过。
- 来源散列核对、临时 Git 索引的源码审计及 `git diff --check` 通过；保留所有原始文件散列，在本地适配／移除记录中登记变更。审计没有发现待发布源码中的私密／生成文件或未登记凭据，真实暂存区不参与本轮暂存。
- 完整历史 Go 运行 `-timeout=4m ./...` 曾未通过，包括 provider／agent／gateway／gui 等失败和超时。本轮修正了其中直接依赖已退役共享语义的测试，未重新宣称完整历史套件通过。翻译检查仍报告 21 处缺键和 4 处跨模块用词；新增／修改的本机网络提示在五语言中齐备，重复键和模块语言一致性检查通过。额外的 Gateway 状态卡 fixture 曾报告 Chromium 字体中心偏差约 0.59px，尚未通过该严格像素断言；没有为此改变现有视觉样式。
- 所有运行均使用 `.qa` 隔离数据／假凭据，只结束本次 QA 自己创建的进程。没有改动或重启真实客户端／日常应用，没有提交、推送或发布。机器日志与截图位于忽略的 `.qa/`；主要日志包括 `lan-local-final.log`、`lan-ui-final.log`、`local-only-ui-checks.json`、`lan-full-tests.log` 和 `lan-ui-nearby.log`。

主要复现入口：

```sh
corepack pnpm build
corepack pnpm test
node scripts/qa-source-local-only.mjs
node scripts/test-go.mjs --log .qa/local-only.log -tags=nogui,uniswitchqa -timeout=3m -run 'Test(Local|GatewayAddr|Legacy|NetworkGateway|CallbackToken|Caller|ConcurrentCaller|ManagedLocal|Browser|OwnBrowser|CodexCatalogForA|CodexResetNeedsA|KeyNote|EveryRoute|FolderVarsAreListed|CodexFailoverIgnores|BootSaysWhere|AppPage|WebOnePort|WebSamePort|ReadOnlyKey|WSLAgentAddressNotAnswering)' ./internal/access ./internal/agent ./internal/cli ./internal/daemon ./internal/gateway ./internal/gui ./internal/settings
node scripts/test-go.mjs --log .qa/local-only-vet.log vet ./...
node scripts/check-upstream-source.mjs
```

## 2026-11-08：从源码移除请求脱敏

- 删除本地识别模型、个人信息检测、脱敏词和自定义规则、屏蔽／警告／阻止动作、可还原占位符、替代模型路由、供应商绕过选项、脱敏密钥／记录／耗时／通知及相关 GUI、CLI、API、资源和专属测试。移除边界见 [请求脱敏移除说明](../backend/docs/subsystems/masking-removed.md)。
- 旧 `redact*`、`maskAlert` 和供应商 `unredacted` 配置不再影响请求，不再由设置接口返回或写回。请求正文按正常协议转发；不会主动删除用户数据目录中的历史文件。用量旧格式中的耗时槽仅保留读取兼容，避免其他计数错位。
- 日志、诊断副本和请求存档的固定凭据清理由 `internal/logsafe` 保留，API Key／登录 Token 不应出现在这些副本中；它不修改发往供应商或客户端的正文。正常 `redacted_thinking`、签名、opaque／encrypted reasoning 和图片编辑 mask 保留。统计分享、账号发现和手动截图账号打码仍是独立功能。
- 已通过前端构建、Windows Go 核心构建、全部 Go 包测试编译、10 项源码／架构检查、全后端 Go vet、受影响网关／GUI／日志凭据清理／设置／CLI／中间件／用量／卸载定向回归。新增旧设置回归在删除前实际被 block 规则阻止，删除后确认原文到达合成供应商，退休接口和资源返回 404。
- 补充协议定向回归通过，覆盖流式签名、隐藏／加密推理、工具响应和 previous-response 续聊。没有把全包编译或定向运行当作完整历史 Go 测试通过；历史设置一致性检查中的 `groupSink`／`groupFirstToken`／`groupPatience` 前端保留字段差异仍需单独处理。
- 编译核心 UI：Chromium／WebKit × en／zh／zh-TW／ja／de 共 10 组通过，验证无脱敏控件和退休接口请求、旧设置不生效、其他隐私开关仍能保存、手动账号显示独立工作。账号发现与分享统计 UI fixture 共 36 项通过。隔离 Codex 哨兵配置保持原样；截图和机器日志在忽略的 `.qa/` 目录。
- 来源散列检查与临时 Git 索引源码审计通过：7,411 个原始文件散列保持不变，6,259 个文件未修改，470 个登记适配、682 个登记移除；7,122 个待发布源文件审计 `findings=[]`，57 个合成凭据夹具按精确散列登记。真实 Git 暂存区散列前后相同。
- 本轮没有重启或停止日常应用／真实客户端，没有使用真实订阅凭据，没有提交、推送或发布。

主要复现入口：

```sh
corepack pnpm build
corepack pnpm test
node scripts/build-go.mjs --qa
node scripts/test-go.mjs --log .qa/masking-compile-all.log -tags=nogui,uniswitchqa -timeout=3m -run '^$' ./...
node scripts/test-go.mjs --log .qa/masking-vet.log vet ./...
node scripts/qa-source-no-masking.mjs
node --test --test-concurrency=1 backend/internal/gui/tests/privacy-usage-stats.test.cjs backend/internal/gui/tests/account-discovery.test.cjs
node scripts/check-upstream-source.mjs
```
