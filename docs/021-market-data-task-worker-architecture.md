# 市场数据任务化架构（AKShare 资产数据 Worker 化）

## 目的

本文描述市场数据任务化架构的最终实现：市场数据获取从「Go 同步调用 sidecar HTTP」切换为「Go 建任务、sidecar 做 worker、Go post-process 落库」的异步链路，并建立全市场本地资产目录（含港股/美股 ETF）。

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
- **Web 层**：资产目录、资产详情、计划选标全部基于本地目录 + 任务轮询（`useWorkerTaskPolling`），搜索不触发外部请求。

## 数据模型

| 表 | 职责 |
| --- | --- |
| `worker_tasks` / `worker_task_versions` | 任务队列、状态机（`pending/running/pre_complete/complete/failed`）、按业务键单调递增的 `version_no` |
| `market_assets` | 全市场资产目录快照（asset_key = `market\|instrument_type\|region_code\|symbol`） |
| `market_asset_sync_state` | 每个目录同步单元（`sync_key`，含 `fx_rates`）的最近任务 / 最近成功记录，`scope` 为索引分组列 |
| `market_asset_points` | 按 (asset_key, adjust_policy, point_type) 维度的历史行情 |
| `market_asset_history_state` | 每个历史维度的同步状态与 source 锁定 |
| `market_asset_detail_projections` | 详情页年度/区间收益投影 |
| `market_data_versions` | post-process 幂等版本表（目录 / 历史 / FX 各自版本键） |
| `fireman_resource.db` | 内容寻址资源库：resource_key = gzip payload 的 SHA256，TTL 清理，仅 Go 可见 |

## 任务类型与执行链

### asset_directory_sync

目录同步以**目录同步单元（`sync_key`）**为真实任务粒度，`cn_all/hk_all/us_all` 只是 UI 聚合视图。单元由 Go 层静态 registry 定义，前端不能提交自定义 `markets/instrument_types`：

| sync_key | scope | instrument_types | 上游 |
| --- | --- | --- | --- |
| `cn_exchange_stock` | `cn_all` | `cn_exchange_stock` | `em.cn_sh_a_list` / `em.cn_sz_a_list` / `em.cn_bj_a_list`（分交易所板） |
| `cn_exchange_fund` | `cn_all` | `cn_exchange_fund` | `em.cn_etf_list` / `em.cn_lof_list`（保留 Eastmoney `f13` 市场标识） |
| `cn_mutual_fund` | `cn_all` | `cn_mutual_fund` | `ak.fund_name_em` |
| `hk_stock` | `hk_all` | `hk_stock` | HKEX List of Securities（身份字段），`em.hk_equity_list`（中文展示名补充） |
| `hk_etf` | `hk_all` | `hk_etf` | HKEX List of Securities（身份字段），`em.hk_fund_list`（中文展示名补充） |
| `us_stock` | `us_all` | `us_stock` | `em.us_equity_list`（普通股/优先股/ADR/CEF） |
| `us_etf` | `us_all` | `us_etf` | `em.us_etf_list` |

单元内任一 required category 失败/为空 → 该单元任务失败；单元之间彼此隔离，例如 `cn_mutual_fund` 失败不影响 `cn_exchange_stock` 成功落库。payload/result 均携带 `sync_key` 并在 post-process 校验一致。

`em.*` 为 sidecar 内对东方财富 quote-center `qt/clist` 接口的直连封装，按分类板 `fs` 过滤、分页拉全量，经 `timeout_util` 硬超时子进程执行，host 顺序 `push2delay` → `72.push2`（延迟行情主机；AKShare spot 函数指向的实时 push2 主机群会在部分网络下于连接层直接断开，且目录场景只取代码/名称/板归属，不取行情数值）。

目录身份只接受结构化、可验证字段，不使用名称关键词、代码前缀或经验枚举做生产判断：

- CN 股票交易所来自分交易所板本身（SH/SZ/BJ 各自的 `fs` 过滤器）；北交所板行级 `f13=0` 与深市相同，故 `f13` 不参与股票交易所判断。
- CN 场内基金交易所来自 Eastmoney 行级 `f13` 市场标识（1=SH，0=SZ）；缺失或未知的行跳过并记录目录数据不完整。
- HK 股票/ETF/REIT/货币柜台来自 HKEX List of Securities 的 `Category`、`Sub-Category`、`Trading Currency`；Eastmoney HK 列表只作为展示名补充，不参与身份判断。
- US 股票/ETF 来自 Eastmoney 对应分类板。

如果上游不能提供需要的确定字段，目录同步应失败或跳过该条目并暴露原因，不能静默猜测后写入权威身份字段。

**post-process 覆盖率门槛**：每个 required category 必须非空，且数量不得低于「上次成功、**同 listing source**」active 数量的 90%；listing source 变更（换源/分类迁移）按首次同步处理，只要求非空，避免旧口径计数把同步永久卡死。通过校验后 upsert 资产、只对当前单元覆盖的 `market + instrument_type` 做 inactive 标记、更新 `market_asset_sync_state[sync_key]` 与版本键 `asset_directory|{sync_key}`。

### asset_history_sync

