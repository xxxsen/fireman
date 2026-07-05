# 市场数据任务化架构（AKShare 资产数据 Worker 化）

## 目的

本文整理 td/078（AKShare 资产数据任务化改造）与 td/079（实施 review 修复）落地后的最终实现：市场数据获取从「Go 同步调用 sidecar HTTP」切换为「Go 建任务、sidecar 做 worker、Go post-process 落库」的异步链路，并建立全市场本地资产目录（含港股/美股 ETF）。

## 总体架构

```
Web ──(公开 API)──> Go backend ──写 worker_tasks──> SQLite 主库
                        ▲                              │
                        │ POST /internal/resources     │ 轮询认领
                        │ POST /internal/tasks/{id}/   ▼
                        │      post-process        sidecar worker ──> AKShare / Eastmoney / TickFlow
                        └──────────(内部 API)──────────┘
```

- **Go 层**：唯一的业务写入方。创建任务（去重）、提供任务状态查询、接收 sidecar 上传的结果资源（`resource_db` 归 Go 所有）、post-process 校验并落库、维护版本幂等。
- **sidecar（`sidecars/market-provider`）**：纯 worker。对外 HTTP 仅 `GET /healthz`；旧 `POST /v1/instruments/fetch`、`/v1/instruments/resolve`、`/v1/metadata/refresh` 已移除（404）。
- **Web 层**：资产目录、资产详情、导入流程全部基于本地目录 + 任务轮询（`useWorkerTaskPolling`），搜索不触发外部请求。

## 数据模型

| 表 | 职责 |
| --- | --- |
| `worker_tasks` / `worker_task_versions` | 任务队列、状态机（`pending/running/pre_complete/complete/failed`）、按业务键单调递增的 `version_no` |
| `market_assets` | 全市场资产目录快照（asset_key = `market\|instrument_type\|region_code\|symbol`） |
| `market_asset_sync_state` | 每个 scope 的最近任务 / 最近成功记录 |
| `market_asset_points` | 按 (asset_key, adjust_policy, point_type) 维度的历史行情 |
| `market_asset_history_state` | 每个历史维度的同步状态与 source 锁定 |
| `market_asset_detail_projections` | 详情页年度/区间收益投影 |
| `market_data_versions` | post-process 幂等版本表（目录 / 历史 / FX 各自版本键） |
| `fireman_resource.db` | 内容寻址资源库：resource_key = gzip payload 的 SHA256，TTL 清理，仅 Go 可见 |

## 任务类型与执行链

### asset_directory_sync

scope 与 required category（任一类别失败/为空 → 整个任务失败，无部分成功）：

| scope | instrument_types | 上游 |
| --- | --- | --- |
| `cn_all` | `cn_exchange_stock`、`cn_exchange_fund`、`cn_mutual_fund` | `ak.stock_zh_a_spot_em` / `ak.fund_etf_spot_em`+`ak.fund_lof_spot_em` / `ak.fund_name_em` |
| `hk_all` | `hk_stock`、`hk_etf` | `em.hk_equity_list`（主板+创业板）、`em.hk_fund_list`（基金板） |
| `us_all` | `us_stock`、`us_etf` | `em.us_equity_list`（普通股/优先股/ADR/CEF）、`em.us_etf_list` |

`em.*` 为 sidecar 内对东方财富 quote-center `qt/clist` 接口的直连封装（AKShare 无 HK/US ETF 列表函数），按分类板 `fs` 过滤、分页拉全量，经 `timeout_util` 硬超时子进程执行，host 顺序 `push2delay` → `72.push2`。

HK 分类规则：基金板（`m:116 t:1`）中名称含 `信托/房托/房产` 或代码 < 2800 的条目视为 REIT/信托，归入 `hk_stock`（kind=`reit`）；其余为 `hk_etf`（kind=`etf`），名称后缀 `-U`/`-R` 分别标记 USD/CNY 货币柜台。美股 ETF 取 Eastmoney `t:5` 分类板（kind=`etf`）。

**post-process 覆盖率门槛**：每个 required category 必须非空，且数量不得低于「上次成功、**同 listing source**」active 数量的 90%；listing source 变更（换源/分类迁移）按首次同步处理，只要求非空，避免旧口径计数把同步永久卡死。通过校验后 upsert 资产、把本次未出现的条目标记 inactive、更新 sync state 与版本键 `asset_directory|{scope}`。

### asset_history_sync

