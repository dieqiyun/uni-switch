# 黑白切换 Logo 与底部官网入口（本地待发布）

> 本文记录此前的本地预览过程；这些改动现已纳入 0.5.17 开源版本。发布内容与许可见 CHANGELOG.md 和 docs/github-release.md。
本次保持 0.5.16 本地预览版本，没有修改 GitHub 正式发布。

## Logo

根据用户的新要求，系列 Logo 仅作为简洁风格的参考，不沿用其云朵和蝴蝶造型。新版采用两条相向路径组成的抽象切换标志，黑色主体、白色留白，适配 uni-switch 的供应商切换和协议连接用途。

继续使用用户指定的 `https://cfimg.dieqiyun.top/v1` 生图接口与该服务提供的 `gpt-image-2.5` 模型，通过 imagegen 技能现有 CLI 的 `generate` 模式生成。此次没有上传旧 Logo 作为编辑目标，提示词明确禁止云朵、蝴蝶、紫色、渐变、阴影和额外文字。原始生成图经人工查看后整理为透明底素材。

- 生图提示词：`output/imagegen/uni-switch-logo-monochrome-prompt.txt`。
- 原始生成结果：`output/imagegen/uni-switch-logo-monochrome-generated.png`。
- 最终透明底主图：`output/imagegen/uni-switch-logo-monochrome-transparent.png`。
- 白底预览：`output/imagegen/uni-switch-logo-monochrome-preview.png`。
- 黑标白底程序图标主图：`output/imagegen/uni-switch-icon-monochrome.png`。
- 侧栏素材：`src/assets/uni-switch-logo.png`。
- 官网宣传使用原有蝶祈云紫色 Logo：`src/assets/dieqiyun-logo.png`。

`scripts/prepare-brand-assets.py` 按灰度覆盖提取黑色标志，去除白底和轻微背景噪点，保留透明边缘。侧栏使用透明底图案；程序图标增加白色底板，保证黑色图案在浅色和深色任务栏中均可见。`tauri icon` 生成安装包、EXE、窗口与托盘图标，同时更新浏览器预览 favicon。根据用户后续要求，Codex 与 Claude 保留原本的官方素材，Claude 使用彩色 Logo；蝶祈云宣传使用紫色 Logo。此处恢复已有素材，没有再次调用生图服务。

`build.rs` 显式跟踪 `icons/icon.ico` 与 `icons/icon.png`，保证修改图标后重新编译 Windows 资源。正式 EXE 的内嵌图标由独立命名的副本提取，验证黑色图案、白色底板和无彩色像素，避免构建缓存或系统图标缓存沿用旧素材。

生图认证仅用于本次命令的进程环境，没有存入源码、供应商数据库或程序包。素材准备脚本不使用网络和认证。

## 官网内容与布局

2026-10-07读取 [蝶祈云官网](https://www.dieqiyun.top/) 的首页与首页组件。官网明确列出 GPT、Claude、Gemini 模型生态，介绍“聚合全网优质渠道”和“支持开具发票”。底部入口据此采用：

> 蝶祈云 API　GPT · Claude · Gemini  
> GPT 真官 Key · 机构合作定制 · 优质渠道聚合 · 支持开具发票　访问官网 ↗

入口仍放在工作区底部，官网文案和跳转地址继续保留，并补充首页明确列出的 GPT 真官 Key 与机构合作定制渠道特点。uni-switch 自身标志、主按钮、开关、选中状态和弹窗保持黑白灰；官方 Claude 图标和蝶祈云 Logo 保留各自品牌颜色，宣传标题使用深紫色。

宣传内容在整个工作区底部水平居中。宽窗口使用左右相等的网格列，左列放本机/兼容服务状态，中间列放紫色 Logo、两行文案与官网入口。相同宽度的两侧列保证宣传不会因为左侧状态而偏向右方。1000px及以下改成一列，宣传和状态依次居中，长文案自然换行。没有新增嵌套卡片，官网入口始终可见。

整个宣传区可以点击，也可以用 Tab 和 Enter 操作。在桌面程序中通过 `open_service_website` 命令调用系统浏览器，地址固定为 `https://www.dieqiyun.top/`。不使用供应商密钥、配置地址或更新仓库地址。浏览器预览使用带 `noopener noreferrer` 的原生新窗口链接。打开期间避免重复调用，失败显示可复制的官网地址并允许重试。

## 验证入口

`ServicePromotion.test.tsx` 覆盖键盘打开、固定地址、重复请求抑制、失败提示与重试、浏览器原生链接。`scripts/qa-brand-refresh.mjs` 接入 `qa-model-dialog.mjs` 的真实桌面隔离流程，验证 uni-switch Logo 为黑白、Claude 和蝶祈云 Logo 保留彩色像素且无灰度过滤、Tauri IPC 记录固定官网目标、文件不变和打开失败恢复。1120/1001/1000/920/760/390px 下测量宣传中心与工作区中心相差不超过1px，状态和宣传不重叠、入口可见、无溢出，并执行Axe。协议回归复用居中检查，覆盖兼容服务长状态文案。

仅QA构建接受 `UNI_SWITCH_QA_SERVICE_OPEN_MARKER` 作为独立目录下的跳转记录；正式包没有该测试分支。

本次前端127项测试与 TypeScript/Vite、Windows/NSIS构建通过。模型、顶部、更新和品牌流程12组原生检查、21处Axe通过；协议转换回归6组原生检查、8处Axe通过，两组均无pageErrors。验证包括两方向实际HTTP协议转换、关闭恢复、重启提示、模型保存/取消，以及底部官网打开失败恢复。六种窗口宽度下的宣传居中与不重叠检查均通过。原生流程及无障碍检查结果见 `.qa/model-dialog/results.json` 和 `.qa/protocol-controls/results.json`。正式二进制核对不含QA官网记录或WebView调试端口标记。

实际窗口截图：`docs/screenshots/centered-service-local.png`；底部宣传截图：`docs/screenshots/centered-service-footer-local.png`；390px窄窗：`docs/screenshots/centered-service-narrow-local.png`。截图来自隔离测试数据，不代表实际供应商余额。

当前本地预览保存在`release/local-preview/centered-service-0.5.16/`。原生测试使用独立数据库与配置目录，没有改变用户真实客户端配置和正在运行的程序。

## 之前的草案

先前的紫色云蝶应用图案和 `release/local-preview/brand-promotion-0.5.16/` 保留为历史草案；主程序图案已替换为独立的黑白切换标志，官网宣传使用原有系列 Logo。

全灰度品牌预览保留在 `release/local-preview/monochrome-brand-0.5.16/`；用户后续明确要求恢复 Claude 彩色 Logo 和蝶祈云紫色 Logo，并将宣传居中。当前版据此修订，uni-switch 独立的黑白标志继续使用。
