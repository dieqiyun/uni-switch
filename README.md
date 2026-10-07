# uni-switch

简洁的 Codex / Claude Code API 配置工具。填写 **API 地址、API Key**，点击 **添加并使用**；日常切换只需一次点击。

**当前版本：0.5.18 · Windows x64 · AGPL-3.0-only**

[下载安装包](https://github.com/dieqiyun/uni-switch/releases/latest) · [使用教程](docs/tutorial.md) · [供应商赞助](docs/sponsors.md) · [更新记录](CHANGELOG.md) · [问题反馈](https://github.com/dieqiyun/uni-switch/issues) · [开源许可](LICENSE)

![uni-switch 模型配置](docs/screenshots/model-catalog-pruned-local.png)

## 供应商赞助名单

感谢 **蝶祈云 API** 对 uni-switch 的支持。目前供应商赞助名单仅列出蝶祈云。

<table>
  <tr>
    <td width="150" align="center">
      <a href="https://www.dieqiyun.top/"><img src="src/assets/dieqiyun-logo.png" width="88" alt="蝶祈云 API 紫色 Logo" /><br /><strong>蝶祈云 API</strong></a>
    </td>
    <td>
      蝶祈云为个人开发者、科研探索者及团队提供 AI API 渠道接入，官网展示 GPT、Claude、Gemini 与国内模型生态。其渠道建设涵盖 <strong>GPT 真官 Key 科研探索、国外机构合作定制、高质量渠道聚合</strong>，让不同模型与渠道在同一入口更方便地选择。<br /><br />
      除个人使用外，也面向项目开发与长期使用需求提供机构定制渠道；官网提供用户社区和联系支持入口，并支持开具发票，团队采购或报销可向支持确认开票资料与办理方式。接入时，从控制台复制对应渠道的 <strong>API 地址与 API Key</strong>，在 uni-switch 点击「添加并使用」，即可继续自动检测协议、同步模型和应用客户端配置。<br /><br />
      <a href="https://www.dieqiyun.top/">访问蝶祈云官网 ↗</a> · <a href="docs/sponsors.md">详细介绍与接入说明</a> · <a href="docs/tutorial.md#getting-started">首次配置教程</a>
    </td>
  </tr>
</table>

介绍依据蝶祈云官网公开内容整理；模型权限、API 接入地址、价格和服务安排以实际控制台及服务方说明为准。

## 功能

- 管理 Codex 桌面端 / CLI、Claude Code 桌面端及 Claude CLI 的 API 配置，同一供应商可在客户端之间复用。
- 自动检测协议、认证方式、上游模型和余额，无需选择站点类型或手填模型 ID。
- 默认启用全部适用模型。每个模型可设置上下文长度，默认 256k。
- 同步成功后移除上游已下架的模型，并为失效默认模型选择有效替代项；同步失败保留原列表。
- 双向协议转换：Codex 使用 Claude Messages 模型；Claude Code 使用 OpenAI 模型。
- 供应商列表直接切换默认模型、上下文、Fast 加速模式和协议转换；完整模型配置在弹窗中确认。
- Codex 配置写入时自动检查思考强度显示。实际修改后提供立即重启 / 稍后重启；Claude 两端检测到对应运行客户端时也会提示。
- 本地配置备份、外部修改冲突检测、恢复原配置；后台协议转换与可选 Windows 登录启动。
- 左下角版本入口检查 GitHub 最新正式版，发现新版时持续显示明确提醒、目标版本和「立即更新」入口。
- 软件内提供可离线查看的分类教程，与 GitHub 使用教程共用内容。

## 使用

详细操作见 [完整中文教程](docs/tutorial.md)。软件左下角「使用说明」可离线阅读相同教程。

1. 在左侧选择 Codex 或 Claude Code；Claude Code 可分别选择桌面端和 CLI。
2. 点击 **添加供应商**，填写 API 地址和 API Key，点击 **添加并使用**。
3. 按提示重启对应客户端，加载新配置及模型列表。
4. 后续切换点击供应商行的 **使用**。点击 **管理模型**调整启用列表、默认模型和上下文。

模型弹窗中的同步和选择属于草稿，取消不会写入。当前供应商确认保存后立即应用；未使用供应商在下次使用时应用。余额查询范围由上游接口决定，密钥额度与账户余额会分别标注。

配置目录在 **设置** 中查看和修改，通常保留默认值即可。关闭窗口后应用驻留托盘，协议转换继续运行；升级前从托盘退出旧版。

## 下载与更新

从 [GitHub Releases](https://github.com/dieqiyun/uni-switch/releases/latest) 下载：

| 文件 | 用途 |
| --- | --- |
| `uni-switch_0.5.18_x64-setup.exe` | Windows 安装包，推荐普通用户使用 |
| `uni-switch_0.5.18_x64-portable.zip` | 便携程序、说明和许可证，解压后运行 |
| `uni-switch_0.5.18_x64.exe` | 独立程序，需要系统已有 WebView2 |
| `uni-switch_0.5.18_source.zip` | 与本版本程序对应的完整源码和构建文件 |
| `SHA256SUMS.txt` | 发布附件校验值 |

软件检测更新后打开发布页供用户下载，目前不自动替换正在运行的程序。

## 从源码构建

架构：**Tauri 2 + Rust + React + TypeScript + Vite + SQLite**，技术选型参考 [cc-switch](https://github.com/farion1231/cc-switch)。

Windows 构建环境：Node.js 20 或更新版本、Corepack / pnpm 10.12.3、Rust 1.89 或更新版本、Visual Studio C++ Build Tools（含 Windows SDK）及 WebView2。

```powershell
corepack enable
corepack pnpm install --frozen-lockfile
corepack pnpm typecheck
corepack pnpm test
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/with-msvc.ps1 -Action test
powershell -NoProfile -ExecutionPolicy Bypass -File scripts/with-msvc.ps1 -Action build
```

安装包输出到 `src-tauri/target/release/bundle/nsis/`，主程序在 `src-tauri/target/release/uni-switch.exe`。预编译图标、模型基础指令和资源已包含在仓库中，常规构建无需图像生成服务或私有凭据。

开发桌面应用使用 `corepack pnpm dev`；仅预览界面使用 `corepack pnpm dev:web`。浏览器预览不写入真实客户端配置。配置实现、协议兼容范围及隔离测试说明见 [docs](docs/qa-inventory.md)。正式发布流程见 [发布说明](docs/github-release.md)。

## 数据与兼容性

供应商密钥和客户端配置保存在本机；GitHub 更新检查不携带供应商认证。源码仓库与发布附件排除真实 API Key、数据库、客户端配置、测试运行数据和构建缓存。

协议探测依据模型接口响应，不发送计费推理请求；上游模型名称或菜单中的思考档位不能保证模型实际能力。Fast 请求优先服务档位，需要上游支持；Claude 转换暂不支持 Fast。原生 QA 使用隔离目录和模拟供应商，真实供应商的权限、额度与兼容性仍取决于其部署。

## 开源许可

Copyright (C) 2026 dieqiyun and uni-switch contributors.

本项目自 **v0.5.17** 起以 **GNU Affero General Public License v3.0 only（AGPL-3.0-only）** 发布。完整条款以 [LICENSE](LICENSE) 为准：

- 分发本项目或修改后的版本时，按 AGPL 提供对应源码并保留许可及版权声明。
- 修改后的版本通过网络与用户交互时，也应按 AGPL 第 13 条向这些用户提供对应源码获取入口。
- AGPL 允许商业使用；纯私人使用或修改本身不要求向公众发布源码。独立程序不会仅因与本工具通信就自动受同一许可约束。
- 本软件按现状提供，不提供任何保证。

第三方组件保留各自许可。Codex 通用基础指令采用 Apache-2.0；OpenAI、Anthropic 和蝶祈云品牌标志保留各自权利，不纳入程序的 AGPL 授权。详见 [第三方声明](THIRD_PARTY_NOTICES.md) 和 [NOTICE](NOTICE)。旧版已授予的许可不因本次更换许可而撤销。
