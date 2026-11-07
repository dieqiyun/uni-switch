# 模型资料：Magpie 逻辑的 Go 复现

日期：2026-11-07。适用范围：uni-switch 当前 API Key 供应商的模型资料链路。

参考仓库为 `yetone/magpie`，固定提交为 `665b9aff126e26eb5fa8931b7395feb6002185f3`。产品运行和正常构建不下载、不导入、不启动 Magpie；由 `backend/internal/modelmeta/` 下我方 Go 代码处理资料。公开预设数据和对照样例的来源、文件 SHA-256 见 [参考清单](magpie-modelmeta-reference.json)，许可见 [第三方声明](../THIRD_PARTY_NOTICES.md)。

## 已替换的完整链路

```text
上游 /models 实时条目
  → Go 解析原始资料，单独保留 vendor
  → Go 读取 models.dev 供应商目录、共享资料及实验室补充
  → Go 匹配模型、借用原厂思考档位
  → Go 合并用户设置、计算最终窗口和可公布输出上限
  → metadata.profile / metadata.capabilities / sources
  → 前端详情、Codex 目录、WorkBuddy / DSH 配置及现有协议桥接消费结果
```

`model_registry.rs` 现在只处理管道调用、缓存失效和结果校验；`model_capabilities.rs` 只投影 Go 结果。前端不再安装另一份内置能力表或自己决定资料优先级。旧 `officialProfile` / `officialCapabilities` 不参与合并；旧签名资料库不再是本链路的来源。

## 来源与实现对照

| 固定上游源与规则                                                                                                  | 我方实现                              | 验证                                                        |
| ----------------------------------------------------------------------------------------------------------------- | ------------------------------------- | ----------------------------------------------------------- |
| `catalog/live.go` 的实时字段、`codexFacts`、`kimiFacts`、`levelsOf`、`EndpointAPIs`                               | `types.go`、`supplier/models.go`      | 实时字段、畸形值、Codex 完整条目、Kimi 平面/嵌套声明测试    |
| `catalog/catalog.go` 的 `Window`、目录窗口、`loadGen`、投票与原厂档位                                             | `catalog.go`、`presets.go`            | 数值/名称/档位平票、原厂与转售商隔离、家族档位              |
| `catalog/samemodel.go`、目录的重命名、标记和拼写匹配                                                              | `match.go`                            | 快照日期、分隔符、渠道前缀、上下文标记、括号标签、canonical |
| `catalog/labs.go` 的实验室补充与 `labKinEfforts`                                                                  | `catalog.go`                          | 不覆盖已列模型；同裸 ID 的多条实验室资料仍参与投票          |
| `provider/presets.go` 的原厂目录顺序和端点数据                                                                    | `presets.json`、`presets.go`          | 从固定源 AST 提取公开数据；完整端点与区域匹配               |
| `catalog/live.go: Decorate`、`provider/models.go: entryFor / WindowOf / ListedWindow / replyLimit / OutputWithin` | `resolve.go`、`engine.go`             | 实时/目录/用户优先级、过小窗口、最大窗口、输出公布值        |
| `provider/modelprefs.go: effortsKept`、图片覆盖；`provider/ultra.go: withUltra`                                   | `types.go`、`resolve.go`              | 保持供应商词与顺序、用户筛选、关闭思考、限定模型的 Ultra    |
| `catalog/catalog.go: NearestEffort`                                                                               | `types.go`；`effortMapping` 返回 Rust | 未知词保留、别名、距离相同向上、启用思考不自动选择 none     |
| `catalog/fresh.go` 及目录读取/刷新约定                                                                            | `cache.go`                            | 新鲜/过期、失败保留、主缓存/备用缓存、跨工作进程可见        |

此表说明已核对的规则，不表示逐行复制了上游文件的全部业务。价格、账号专属模型目录、绘图/视频/检索路由、服务档位等不是本次模型规格字段的交付范围。

## 合并规则

### 上下文和输出

- 单模型手动上下文优先，不被目录中的上限截断。
- 供应商统一上下文设置受该供应商实时/自有目录窗口及实时 `max_context_window` 约束，共享投票得出的窗口不能据此限制用户统一设置。
- 无手动值时，先采用有效实时窗口；自有目录只补原始零值；正数但低于 2048 的实时窗口走共享目录回退。这与先统一清洗成零、再套自有目录的结果可能不同。
- 目录窗口考虑 `limit.input`、`limit.context` 和 2048 下限；保留 4095 等真实整数，不擅自取整成千位数。
- 输出上限先实时、再目录；单模型用户输出设置优先。对外公布的 `publishedOutput` 不大于最终上下文；资料中的原始输出仍单独保留。
- 界面/客户端所需的未知窗口默认值只在展示或序列化边界使用，不写成供应商事实或单模型用户选择。

