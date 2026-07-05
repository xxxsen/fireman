# fireman-market-provider

Fireman 项目的市场数据 sidecar（AKShare 为主上游，TickFlow 为可选优先行情源）。在市场数据任务化架构下，本服务是**纯任务 worker**：

- 对外 HTTP 仅保留 `GET /healthz`（存活探针）；
- 不再提供同步 fetch / resolve / metadata HTTP API（旧 `POST /v1/instruments/fetch`、`POST /v1/instruments/resolve`、`POST /v1/metadata/refresh` 已全部移除，请求这些路径会得到 404）；
- 市场数据统一通过任务链路流转：Go 在主库 `worker_tasks` 表创建任务 → worker 轮询认领并执行 → 结果 gzip 后经 Go 内部接口 `POST /internal/resources` 上传（`resource_db` 归 Go 层所有，sidecar 不可见）→ 任务置 `pre_complete` 并通知 Go `POST /internal/tasks/{id}/post-process` 完成业务落库。

该服务只负责获取并标准化市场数据，不承担业务指标计算或计划配置。

## worker 任务类型

| 任务类型 | 说明 |
| --- | --- |
| `asset_directory_sync` | 按 scope（`cn_all` / `hk_all` / `us_all`）全量同步资产目录快照，覆盖 A 股、场内 ETF/LOF、场外基金、港股、港股 ETF、美股、美股 ETF |
| `asset_history_sync` | 按 (asset_key, adjust_policy, point_type) 维度同步历史行情；同源增量 merge 或全量替换（source-pinned 语义） |
| `fx_rate_sync` | 同步系统汇率（`USDCNY` / `HKDCNY`）全量历史 |

worker 生命周期：claim（`pending -> running`，`BEGIN IMMEDIATE` 原子认领）→ 每 10s 心跳 → 执行完成后上传资源并 CAS `running -> pre_complete` → 通知 Go post-process，按指数退避重试（最多 10 次，间隔上限 5 分钟，`pre_complete` 硬超时 1 小时）→ 依据 Go 返回的 success / retryable_error / permanent_error 驱动 `complete` / 退避重试 / `failed` 终态。心跳超时的 running 任务由 janitor 标记失败。

## 目录同步数据源

| 类别 | 数据源 |
| --- | --- |
| A 股 | `ak.stock_zh_a_spot_em` |
| CN 场内 ETF/LOF | `ak.fund_etf_spot_em` / `ak.fund_lof_spot_em` |
| CN 场外基金 | `ak.fund_name_em` |
| 港股（主板+创业板，含 REIT） | `em.hk_equity_list` / `em.hk_fund_list`（东方财富 clist 分类板） |
| 港股 ETF | `em.hk_fund_list`（剔除 REIT/信托，含 L&I 与 -U/-R 货币柜台） |
| 美股（普通股/优先股/ADR/CEF） | `em.us_equity_list` |
| 美股 ETF | `em.us_etf_list` |

`em_*` 操作是对东方财富 quote-center `qt/clist` 接口的直连封装（AKShare 未提供 HK/US ETF 列表函数），与 AKShare 调用一样经由硬超时子进程执行。

## 历史行情多数据源 fallback

`asset_history_sync` 无 `required_source_name` 时按序尝试多个上游，前一个失败或返回空数据时自动切换下一源；`required_source_name` 固定时只调用该源，失败即 `source_unavailable`，绝不换源：

| 类型 | 数据源顺序（节选） |
| --- | --- |
| A 股 | （TickFlow 日K，可选）→ 东方财富 → 腾讯 → 新浪 |
| 场内 ETF | （TickFlow 日K，可选）→ 东方财富 → 腾讯 → 新浪 → F10 净值 |
| 场内 LOF | 东方财富 LOF → 腾讯（不接入 TickFlow） |
| 公募基金 | 开放式 EM 累计/单位净值 → 货币型 EM → 理财型 EM（不接入 TickFlow） |
| 港股 / 港股 ETF | `stock_hk_hist` → `stock_hk_daily` |
| 美股 / 美股 ETF | `stock_us_daily` → `stock_us_hist` |
| 外汇 | 中行新浪 → 外汇对报价 |

结果字段 `source_name` 记录最终命中的数据源（如 `ak.fund_etf_hist_em`、`tickflow.klines:1d`），便于排查。

## TickFlow 优先策略

TickFlow 通过官方 `tickflow` Python SDK 接入，仅作为**已解析交易所标的**的优先历史行情源，默认关闭：

- 只服务 `cn_exchange_stock` 与 `cn_exchange_fund` 中已解析为 `etf` / `index_etf` 的标的；
- LOF、kind 为空/未知的场内基金、公募基金（`cn_mutual_fund`）永远不走 TickFlow；
- 第一阶段仅在 `adjust_policy=none`（未复权）时启用，避免复权口径误用；
- 名称、费用、分类均不使用 TickFlow。

### free / paid 两种模式

- **free**：不配置 API key 时，客户端指向免费 API（默认 `https://free-api.tickflow.org`），仅提供盘后日 K；
- **paid**：配置 `MARKET_PROVIDER_TICKFLOW_API_KEY` 后，客户端自动指向付费 API（默认 `https://api.tickflow.org`），由 SDK 通过 `x-api-key` 请求头鉴权；
- `MARKET_PROVIDER_TICKFLOW_BASE_URL` 可显式覆盖上述两个默认地址；
- API key 只允许通过环境变量注入，不得写入代码、文档、compose 文件或测试 fixture；日志与错误信息中也不会出现 key。

