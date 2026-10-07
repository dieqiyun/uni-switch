# uni-switch

管理 Codex 与 Claude Code 桌面端、CLI 的 API 配置。填写 API 地址和 API Key 后自动同步模型与查询余额，日常切换只需点击使用。

## 下载

请从 [GitHub Releases](https://github.com/dieqiyun/uni-switch/releases/latest) 下载 Windows x64 正式版本。

- 安装版：名称以 `x64-setup.exe` 结尾。
- 便携版：名称以 `x64-portable.zip` 结尾，解压后运行 `uni-switch.exe`。
- 独立 EXE：适用于已经安装 WebView2 的 Windows。
- `SHA256SUMS.txt` 提供程序和说明文件校验值。

升级前从系统托盘退出旧版，再安装或启动新版。关闭窗口会保留托盘及协议转换服务。

## 使用

1. 选择 Codex 或 Claude Code；Claude 可分别选择桌面端和 CLI。
2. 添加供应商，只填写 API 地址和 API Key，然后点击添加并使用。
3. 模型默认勾选上游适用模型，管理模型可调整默认模型、启用列表和上下文。
4. 设置中可检测软件更新；发现新版后前往 GitHub 下载。

本仓库用于分发已编译程序和使用说明，不包含应用源码。GitHub 自动生成的 Source code 压缩包仅包含本发布仓库的说明文件，请下载 Release 附件中的程序。
