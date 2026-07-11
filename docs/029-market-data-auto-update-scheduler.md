# 市场数据自动更新管理与定时扫描

## 目的与边界

为市场资产目录和单个资产的历史行情提供可配置的自动更新能力。默认不产生任何自动行为；仅在用户显式创建更新规则后，后端定时扫描器才会按规则周期创建刷新任务。

自动更新完全复用现有"手动刷新 → worker task → sidecar 执行 → post-process 落库"链路，不引入新的 cron、sidecar 直写或 HTTP 回调。

不在本功能范围内：实时行情订阅、分钟级调度、按交易日历跳过非交易日、通知推送、批量默认开启、替换数据源。

## 数据模型

迁移文件 `migrations/0028_market_data_auto_update_rules.sql`。

### 规则表 `market_data_auto_update_rules`

| 字段 | 用途 |
| --- | --- |
| `id` | `aur_` 前缀 UUID |
| `target_type` | `directory_unit`（目录）或 `asset_history`（资产历史） |
| `sync_key` | 目录规则的同步单元标识，资产规则为空 |
| `asset_key` / `adjust_policy` / `point_type` | 资产历史规则的目标维度，目录规则为空 |
| `enabled` | 启停状态 |
| `interval_hours` | 更新周期（1–168 小时），默认 24 |
| `next_run_at` | 下一次允许扫描器入队的时间，是到期判据 |
| `last_enqueued_at` | 最近一次扫描器创建/绑定任务的时间 |
| `last_task_id` | 最近自动任务的 worker task ID |
| `last_success_at` / `last_failed_at` | 终态对账写入 |
| `last_error_code` / `last_error_message` | 失败原因 |
| `version` | 乐观并发控制版本号 |

唯一约束：
- 目录规则按 `(target_type, sync_key)` 唯一
- 资产历史规则按 `(target_type, asset_key, adjust_policy, point_type)` 唯一

CHECK 约束确保两种规则的字段互斥填充。

### 有效目录单元（静态注册表）

| sync_key | 说明 |
| --- | --- |
| `cn_exchange_stock` | A 股股票 |
| `cn_exchange_fund` | 场内基金（ETF/LOF） |
| `cn_mutual_fund` | 场外基金 |
| `hk_stock` | 港股股票 |
| `hk_etf` | 港股 ETF |
| `us_stock` | 美股股票 |
| `us_etf` | 美股 ETF |

## 后端架构

### Repository

`internal/repository/market_data_auto_update.go`，`MarketDataAutoUpdateRepo` 提供：

- `List(filter, page)` — 支持 target_type、enabled（含 `failed` 状态筛选）、关键字搜索
- `GetHistory(assetKey, adjustPolicy, pointType)` — 查询单个历史规则
- `UpsertDirectory` / `EnableHistory` — 幂等创建或重新启用
- `Update(id, version, enabled, intervalHours)` — 乐观锁更新
- `Due(now, limit)` — 获取到期规则
- `BindTask(id, version, taskID)` / `BindTaskTx` — 在事务内绑定任务
- `MarkScheduleFailure` — 记录调度失败并推进下一周期
- `MarkTaskSuccess(taskID)` — post-process 成功回写
- `Reconcile(now)` — 根据 worker_tasks 终态批量对账

### AutoUpdateService

`internal/service/market_data_auto_update_service.go`，负责：

1. **规则 CRUD**：`CreateDirectory`、`SetHistory`（启用/暂停）、`Update`、`List`、`HistoryRule`
2. **RunOnce 扫描**：
   - 先调用 `Reconcile` 同步已完成任务的终态到规则
   - 批量获取到期规则（每批 100 条）
   - 对每条规则调用 `enqueueRule` 创建任务
3. **入队逻辑**：
   - 目录规则 → `MarketAssetService.SyncDirectoryWithTaskHook`
   - 历史规则 → `MarketAssetService.SyncHistoryWithTaskHook`
   - 两者在同一事务内完成任务创建和规则绑定（`BindTaskTx`）
   - 若已有 pending/running 同维度任务，绑定既有任务而非创建重复
4. **失败处理**：目标不存在/无效时记录 `auto_update_target_invalid` 并推进下一周期

### AutoUpdateScheduler

同一文件中的调度器组件：

- `Start(ctx)` — 启动后立即执行一次 `RunOnce`，随后按配置间隔（默认 60 分钟）循环
- `Stop()` — 取消 context 并等待当前扫描退出
- 每次扫描有 10 分钟超时保护

### 应用生命周期集成

