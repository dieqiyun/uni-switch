# GitHub 三平台开源发布

从 v0.5.20 起，按维护者新要求恢复完整源码公开，采用 AGPL-3.0-only。v0.5.19 的仅程序发布记录保留，不改动历史版本。

## 构建与验证

更新 package.json、Cargo.toml、Cargo.lock、tauri.conf.json 和 CHANGELOG 的版本。教程更新后运行 docs:generate / docs:check。公开文件经 audit-source.py 审核，禁止带入 API Key、真实客户端配置、数据库、缓存或构建目录。

完整源码提交并推送 GitHub main 后，运行 `.github/workflows/build-desktop.yml`（手动触发或新版本标签）。工作流使用 Windows、Ubuntu 22.04、macOS 15 Intel 原生 runner（`macos-15-intel`）；macOS 编译 aarch64 与 x86_64 后合为 universal，隔离启动在 Intel 主机执行。固定 action 提交、冻结 pnpm / Cargo 锁文件；工作流只有只读仓库权限，不自行公开 Release。

每个平台执行 Rust 测试、Clippy、正式构建和隔离启动检查；Linux 还执行前端测试。macOS 核对 lipo 双架构与 codesign 签名，Linux 在 Xvfb 桌面环境验证启动，Windows 验证独立数据目录启动。不会测试或改变用户真实客户端。

## 产物与发布

下载成功工作流的三组产物，运行 `scripts/prepare-multiplatform-release.py --artifacts 产物目录`。该脚本核对各 build-manifest 的版本、提交、文件哈希，要求与当前源码提交一致；生成精确对应的 source.zip、完整教程、SHA256SUMS 及本地发布清单。

发布附件共十项：Windows 安装/独立/便携三项，Linux DEB / AppImage 两项，macOS DMG / app.tar.gz 两项，对应 source.zip、README-zh-CN.md 和 SHA256SUMS.txt。

使用 `scripts/publish-multiplatform-release.py` 发布；源码必须在公开 main，标签必须指向构建提交。先建草稿，固定清单上传，再下载所有附件校验 SHA256，最终公开并设 Latest。不覆盖公开同版本；失败可继续同版本草稿。草稿阶段从发布列表定位唯一的标签与 Release ID，再按 ID 查询，避免按标签读取尚未公开的草稿时返回 404；公开后再核对 Latest 与全部附件。

macOS 使用 ad-hoc 签名，不具备 Apple Developer ID 公证；发布说明应明确首次打开限制。Windows 程序未增加购买的代码签名证书。macOS / Linux 自动重启与系统登录启动尚未实现，配置写入后手动重启。

## 使用者

按系统下载对应程序，核对 SHA256；源码可从仓库、标签或 source.zip 获取。软件内「使用说明」包含离线教程，左下角版本入口检测最新正式版。macOS / Linux 浏览器使用原生 open / xdg-open，URL 仍受固定目的地校验保护。

## 远程更新兼容

新客户端每次允许选择 GitHub 手动下载或远程下载，不后台安装。远程路径要求 GitHub 附件提供真实大小，并能从附件 sha256 digest 或同版本 SHA256SUMS.txt 获得精确文件的校验值；保持上面的三平台版本化命名和校验清单即可，无需另设升级服务器。大小、哈希或包格式不符时拒绝安装；安装前重新校验缓存。没有合格安装包的旧版本继续提供手动下载，不改动历史发布。

Windows 确认后启动安装向导并退出应用；macOS 打开 DMG，由用户退出旧版后拖入 Applications；Linux 请求系统安装 DEB，或提示替换 AppImage。平台权限与签名仍由系统处理，不将 SHA256 校验称为代码签名或公证。详见[远程更新说明](remote-update.md)。