### 图片、思考和能力

- 图片区分未知、明确支持、明确不支持。实时 `modalities.input` 明确只有文本时，目录投票不能把图片打开；用户显式 `false` 也必须保留。
- 目录的图片推断与显式声明分开保存；图片覆盖只增删 image，不抹掉 audio、video、pdf 等其他输入。
- 自有供应商目录的思考档位可以覆盖一般实时列表；完整 Codex 条目恢复供应商原始档位。转售商空缺时优先借原厂目录，原厂明确没有档位不被转售商猜测覆盖。
- 保留 `light`、`extra_high` 等供应商词及顺序，不强制清洗成我方菜单。用户选择是按供应商顺序筛选，关闭思考有独立语义。
- Ultra 只按固定上游名单及条件添加；它是 Codex 客户端模式，不代表上游 API 直接接受 ultra。完整 Codex 条目不被追加猜测档位。
- 工具调用、结构化输出、并行工具不是该上游通用实时解析器必然给出的事实。本实现不会因模型名、默认开放策略或未识别布尔字段伪造支持；仍保留已保存的显式用户选项。旧四维验证已移除，单次 16 Token 连接测试不产生能力声明。

### 模型匹配和补充

自有供应商目录、裸 ID、canonical、已知拼写及允许的别名按来源规则处理。实验室资料只补无人列出的模型；共享值按投票选出，数值平票取较小值，档位列表平票先取较短列表，再按字典序，名称平票先取较短名称。

我方只保存 API 地址，没有 Magpie 的供应商 preset 主键，因此以完整端点路径和区域恢复目录身份；同一 hostname 的按量 API 与 Coding Plan 不混用。这是接入模型的适配边界。

## 缓存和离线

- 公开源：`https://models.dev/api.json` 与 `https://models.dev/models.json`。
- 独立保存在应用数据目录 `model-metadata/`；主目录 `models-dev.json`，实验室目录 `models-labs.json`。
- 每小时检查，主缓存满 24 小时后自动更新；手动操作可强制更新。更新失败保留现有有效缓存，实验室下载失败不丢弃成功下载的主目录。
- 可回退读取已有 OpenCode `~/.cache/opencode/models.json`，但测试和设置了 `UNI_SWITCH_DATA_DIR` 的隔离环境禁用真实用户备用缓存。
- 公共下载不带供应商 Key、账户凭据或 Cookie；目录未使用发布者签名，SHA-256 用于识别缓存内容，不是来源认证。
- 首次离线无目录时继续使用实时条目和用户设置，无法确认的字段保留未知。更新目录不自动写入客户端配置。

## 保留的我方职责

SQLite、草稿确认、冲突保护、配置写入与恢复、真实推理验证、客户端格式和双向协议转换状态机仍由我方实现。`compatibility.go` 对上游 adaptive/思考关闭/采样约束作协议投影；`bridge_compatibility.json` 仅保存我方既有的中途 system 序列化开关，不包含窗口、图片、工具或档位事实。

Go 解析或通信失败时明确返回错误，不偷偷套回旧 Rust/TypeScript 能力合并。菜单的八档展示和请求协议编码不是模型资料源。

## 验证与复查

`backend/internal/modelmeta/oracle_test.go` 对比固定上游纯函数生成的 **173 个输入/预期向量**，覆盖 11 组函数。预期由隔离参考程序生成，不由我方算法计算。另有目录/缓存/最终合并测试，以及 Rust → Go → 本地模拟 HTTP 和原生 Tauri IPC 检查。

```powershell
python scripts/check-modelmeta-reference.py <固定提交源码目录>
go -C backend test ./...
go -C backend vet ./...
cargo build --manifest-path src-tauri/Cargo.toml --no-default-features --example go_core_fixture
python scripts/test-go-bridge.py
node scripts/qa-go-backend.mjs
```

参考脚本核对固定源 SHA-256 和已提交向量；产品不需要这份上游源码目录。173 个样例不是整个 Magpie 的全量等价证明。原生检查只使用 `.qa`、合成 Key 和本地模拟服务，不运行收费推理、不动真实客户端；本地 Windows 验证也不代表 Linux/macOS 原生验收已经完成。
