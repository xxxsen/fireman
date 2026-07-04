# fireman-market-provider

Fireman 项目的市场数据 sidecar（AKShare 为主上游，TickFlow 为可选优先行情源）。本服务对外暴露：

- `GET /healthz`
- `POST /v1/instruments/fetch`
- `POST /v1/instruments/resolve`
- `POST /v1/metadata/refresh`

该服务只负责获取并标准化市场数据，不承担业务指标计算或计划配置。

## 多数据源 fallback

各 `instrument_type` 会按顺序尝试多个上游接口，前一个失败或返回空数据时自动切换下一源，直至成功或全部失败：

| 类型 | 数据源顺序（节选） |
| --- | --- |
| A 股 | （TickFlow 日K，可选）→ 东方财富 → 腾讯 → 新浪 |
| 场内 ETF | （TickFlow 日K，可选）→ 东方财富 → 腾讯 → 新浪 → F10 净值 |
| 场内 LOF | 东方财富 LOF → 腾讯（不接入 TickFlow） |
| 公募基金 | 开放式 EM 累计/单位净值 → 货币型 EM → 理财型 EM（不接入 TickFlow） |
| 美股 | `stock_us_daily` → `stock_us_hist` |
| 外汇 | 中行新浪 → 外汇对报价 |

响应字段 `source_name` 记录最终命中的数据源（如 `ak.fund_etf_hist_em`、`tickflow.klines:1d`），便于排查。

## TickFlow 优先策略

TickFlow 通过官方 `tickflow` Python SDK 接入，仅作为**已解析交易所标的**的优先历史行情源，默认关闭：

- 只服务 `cn_exchange_stock` 与 `cn_exchange_fund` 中已解析为 `etf` / `index_etf` 的标的；
- LOF、kind 为空/未知的场内基金、公募基金（`cn_mutual_fund`）永远不走 TickFlow；
- 第一阶段仅在 `adjust_policy=none`（未复权）时启用，避免复权口径误用；
- 资产解析（resolve）、名称、费用、分类均不使用 TickFlow。

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
| `MARKET_PROVIDER_TICKFLOW_ENABLED` | `false` | 是否启用 TickFlow 优先 fetch |
| `MARKET_PROVIDER_TICKFLOW_API_KEY` | 空 | sidecar 专用 TickFlow API key；非空时默认走付费 API |
| `MARKET_PROVIDER_TICKFLOW_BASE_URL` | 空 | 显式覆盖 TickFlow API 地址（优先级最高） |
| `MARKET_PROVIDER_TICKFLOW_FREE_BASE_URL` | `https://free-api.tickflow.org` | 无 API key 时使用的免费 API 地址 |
| `MARKET_PROVIDER_TICKFLOW_PAID_BASE_URL` | `https://api.tickflow.org` | 有 API key 时默认使用的付费 API 地址 |
| `MARKET_PROVIDER_TICKFLOW_TIMEOUT` | `8`（秒） | SDK 单次请求超时 |
| `MARKET_PROVIDER_TICKFLOW_MAX_RETRIES` | `0` | SDK 内部重试次数；已有 AKShare fallback，优先源不应长时间重试 |
| `MARKET_PROVIDER_TICKFLOW_TYPES` | `cn_exchange_stock,cn_exchange_fund` | 允许启用的类型 |
| `MARKET_PROVIDER_TICKFLOW_REQUIRE_ADJUST_NONE` | `true` | 强制仅未复权数据走 TickFlow |

### Live 验证

```bash
FIREMAN_LIVE_TICKFLOW=1 MARKET_PROVIDER_TICKFLOW_ENABLED=true \
  uv run pytest -m live tests/test_tickflow_live.py
```

覆盖场内样本 smoke（`510300.SH` / `159915.SZ` / `600000.SH` / `000001.SZ`）、
`600036.SH` 单次全历史拉取（2002-04-09 起、>5800 根）、场外基金不支持确认（`110022.OF`）、
TickFlow×AKShare 最近 60 交易日收盘对账（日期交集 ≥95%、相对误差 ≤0.5%）以及
TickFlow 不可用时的 AKShare 回退。额外在本地环境导出
`MARKET_PROVIDER_TICKFLOW_API_KEY` 时会追加付费 API smoke。
