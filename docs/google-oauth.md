# Google 订阅的 OAuth 配置

uni-switch 不内置 Gemini CLI 或 Antigravity 的 OAuth Client ID/Secret。
使用这些订阅前，需要自行取得对应服务授权的 OAuth 客户端注册。
普通 Gemini API Key 供应商不需要这些设置。

启动 uni-switch 的环境可设置以下变量：

| 服务 | Client ID | Client Secret（注册要求时） |
| --- | --- | --- |
| Gemini / Code Assist | `UNI_SWITCH_GEMINI_OAUTH_CLIENT_ID` | `UNI_SWITCH_GEMINI_OAUTH_CLIENT_SECRET` |
| Antigravity | `UNI_SWITCH_ANTIGRAVITY_OAUTH_CLIENT_ID` | `UNI_SWITCH_ANTIGRAVITY_OAUTH_CLIENT_SECRET` |

程序不会从其他应用提取 OAuth 客户端凭据，也不会自动下载它们。
没有配置 Client ID 时，登录和令牌刷新会在本地返回提示。
读取 Gemini CLI 自身登录记录时，必须先配置匹配的客户端注册；原文件保持不变。
从其他工具导入刷新令牌同样要求原先签发该令牌的客户端注册。

新登录或导入的账号会在本机登录记录中保留用户提供的客户端信息，供后续续期使用。
之后修改环境变量不会把已保存账号的刷新令牌交给另一套客户端。
旧账号没有保留客户端信息时，仍使用启动环境中的配置。
这些信息属于本地凭据，不应提交到 GitHub 或复制到公开日志。

OAuth 客户端注册不自动赋予 Code Assist 或 Antigravity 的使用权限。
本候选版本仅使用合成注册和本地模拟服务验证，未完成真实订阅登录验收。
