# New API 能否只用 API Key 查询账户余额

2026-10-06 核对 QuantumNous/new-api 的 main，固定提交为 `973cf8ef4600947a4270e95ada7916740fa8264c`。

结论：可以，但取决于站点管理员的 `DisplayTokenStatEnabled` 设置。此前“New API 账户余额只能通过 Access Token + 用户 ID 查询”的描述不完整，遗漏了兼容 billing 接口。账户接口 `/api/user/self` 与 API Key 认证的 billing 接口是不同的查询途径。

## API Key 认证的计费接口

[router/dashboard.go:16](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/router/dashboard.go#L16) 将这些路由放在 `middleware.TokenAuth()` 后：

- `GET /v1/dashboard/billing/subscription`，也提供不含 `/v1` 的别名。
- `GET /v1/dashboard/billing/usage`，也提供不含 `/v1` 的别名。

请求只需 `Authorization: Bearer sk-…`。认证中间件查找 API Key 对应的令牌，并自动把其 `UserId` 写入请求上下文，无需调用方提供用户 ID 或控制台 Access Token。证据见 [middleware/auth.go:568](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/middleware/auth.go#L568) 及 [SetupContextForToken:669](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/middleware/auth.go#L669)。

## 同一个接口的余额范围取决于站点设置

| `DisplayTokenStatEnabled` | subscription 读取 | usage 读取 | 余额范围 |
| --- | --- | --- | --- |
| `false` | `GetUserQuota(userId)` 和 `GetUserUsedQuota(userId)` | `GetUserUsedQuota(userId)` | 密钥所属账户的钱包额度 |
| `true` | `token.RemainQuota` 和 `token.UsedQuota` | `token.UsedQuota` | 当前密钥额度 |

证据见 [controller/billing.go:17](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/controller/billing.go#L17) 和 [controller/billing.go:75](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/controller/billing.go#L75)。

该提交中默认值为 `true`，见 [common/constants.go:25](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/common/constants.go#L25)。管理员可以修改并保存该选项，见 [model/option.go:416](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/model/option.go#L416)。客户端不能通过修改请求头或传入用户 ID 强制将它切换到账户余额。

该版本的公开 `/api/status` 返回展示单位与汇率配置，但未暴露 `DisplayTokenStatEnabled`；两个 billing 响应也没有明确的账户 / 密钥范围字段。因此不能仅根据成功返回或余额数值，自动证明读取的是账户余额。

## 剩余金额需要两个接口计算

`subscription.hard_limit_usd` 是已用与剩余之和；`usage.total_usage` 是已用值乘 100。因此有限额度时：

```text
remaining = subscription.hard_limit_usd - usage.total_usage / 100
```

不能把 `hard_limit_usd` 直接当作剩余金额，也不能再次除以 500000。后端已按站点的额度展示类型换算：USD、CNY 或 TOKENS；字段名包含 `_usd` 并不保证实际单位总是 USD。无限密钥在该接口中使用 `100000000` 的特殊上限值，不应把它显示成真实账户资产。证据见 [controller/billing.go:41](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/controller/billing.go#L41) 和 [controller/billing.go:93](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/controller/billing.go#L93)。

billing 路由使用严格 TokenAuth：密钥被禁用、过期、额度耗尽或受到 IP / 分组限制时可能拒绝查询。专门的 `/api/usage/token/` 使用只读令牌认证，对过期 / 耗尽 / 禁用密钥的处理不同，但返回的是密钥额度，不能静默将其结果作为账户余额。证据见 [model/token.go:220](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/model/token.go#L220)、[middleware/auth.go:447](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/middleware/auth.go#L447) 和 [router/api-router.go:301](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/router/api-router.go#L301)。

## “导入 CC Switch”按钮实际携带的内容

当前官方 [cc-switch-dialog.tsx:70](https://github.com/QuantumNous/new-api/blob/973cf8ef4600947a4270e95ada7916740fa8264c/web/src/features/keys/components/dialogs/cc-switch-dialog.tsx#L70) 生成 `ccswitch://v1/import?...` 深链接，携带供应商名称、客户端类型、API 地址、API Key、模型、homepage 及 `enabled=true`。

该函数没有携带控制台 Access Token、用户 ID、`usageScript`、`usageEnabled` 或自动查询间隔。`enabled=true` 不能等同于 Sub2API 的 `usageEnabled=true`。Sub2API 的导入链接明确携带查询脚本，两个项目的官方导入实现不同；第三方站点分支也可能自行扩展链接。

## 当前 uni-switch 的实现范围

0.3.2 已实现默认自动查询：只输入 API 地址和 Key 后，软件自动尝试 `/api/usage/token/`、公开 `/api/status` 与 `subscription + usage` 双接口，不需要用户选择类型或输入 Access Token / 用户 ID。billing 结果按差值计算，显示“可用额度”并说明由站点决定账户或密钥范围；查询失败且密钥额度可用时明确显示“密钥额度”。单位读取公开展示配置，billing 响应不会再次除以 500000。

旧版已保存的独立控制台 Access Token 和用户 ID 仍可自动复用 `/api/user/self`，仅用于绑定的原站点。编辑 API 地址或重新填写推理 Key 后改用自动配置；控制台令牌不写入 Codex 或 Claude 客户端。新增配置不要求这些额外信息。行为与验证见 [自动余额查询说明](auto-balance.md)。

此次证据为官方源码，未使用真实站点和用户凭据；无法据此判断某个供应商实例当前的开关值或部署版本。
