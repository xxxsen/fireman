# 管理后台（Admin Console）

## 目的

管理后台提供系统级只读观测能力，用于查看市场数据任务、计算作业、post-process 回调记录和本地数据版本。入口位于左侧工作栏底部，页面路由为 `/admin`。

## 页面结构

管理后台包含五个板块：

| 路由 | 内容 |
| --- | --- |
| `/admin` | 概览：任务、作业、回调、同步健康和存储统计 |
| `/admin/worker-tasks` | 市场数据任务列表和任务详情 Drawer |
| `/admin/jobs` | 计算作业列表 |
| `/admin/callbacks` | post-process 回调记录列表 |
| `/admin/data-versions` | `market_data_versions` 数据版本列表和同步健康摘要 |

所有列表页支持 URL 同步的筛选和分页，刷新页面或分享链接可以恢复当前筛选状态。

## 后端 API

Admin API 挂载在 `/api/v1/admin/*`，第一期全部为只读 `GET`：

| 路由 | 职责 |
| --- | --- |
| `GET /api/v1/admin/overview` | 概览聚合 |
| `GET /api/v1/admin/worker-tasks` | 市场数据任务列表 |
| `GET /api/v1/admin/worker-tasks/:task_id` | 市场数据任务详情 |
| `GET /api/v1/admin/jobs` | 计算作业列表 |
| `GET /api/v1/admin/post-process-records` | 回调记录列表 |
| `GET /api/v1/admin/data-versions` | 数据版本列表 |

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

## 回调记录

`post_process_records` 表记录每一次 sidecar 调用 Go post-process 的结果。该表只用于观测，不参与任务状态机，也不参与业务幂等判定。

字段包括：

- `task_id`
- `task_type`
- `attempt_no`
- `result`
- `error_code`
- `error_message`
- `duration_ms`
- `created_at`

记录写入点在 `PostProcessService.Process`。写入失败或清理失败只记录 warning，不改变 post-process 返回给 sidecar 的分类结果。

记录默认保留 30 天，每次插入后清理过期记录。

## 观测口径

- worker task 活跃状态：`pending`、`running`、`pre_complete`。
- stale running：`running` 且 heartbeat 早于当前时间 60 秒。
- 24h 统计：按 `finished_at >= now - 24h` 或 `created_at >= now - 24h` 的对应口径聚合。
- 目录和历史数据 stale：最近成功时间超过 7 天。
- worker task 列表不返回 `payload_json` / `result_data`；只有任务详情接口返回完整原始字段。

## 前端交互

- 侧边栏底部固定显示「管理后台」，与业务导航分区。
- 移动端顶部导航追加「管理」入口。
- 列表筛选写入 query string。
- 列表有 active 项时短轮询，否则低频轮询。
- 任务详情 Drawer 展示状态、时间线、heartbeat、回调记录、payload/result JSON。
- 页面仅提供观测能力，不提供删除、修改、重试或取消等写操作。

## 验证

建议验证命令：

```bash
go test ./...
npm test -- --run app/admin components/admin components/layout/AppShell.test.tsx lib/admin-format.test.ts
npm run build
```