- 维度：(asset_key, adjust_policy, point_type)；模式 `default_refresh`（有历史 → 同源增量 merge，无历史 → 全量替换）与 `switch_source_full`（仅上次任务 `source_unavailable` 失败后允许，换源全量替换）。
- source-pinned：payload 带 `required_source_name` 时 sidecar 只调该源，任何失败 → `source_unavailable`，绝不静默换源；无 pinned 时走 TickFlow 优先 + AKShare fallback 链。
- post-process 校验：merge 窗口必须与已有数据衔接（无 gap）；全量替换必须覆盖既有起点、点数 ≥ 既有 95%、最新日期滞后 ≤ 10 天（退市除外）。通过后重算详情投影，并把序列镜像到 `market_data_points` / `instrument_annual_returns` / `instrument_library_metrics`（用户 instrument 投影）。

### fx_rate_sync

`USDCNY` / `HKDCNY` 全量历史（自 1990-01-01），来源中行新浪牌价，post-process 整表替换系统 FX instrument 序列，版本键 `fx_rate|{pair}`。

## worker 生命周期

1. **claim**：`BEGIN IMMEDIATE` 下 CAS `pending → running`，单进程串行执行；
2. **heartbeat**：每 10s 刷新 `heartbeat_at`，CAS 失配即放弃当前任务；
3. **执行**：executor 产出结果 JSON → gzip → `POST /internal/resources`（幂等：resource_key = SHA256）→ CAS `running → pre_complete`；
4. **notify**：`POST /internal/tasks/{id}/post-process`，Go 返回 `success` / `retryable_error` / `permanent_error`，worker 对应 CAS 到 `complete` / 指数退避重试（≤10 次、间隔 ≤5min）/ `failed`；
5. **janitor**：心跳超时（60s）的 running 任务标记失败；`pre_complete` 超过 1h 硬超时标记失败。

## 公开 API（Go）

| 路由 | 职责 |
| --- | --- |
| `POST /api/v1/market-assets/sync` | 创建目录同步任务（`{scope}`，active 任务去重返回 `existed=true`） |
| `POST /api/v1/market-assets/history-sync` | 创建历史同步任务（`{asset_key, mode, ...}`） |
| `POST /api/v1/market-assets/fx-sync` | 创建汇率同步任务 |
| `GET /api/v1/tasks/{id}` | 任务状态轮询 |
| `GET /api/v1/market-assets` | 本地目录搜索（`q/market/instrument_types/include_inactive/limit/offset`），响应含各 scope 同步状态块 |
| `GET /api/v1/market-assets/by-key?asset_key=` | 资产详情（目录条目 + 历史状态 + 点位 + 收益投影） |
| `POST /api/v1/instruments/import` | 从目录条目导入用户 instrument（纯本地投影，无历史时返回 `market_asset_history_empty`） |

## Web 页面

- **资产目录 `/assets`**：三个 scope 同步行 + FX 同步行（状态徽章、最近成功时间、失败错误码、轮询错误提示）、本地搜索、市场筛选、「含已退市」过滤、7 天未同步提醒；HK/US ETF 与股票同列展示，可直接进入详情/录入。
- **资产详情 `/assets/market/{assetKey}`**：基础信息、历史维度状态、年度收益、手动「刷新历史」（default_refresh / switch_source_full）。
- **导入 `/assets/import`**：本地目录搜索 → 选择候选（含 HK/US ETF）→ 选资产类别/区域 → 导入；无本地历史时引导先到详情页同步。

## 关键不变量

- `resource_db` 对 sidecar 不可见，资源只经 Go 上传 API 写入；上传幂等（同 SHA256 重复上传仅刷新 TTL）。
- post-process 全部幂等：`market_data_versions` 版本键 + `version_no` 单调比较，重复/乱序通知不产生重复写入。
- 目录/历史/FX 落库均为单事务提交，校验失败不触碰业务表。
- 历史序列同源不变量：merge 锁定单源、full 整段替换；提交后发现混源仅告警并等待下次全量修复。
- 任务去重：`(type, dedupe_key)` 上 active 状态唯一。

## 测试与验证

- Go：`go test ./...`（任务 API、内部 API、post-process 生命周期/覆盖率门槛/换源迁移、资源库、ETF 目录可搜索集成测试）。
- sidecar：`uv run pytest`（worker 循环、执行器、em_directory 分页/降级、fetch 链回归）；live 验证通过 `FIREMAN_LIVE_AKSHARE=1` / `FIREMAN_LIVE_TICKFLOW=1` 直驱 fetch 链，不再依赖已移除的 HTTP 端点。
- Web：`npm test -- --run`（目录/详情/导入/持仓选择器，含 HK/US ETF 展示与选择用例）。