`internal/app/app.go` 中：
- DB 迁移和服务构造完成后创建并启动 `AutoUpdateScheduler`
- 共用与 API 相同的 DB pool 和 `MarketAssetService`
- 关闭流程：先停止 HTTP → 停止扫描器 → 停止 worker → 关闭 DB

### Post-process 集成

`PostProcessService` 在目录/历史 post-process 成功提交后调用 `MarkTaskSuccess(taskID)`。该操作为 best-effort，失败只记日志不影响 post-process 返回；`Reconcile` 在下一轮扫描时以 worker_tasks 终态兜底。

### 配置

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `AUTO_UPDATE_SCAN_INTERVAL_MINUTES` | 60 | 扫描间隔（分钟），范围 5–1440 |

## API 合同

### 资产详情侧

```text
PUT /api/v1/market-assets/history-auto-update
```

请求体：

```json
{
  "asset_key": "cn:stock:600519",
  "adjust_policy": "none",
  "point_type": "close",
  "enabled": true
}
```

- `enabled=true`：幂等创建或重新启用，首次默认 24 小时
- `enabled=false`：暂停规则（不删除）
- 资产详情响应的 `history.auto_update` 字段返回当前维度的规则状态

### 管理后台 API

```text
GET  /api/v1/admin/auto-updates?target_type=&enabled=&q=&page_size=&offset=
GET  /api/v1/admin/auto-updates/directories
POST /api/v1/admin/auto-updates/directories
PUT  /api/v1/admin/auto-updates/:id
```

- 目录清单 GET 返回全部 7 个静态目录单元（sync_key + scope + label）
- 创建目录规则：`{ "sync_key": "...", "interval_hours": N }`
- 更新规则：`{ "enabled": bool, "interval_hours": N, "version": N }`
- 版本冲突返回 `409 rule_version_conflict`

规则列表每项包含 `target_label`（目标显示名）和 `task`（关联任务状态视图）。

## 前端实现

### 资产详情页

在"刷新历史数据"控制组旁：
- 无规则/暂停 → 显示"启用每日自动更新"，点击调用 `PUT history-auto-update(enabled=true)`
- 已启用 → 显示"自动更新：每 N 小时"，可暂停或跳转管理页
- 切换历史维度后控件随响应中对应维度的规则更新

### 管理后台页面

路由 `/admin/auto-updates`，包含两个区域：

1. **资产目录区**：始终按静态注册表列出全部 7 个目录单元。未启用的行显示周期选择和"启用"按钮；已启用的行显示完整规则状态和操作。
2. **资产历史区**：支持状态筛选（全部/已启用/已暂停/最近失败）和关键字搜索，仅展示已创建的历史规则。

行操作：启用/暂停、修改周期（下拉选项 1/6/12/24/48/72/168 小时）。提交带 `version`，收到 409 时保留用户输入并提示刷新。

## 时间语义与并发安全

### 周期管理

- 规则首次启用：`next_run_at = now`，下一整点扫描进入候选
- 修改周期：`next_run_at = now + 新周期`
- 暂停：清空 `next_run_at`
- 重新启用：`next_run_at = now`
- 创建任务后立即推进 `next_run_at = now + interval`（不等待任务成功）

### 并发保护

1. `BindTaskTx` 在 `enabled=1 AND version=N` 条件下原子更新，防止并发领取
2. 任务创建和规则绑定在同一 SQLite 事务，失败回滚
3. 已有 pending/running 任务时复用而非重复创建（active-dedupe）
4. 两个并发 `RunOnce` 不产生重复任务（数据库条件更新 + worker task dedupe 双保险）

### 失败恢复

- 任务失败后仍按下一周期重试，不会每小时重复入队
- `Reconcile` 在每轮扫描开始时同步 worker_tasks 终态
- 进程重启后从持久化 `next_run_at` 恢复，不因启动立即扫描而重复创建未到期任务

## 测试覆盖

### 后端

- 空规则库不创建任何任务
- 目录/历史规则首次扫描各创建正确类型任务，payload 格式与手动一致
- 同一资产不同维度可分别建规则；重复启用幂等
- 未到期规则不入队；并发扫描不重复
- 已有手动任务时绑定复用
- Post-process 成功后 `last_success_at` 更新；失败后对账记录错误
- 暂停/版本冲突/非法目录/已删除资产/非法周期返回指定错误
- 调度器启动后立即执行并可正常停止
- 超过单批（100条）的规则可分多批处理

### 前端

- 目录清单始终显示全部 7 个单元
- 启用操作发送正确周期并更新行状态
- 版本冲突保留用户编辑并提示错误
- URL 参数 `?q=` 传递资产搜索条件（资产详情跳转管理页）
