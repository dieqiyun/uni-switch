# 维护者协作与合并指南

uni-switch 使用公开 Fork + Pull Request 协作，维护者为 **@dieqiyun**。开发者无需获得写入权限就能提交 PR；仓库不启用自动合并或自动发布。

## 外部 PR 的处理顺序

1. 打开 PR 的 **Files changed**，确认目的、影响范围及是否含敏感数据。首次 Fork 贡献可能需要在 Actions 中批准运行检查；这只是允许测试，不代表批准代码。
2. 查看 **Checks**：前端源文件审计、教程一致性、前端测试、TypeScript / Vite 构建，以及 Windows / Linux / macOS 的 Rust 测试与 Clippy。测试数量会随开发变化，以本次 CI 为准。
3. 在 **Review changes** 中选择 **Request changes** 提出修改，或选择 **Approve** 批准。所有文件（包括 CODEOWNERS、工作流与规则配置）都归 @dieqiyun 审核。
4. 作者追加修改后，旧批准自动失效，需要再次确认代码；处理全部审核讨论。若主分支有新提交，让作者更新分支并重新通过检查。
5. 满足规则后，维护者决定是否点击 **Squash and merge**、**Merge** 或 **Rebase and merge**。关闭 PR 也可以，CI 成功不会自动合并。

## main 的保护规则

规则定义保存在 `.github/rulesets/`，GitHub 生效设置在 **Settings → Rules → Rulesets**；仅修改 JSON 文件不会自动改变远端设置。

| 规则集 | 作用 | 例外 |
| --- | --- | --- |
| main-safety | 禁止删除 main 和非快进强制推送 | 无 |
| main-checks | 合并前要求来自 GitHub Actions 的 PR checks 成功，且分支跟上最新 main | 无 |
| main-review | 必须使用 PR，至少 1 个批准且必须包含代码负责人；新提交作废旧批准，审核讨论须解决 | 仅 @dieqiyun 可在 PR 合并时处理审核例外 |

GitHub 不允许作者审核批准自己的 PR。维护者自己提交代码时，仍需开 PR、通过全部 CI，可使用仅限 PR 的维护者审核例外合并。这个例外不允许直接推送 main，也不绕过另一规则集里的测试、强推或删除限制。维护者作为最终决策者，也可以在特殊情况下使用此审核例外处理外部 PR，应记录原因。

当前普通贡献者没有写入权限，因此不能直接点击合并。如果以后授予 Collaborator 写入权限，批准后的 PR 可能由该协作者点击合并；若仍要求只有维护者实际执行合并，请继续使用 Fork + PR，不新增写入权限。

## 必需检查的工作方式

[PR 工作流](../.github/workflows/pr-checks.yml) 在面向 main 的 PR、main 合并后和手动触发时执行。前端与三平台 Rust 检查结束后，固定名字的 `PR checks` 汇总结果：任何失败、取消或跳过都会使汇总失败。没有按路径跳过检查，文档 PR 也会产生必需结果。

Fork PR 只获得只读 GITHUB_TOKEN，不使用 `pull_request_target` 执行外部代码，不传入发布秘密，不上传正式程序或自动合并。工作流使用固定提交的 Action，checkout 不保留凭据。仓库 Actions 默认权限保持只读，不能批准 PR。

遇到检查失败，请先看对应 job 日志并让作者修复；基础设施临时失败可以重新运行。不要为合并某个 PR 删除必需检查。规则集对 GitHub Actions 的来源 ID 和检查名字同时匹配，改 job 名字时必须协调更新 `main-checks.json` 和远端规则，避免所有 PR 一直等待旧检查。

## 恢复或调整远端规则

GitHub CLI 登录仓库管理员账号后，可以查看当前规则：

```bash
gh api repos/dieqiyun/uni-switch/rulesets
gh api repos/dieqiyun/uni-switch/rules/branches/main
```

新建时使用 `gh api --method POST repos/dieqiyun/uni-switch/rulesets --input .github/rulesets/main-safety.json`；另外两个定义同理。已有同名规则请找到对应 ID，使用 PUT 到 `repos/dieqiyun/uni-switch/rulesets/RULESET_ID` 更新，避免重复创建。用户名变更时，需同步 CODEOWNERS；账号转移时还需更新审核例外的 User ID。

## 发布仍由维护者控制

合并不创建版本标签或 Release。正式发布沿用 [三平台构建](../.github/workflows/build-desktop.yml) 和 [发布流程](github-release.md)，保留 AGPL-3.0-only、精确对应源码归档、教程与校验文件，不替换历史版本。
