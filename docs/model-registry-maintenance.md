# 模型资料签名源维护

应用与前端内置 `src/content/model-capabilities.json`，数据版本独立于应用版本。只补充经官方资料核实的精确 ID 或别名，不使用任意型号前缀匹配。资料是能力声明，不代表所有中转站实际支持。上下文资料不得改变当前上下文同步规则，资料中的思考档位不得缩减客户端八档菜单。

## 准备资料

1. 更新 `schemaVersion: 1` 的 JSON，递增整数 `version` 并记录 `verifiedAt`（YYYY-MM-DD）。同版本不同内容被拒绝，回滚内容也必须使用新的更高版本。
2. 每条记录提供不重复的精确 `ids`、HTTPS 官方 `source`、`capabilities`，以及可选 `profile`。缺失或 null 表示未知，false 表示明确不支持，`reasoningEfforts: []` 表示明确不支持思考档位。不要把网关私有名称直接视作官方型号。
3. `profile` 支持 `contextWindow`、`maxInputTokens`、`maxOutputTokens`、`reasoningEfforts`、`defaultEffort`、`thinkingFormat`、`samplingParameters`、`toolCalls`、`structuredOutput` 和 `endpoints`。接口声明分别使用 `messages`、`chatCompletions`、`responses`。
4. `thinkingFormat` 使用 `adaptive`、`budget`、`deepseek`、`openai` 或 `none`。有效档位使用客户端八档值；默认档位必须出现在明确的档位列表中。官方档案仅作低优先级回退；手动设置与上游声明优先。
5. 运行 `cargo test --manifest-path src-tauri/Cargo.toml --no-default-features model_registry` 与前端能力测试，并人工复核官方来源、JSON 差异和升级影响。

## 签名与配置

私钥在仓库外生成并保管，绝不提交或打包。使用标准 Ed25519 PKCS#8 PEM 私钥；应用仅包含对应的 32 字节公钥。签名命令不会生成、输出或改写私钥，也不会改写 JSON：

```powershell
node scripts/sign-model-registry.mjs src/content/model-capabilities.json C:/release-keys/model-registry.pem C:/release-output/model-capabilities.json.sig
```

输出含公钥的 Base64 编码及资料版本。签名覆盖 JSON 的原始字节，上传后不能重新格式化或更改换行。输出文件必须不存在，工具不会覆盖已有文件。

在审查过的 HTTPS 静态托管位置发布完全相同的 `model-capabilities.json` 和 `model-capabilities.json.sig`。签名文件包含 Base64 签名，位于 JSON URL 后追加 `.sig` 的位置。应用不接受重定向；源不能包含账号、密码、查询参数或片段。

由维护者将 `release-config.json` 中的配置替换为：

```json
{
  "githubRepository": "dieqiyun/uni-switch",
  "modelRegistry": {
    "url": "https://your-reviewed-host.example/model-capabilities.json",
    "publicKey": "BASE64_OF_THE_32_BYTE_ED25519_PUBLIC_KEY"
  }
}
```

以上 URL 和公钥是格式示例，不是已部署服务。配置嵌入编译产物；首次配置或更换信任公钥需要新的应用版本。后续同公钥的数据更新不需要重发应用。先上传 JSON，再上传对应签名；短暂不一致只会验签失败并保留旧资料。完成平台构建和制品审计前不得发布。

## 失败与离线

单次下载限制 2 MiB，签名限制 256 字节，HTTP 超时 20 秒。仅验签、校验字段并检查版本通过后，原子保存私有数据目录中的 `model-registry-signed.json`，再替换内存资料。缓存保存签名封装而非可信裸 JSON，启动时重新验签。不保存供应商凭据，也不向更新源发送供应商认证。

`model-registry-checked.json` 仅记录成功检查时间。网络失败或签名错误不会更新资料；每日检查失败后下个周期再试。无有效远程配置时不发请求，内置资料与供应商同步继续工作。更新资料不会直接修改真实客户端配置；重新同步、确认保存后可更新模型目录。转换服务的官方回退会随最新资料生效，仍不覆盖上游声明或手动设置。
