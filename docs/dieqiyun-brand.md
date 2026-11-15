# 蝶祈云预设与图标

- 官网：https://www.dieqiyun.top
- 官网公开配置的 API 根地址：https://cfapi.dieqiyun.top
- 本应用 Chat Completions Base URL：https://cfapi.dieqiyun.top/v1
- Responses Base URL：https://cfapi.dieqiyun.top/v1
- Anthropic Messages Base URL：https://cfapi.dieqiyun.top（请求路径 /v1/messages）
- 服务端点选择（按用户 2026-11-09 提供的官网截图）：
  - 普通用户（默认推荐）：`https://cfapi.dieqiyun.top`
  - 下游对接（美国洛杉矶）：`https://haapi.dieqiyun.top`
  - 生图专用：`https://cfimg.dieqiyun.top`，不提供 Responses / Anthropic 地址或对话端点测试。
  - CF CDN 备用：`https://hw.dieqiyun.top`
- 对话端点沿用 OpenAI / Responses 的 `/v1` 地址和 Anthropic 根地址；生图专用端点使用 `/v1` 作为图像 API 的基础地址。切换端点会自动更新表单地址，保存后才应用；已有自定义地址不会自动改写。
- 新增端点的配置选择与保存已做隔离验证，未使用真实 API Key 验证各节点的模型推理能力。
- 无凭据路由检查：Chat Completions、Responses、Messages 返回 API_KEY_REQUIRED；不存在的路径返回 404。该检查不代表真实模型推理通过。
- 图标来源：https://www.dieqiyun.top/dieqi-tab-32.png?v=20261004
- 官网完整标志由 `/assets/dieqi-logo-CqpitJd4.js` 指向 `/dieqi-logo.png`；应用的小尺寸图标使用官网提供的 32×32 透明 PNG，无重绘。
- 获取日期：2026-11-08
- 本地路径：`backend/internal/gui/assets/icons/dieqiyun.png`
- 文件 SHA-256：`58816380fb7a07045fc7e3a18eaa6fa41f65cae51fbb5db7460f6c05ad238fbe`

图标用于标识对应服务，商标权利归其权利人所有。
未使用真实 API Key 发起付费模型请求；未将无凭据的 401 当作推理协议验证通过。
