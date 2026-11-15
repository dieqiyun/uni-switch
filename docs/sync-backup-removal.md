# 同步与备份功能移除

调整日期：2026-11-08。

本应用不再提供跨设备配置同步和整机设置备份。删除的是实际实现及调用链，包括页面、后台任务、管理 API 和命令行命令。

已删除：

- 设置中的「同步与备份」分页，以及搜索和导航关联。
- WebDAV、S3、GitHub 配置同步、定时同步、冲突合并、远程恢复和撤销。
- 配置包的导出、导入、密码加密与恢复。
- 自动备份、备份周期、保留份数、文件夹选择、版本列表和版本恢复。
- 登录凭据随备份打包、跨设备合并及恢复的专用代码。
- 跨设备配额历史合并、用量上传／导入和远端排序镜像等专用辅助代码。
- 远程 S3 请求存档接入；请求存档继续支持用户主动开启的本地记录。
- 客户端「不参与同步」、MCP「同步到其他电脑」及相关命令。
- 资源库的备份文件夹管理入口。写入客户端文件前的安全副本继续保留。

`internal/backup`、`internal/davsync`、GUI/CLI 的同步备份实现及其专属脚本已经删除；供应商和资源库中的专用打包、恢复与远端合并代码也已移除。旧的 `/api/davsync`、`/api/backup/*`、`/api/autobackup/*` 和 `/api/agents/here` 不再注册，访问返回 404。

旧地址 `?view=settings&tab=sync` 和旧版保存的同步分页状态会回到仍可用的设置分页。无任何远程服务需要重新配置。

## 保留的本地安全机制

以下机制用于保护配置写入，属于正常业务操作的错误恢复：

- 原子写入、临时文件与文件锁。
- 客户端原配置副本、`.bak`、损坏文件检测及失败回滚。
- 断开客户端时恢复接入前的配置。
- 资源库修改客户端文件前保存安全副本。
- 同一台电脑上将资源库的技能、MCP 和指令应用到客户端。

账号登录与刷新、供应商管理、推理网关、模型目录、用量与会话继续保留。资源库的本地应用刷新不属于跨设备同步。

## 历史数据

不清理已有的 `sync.json`、`sync-state.json`、`autobackup.json`、同步文件夹或备份文件。旧文件不能重新启动已删除的任务；其中凭据不会用于远程同步。部分历史 JSON 字段只为读取既有配置而保留，不提供同步操作。已有用量历史仍可查看，其兼容读取代码不再提供同步写入、导出或合并。

## 验证入口

```sh
corepack pnpm build
corepack pnpm test
node scripts/test-go.mjs --log .qa/sync-regressions.log -timeout=5m -run '^TestUniSwitch' ./...
node scripts/test-go.mjs --log .qa/sync-vet.log vet ./...
node scripts/qa-source-sync-backup.mjs
node scripts/check-upstream-source.mjs
```

具体执行结果和验证边界记录在 [源码整合验证记录](source-integration-qa.md)。不将定向回归称作全部历史测试通过，也不将 Windows 构建称作完成三平台发布验收。