### fallback 规则

TickFlow 出现以下任一情况视为“未命中”，自动回退 AKShare 链，不产生最终错误：
SDK 连接/超时/API/限流异常、payload 结构异常、`timestamp` 为空、OHLC 数组长度不一致、
日期无法转换、过滤到请求区间后为空、返回 symbol 与请求不一致、疑似截断
（单次响应填满 SDK 上限 10000 根且最早日期晚于请求起点）。日志记录
`source_code / tickflow_symbol / instrument_type / instrument_kind / adjust_policy / fallback_reason`。

### 配置项

| 环境变量 | 默认 | 说明 |
| --- | --- | --- |
| `FIREMAN_WORKER_ENABLED` | `true` | 是否启动 worker 循环（claim/heartbeat/janitor/notify） |
| `FIREMAN_DB_PATH` | `/data/fireman.db` | 主库路径（`worker_tasks` 所在 SQLite） |
| `FIREMAN_INTERNAL_API_URL` | `http://backend:8081` | Go 内部接口地址（资源上传 + post-process 通知） |
| `MARKET_PROVIDER_TICKFLOW_ENABLED` | `false` | 是否启用 TickFlow 优先 fetch |
| `MARKET_PROVIDER_TICKFLOW_API_KEY` | 空 | sidecar 专用 TickFlow API key；非空时默认走付费 API |
| `MARKET_PROVIDER_TICKFLOW_BASE_URL` | 空 | 显式覆盖 TickFlow API 地址（优先级最高） |
| `MARKET_PROVIDER_TICKFLOW_FREE_BASE_URL` | `https://free-api.tickflow.org` | 无 API key 时使用的免费 API 地址 |
| `MARKET_PROVIDER_TICKFLOW_PAID_BASE_URL` | `https://api.tickflow.org` | 有 API key 时默认使用的付费 API 地址 |
| `MARKET_PROVIDER_TICKFLOW_TIMEOUT` | `8`（秒） | SDK 单次请求超时 |
| `MARKET_PROVIDER_TICKFLOW_MAX_RETRIES` | `0` | SDK 内部重试次数；已有 AKShare fallback，优先源不应长时间重试 |
| `MARKET_PROVIDER_TICKFLOW_TYPES` | `cn_exchange_stock,cn_exchange_fund` | 允许启用的类型 |
| `MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE` | `true` | 强制仅未复权数据走 TickFlow |
| `MARKET_PROVIDER_STARTUP_WARM_ENABLED` | `true` | 是否在启动时后台预热场外基金名称缓存 |
| `FIREMAN_DISABLE_STARTUP_WARM` | 空 | 设为 `1` 时强制关闭启动预热（测试用 kill switch） |

### 测试与网络隔离

默认 `uv run pytest` 完全离线（`pyproject.toml` 已配置 `-m 'not live'`）：

- conftest 会自动 monkeypatch `requests.get/post/...`、`requests.Session.request`
  和 TickFlow SDK 的 HTTP 层，任何未 mock 的真实网络调用会立即失败并提示使用
  `tests/testdata` fixture；
- conftest 自动注入 `FIREMAN_WORKER_ENABLED=false` 与
  `FIREMAN_DISABLE_STARTUP_WARM=1`，启动 app 不会触发 worker 循环或名称缓存预热；
- 上游录制数据放在 `tests/testdata/*.json.gz`（gzip JSON），通过
  `tests/dataload.py` 的 `load_json_gz` / `load_dataframe_gz` 加载；
- AKShare 调用必须经 `register_test_dispatch` 或 `unittest.mock.patch` 显式 mock。

### Live 验证

Live 测试会请求真实上游（AKShare / Eastmoney / TickFlow），有配额与被限流风险，
只应在本地显式运行；必须同时带上 `-m live` marker 和对应环境变量才会执行。
Live 验证不再经过 HTTP fetch 端点（已移除），直接驱动 worker 执行器 / fetch 链：

```bash
# AKShare fetch 链 smoke（tests/fetch_compat.py 直接调用 adapters.registry.fetch_instrument）
FIREMAN_LIVE_AKSHARE=1 uv run pytest -m live tests/test_live_smoke.py

# TickFlow 优先链路 smoke / 对账（走 fetch 链与 worker history 执行器语义一致的路径）
FIREMAN_LIVE_TICKFLOW=1 MARKET_PROVIDER_TICKFLOW_ENABLED=true \
  uv run pytest -m live tests/test_tickflow_live.py
```

TickFlow live 覆盖场内样本 smoke（`510300.SH` / `159915.SZ` / `600000.SH` / `000001.SZ`）、
`600036.SH` 单次全历史拉取（2002-04-09 起、>5800 根）、场外基金不支持确认（`110022.OF`）、
TickFlow×AKShare 最近 60 交易日收盘对账（日期交集 ≥95%、相对误差 ≤0.5%）以及
TickFlow 不可用时的 AKShare 回退。额外在本地环境导出
`MARKET_PROVIDER_TICKFLOW_API_KEY` 时会追加付费 API smoke。