- 维度：(asset_key, adjust_policy, point_type)；模式 `default_refresh`（有历史 → 同源增量 merge，无历史 → 全量替换）与 `switch_source_full`（仅上次任务 `source_unavailable` 失败后允许，换源全量替换）。
- source-pinned：payload 带 `required_source_name` 时 sidecar 只调该源，任何失败 → `source_unavailable`，绝不静默换源；无 pinned 时走 TickFlow 优先 + AKShare fallback 链。
- 历史抓取 payload 的 `market`、`instrument_type`、`region_code`、`exchange`、`symbol`、`instrument_kind` 全部来自 `market_assets`；sidecar 只做格式转换，例如 `region_code=sh + symbol=600036` 转成 `sh600036`。
- CN 场内资产缺少 `region_code/exchange` 时任务失败为 `asset_identity_incomplete`；字段冲突时失败为 `directory_identity_invalid`。sidecar 不根据代码前缀、名称或上游空结果重新推断交易所。
- post-process 校验：merge 窗口必须与已有数据衔接（无 gap）；全量替换必须覆盖既有起点、点数 ≥ 既有 95%、最新日期滞后 ≤ 10 天（退市除外）。通过后重算 `market_asset_detail_projections` 详情投影；历史序列只写 `market_asset_points`，不存在用户资产镜像。

provider 响应不包含 FIRE `asset_class`。FIRE 分类只来自 `plan_holdings.asset_class`，由用户在计划持仓中选择；行情抓取层只返回市场数据、数据源、币种、点位类型和 source_kind。

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
| `POST /api/v1/market-assets/sync` | 创建目录同步任务：`{scope}` 批量创建该 scope 下全部单元任务，`{sync_key}` 只创建单个单元任务；统一返回 `tasks` 数组，active 单元去重返回 `existed=true` |
| `POST /api/v1/market-assets/history-sync` | 创建历史同步任务（`{asset_key, mode, ...}`） |
| `POST /api/v1/market-assets/fx-sync` | 创建汇率同步任务 |
| `GET /api/v1/tasks/{id}` | 任务状态轮询 |
| `GET /api/v1/market-assets` | 本地目录搜索（`symbol_q/name_q/market/instrument_types/include_inactive/limit/offset`），响应固定携带全部 scope 聚合同步视图（`status` = `running/complete/partial/failed/never` + `units` 单元明细）与每个资产的历史就绪状态（`has_history`、最新同步任务状态） |
| `GET /api/v1/market-assets/by-key?asset_key=` | 资产详情（目录条目 + 历史状态 + 点位 + 收益投影） |
| `GET /api/v1/plans/{id}/simulation-readiness` | 计划模拟就绪检查（`blocking_assets` 按原因细分 + 进行中的同步任务） |
| `POST /api/v1/plans/{id}/sync-missing-asset-history` | 仅为真正缺历史的资产批量创建/复用历史同步任务；已同步但不可模拟的资产返回 `blocked` |

计划持仓直接引用 `market_assets.asset_key`（系统现金为内置资产 `SYS|cash||CNY` 等），
不存在“用户资产库/录入”中间层；缺历史的持仓可先保存，模拟创建前由 readiness 检查拦截
（错误码 `market_asset_history_missing`，准入以快照试算为准，细分原因见 `docs/017`）。

## Web 页面

- **资产目录 `/assets`**：固定展示三个目录 scope 聚合行（每行内含单元明细）+ FX 同步行，不受资产列表筛选影响。scope 行使用 split button：主按钮「同步全部」创建该 scope 下全部单元任务，下拉项只同步单个单元（active 单元下拉项禁用并显示同步中）。展示聚合状态徽章、最近全量成功时间（任一单元未成功显示「部分未同步」）、单元级失败错误码、轮询错误提示；全部 scope `complete` 时默认折叠。另有代码/名称/市场/类型筛选、「含已退市」过滤、分页跳转、7 天未同步提醒；HK/US ETF 与股票同列展示，可直接进入详情。
- **资产详情 `/assets/market/{assetKey}`**：基础信息、历史维度状态、年度收益、手动「刷新历史」（default_refresh / switch_source_full）。
- **计划选标**：新建计划向导与持仓校正直接搜索市场目录（候选项展示历史就绪状态），选择时由用户指定资产类别/区域。

## 关键不变量

- `resource_db` 对 sidecar 不可见，资源只经 Go 上传 API 写入；上传幂等（同 SHA256 重复上传仅刷新 TTL）。
- post-process 全部幂等：`market_data_versions` 版本键 + `version_no` 单调比较，重复/乱序通知不产生重复写入。
- 目录/历史/FX 落库均为单事务提交，校验失败不触碰业务表。
- 历史序列同源不变量：merge 锁定单源、full 整段替换；提交后发现混源仅告警并等待下次全量修复。
- 任务去重：`(type, dedupe_key)` 上 active 状态唯一；目录同步 dedupe key 为 `asset_directory_sync|{sync_key}`，`force` 不改变 dedupe 行为。
- 资产目录是市场身份唯一来源；生产路径不保留基于名称、代码前缀、经验枚举的身份推断。
- FIRE 资产大类只存在于计划持仓，不由 provider 或 sidecar 推断。

## 测试与验证

- Go：`go test ./...`（任务 API、内部 API、post-process 生命周期/覆盖率门槛/换源迁移、资源库、ETF 目录可搜索集成测试）。
- sidecar：`uv run pytest`（worker 循环、执行器、em_directory 分页/降级、fetch 链回归）；live 验证通过 `FIREMAN_LIVE_AKSHARE=1` / `FIREMAN_LIVE_TICKFLOW=1` 直驱 fetch 链，不再依赖已移除的 HTTP 端点。
- Web：`npm test -- --run`（目录/详情/持仓选择器，含 HK/US ETF 展示与选择用例）。
