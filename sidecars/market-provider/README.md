# fireman-market-provider

Fireman 项目的 AKShare 市场数据 sidecar。本服务对外只暴露：

- `GET /healthz`
- `POST /v1/instruments/fetch`

该服务只负责从 AKShare 获取并标准化市场数据，不承担业务指标计算或计划配置。

## 多数据源 fallback

各 `instrument_type` 会按顺序尝试多个 AKShare 接口，前一个失败或返回空数据时自动切换下一源，直至成功或全部失败：

| 类型 | 数据源顺序（节选） |
| --- | --- |
| A 股 | 东方财富 → 腾讯 → 新浪 |
| 场内 ETF | 东方财富 → 新浪 → LOF 历史 → F10 净值 |
| 公募基金 | 开放式 EM 累计/单位净值 → 货币型 EM → 理财型 EM → LOF 历史 |
| 美股 | `stock_us_daily` → `stock_us_hist` |
| 外汇 | 中行新浪 → 外汇对报价 |

响应字段 `source_name` 记录最终命中的 AKShare 函数名，便于排查。
