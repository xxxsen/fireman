# td/054 实施 Review

## Review 结论

`td/054` 的 4 项问题均已完整修复，未发现新的缺陷或实现缺失：

- running 的 stress/sensitivity 任务以取消优先的条件更新收敛终态，supersede 与成功收尾竞态下不会再覆盖为 `succeeded`。
- 抓取中的资产从前端和服务端均禁止编辑分类；`fetch_failed` 资产编辑后重试会保留新分类。
- 外部点击与 Escape 会统一关闭本地/AKShare 候选及相关状态。
- 本地候选列表固定为 10 个 48px 单行候选的 30rem 视口，分页不改变高度。

## 已验证项

- `RequestCancelRunningWithErrorTx` 仅取消 running 任务并记录 `superseded_by_newer_analysis`；`FinishRunningIfNotCanceled` 与 `FinishCanceledIfRequested` 使取消请求优先于成功终态。
- `worker_analysis_cancel_test.go` 使用同步屏障复现“最后一次 cancelCheck 后发生 supersede”的竞态，确认旧 job 最终为 `canceled`；未被取消的分析仍正常成功。
- 分类更新服务复用进行中抓取检查，`pending_fetch` 返回 `instrument_fetch_in_progress`；详情页隐藏编辑入口并提示等待抓取完成。
- `fetch_failed` 资产编辑分类后重试抓取的集成测试确认分类不会被导入时 payload 覆盖。
- 外部 AKShare 候选的外部点击/Escape 关闭测试，以及固定 10 行高度和单行截断测试均已覆盖。

## 验证记录

- `go test -count=1 ./...` 通过。
- `cd web && npm run test:ci -- app/assets/[id]/page.test.tsx components/plans/AssetClassHoldingPicker.test.tsx app/plans/new/page.test.tsx lib/format.test.ts` 通过，58 个测试通过。
- `cd web && npm run lint` 通过。
- `cd web && npm run build` 通过。
- `git diff --check` 通过。

## 文档状态

正式行为已同步至：

- `docs/012-web-data-density-and-asset-detail.md`
- `docs/015-fire-simulation-history-retention.md`
