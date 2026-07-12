# 管理后台（Admin Console）

## 目的

管理后台提供系统级任务观测和自动更新配置能力，用于查看统一 worker task、finalizer 落库记录、本地数据版本及自动更新规则。入口位于左侧工作栏底部，页面路由为 `/admin`。

## 页面结构

管理后台包含五个板块：

| 路由 | 内容 |
| --- | --- |
| `/admin` | 概览：统一任务、finalizer、同步健康和存储统计 |
| `/admin/worker-tasks` | 全部 Go/sidecar 任务列表和任务详情 Drawer |
| `/admin/finalizations` | finalizer 业务落库记录列表 |
| `/admin/data-versions` | `market_data_versions` 数据版本列表和同步健康摘要 |
| `/admin/auto-updates` | 目录及资产历史自动更新规则管理 |

所有列表页支持 URL 同步的筛选和分页，刷新页面或分享链接可以恢复当前筛选状态。

## 后端 API

Admin API 挂载在 `/api/v1/admin/*`。任务、finalizer 和数据版本接口只读；自动更新规则提供启用、停用和周期修改：

| 路由 | 职责 |
| --- | --- |
| `GET /api/v1/admin/overview` | 概览聚合 |
| `GET /api/v1/admin/worker-tasks` | 按 worker type、task type、状态和关键词查询统一任务 |
| `GET /api/v1/admin/worker-tasks/:task_id` | 统一任务详情 |
| `GET /api/v1/admin/finalize-records` | finalizer 落库记录列表 |
| `GET /api/v1/admin/data-versions` | 数据版本列表 |
| `GET /api/v1/admin/auto-updates` | 自动更新规则列表 |
| `GET /api/v1/admin/auto-updates/directories` | 可配置目录单元清单 |
| `POST /api/v1/admin/auto-updates/directories` | 启用目录自动更新 |
| `PUT /api/v1/admin/auto-updates/:id` | 修改启用状态和更新周期 |

列表响应统一使用：

```json
{
  "items": [],
  "total": 0,
  "limit": 20,
  "offset": 0
}
```

分页规则：默认 `limit=20`，最大 `100`，负数 `offset` 归零。

## Finalizer 记录

`worker_task_finalize_records` 记录 Go finalizer 对 `pre_complete` 任务执行资源校验和业务落库的每次尝试。该表只用于观测，不是任务状态或业务幂等的事实来源。

字段包括：

- `task_id`
- `task_type`
- `attempt_no`
- `result`
- `error_code`
- `error_message`
- `duration_ms`
- `created_at`
- `created_at`

任务是否完成仍由 `worker_tasks` 状态和 finalizer 的原子业务提交决定。

## 观测口径

- worker task 活跃状态：`pending`、`running`、`pre_complete`。
- lease 是否过期以服务端 `lease_expires_at` 判断，不由前端按 heartbeat 自行推导。
- 24h 统计：按 `finished_at >= now - 24h` 或 `created_at >= now - 24h` 的对应口径聚合。
- 目录同步健康：按 scope 展示聚合状态（`running/complete/partial/failed/never`），并展开每个目录同步单元（`sync_key`）的活跃任务、最近失败与最近成功时间；scope 级 `last_success_at` 为全部单元成功时间的最小值，任一单元未成功则为空。
- 目录和历史数据 stale：最近成功时间超过 7 天。
- worker task 列表不返回完整 payload/result；任务详情接口提供冻结 payload、result key/meta、attempt 和 finalizer 时间线。

## 前端交互

- 侧边栏底部固定显示「管理后台」，与业务导航分区。
- 移动端顶部导航追加「管理」入口。
- 列表筛选写入 query string。
- 列表有 active 项时短轮询，否则低频轮询。
- 任务详情 Drawer 展示状态、时间线、heartbeat、finalizer 记录、payload 和 result metadata。
- 任务页面只读；自动更新页面通过乐观锁修改规则，版本冲突时要求刷新后重试。

## 验证

建议验证命令：

```bash
go test ./...
npm test -- --run app/admin components/admin components/layout/AppShell.test.tsx lib/admin-format.test.ts
npm run build
```
