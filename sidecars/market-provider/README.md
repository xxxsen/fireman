# fireman-market-provider

Fireman 项目的 AKShare 市场数据 sidecar。本服务对外只暴露：

- `GET /healthz`
- `POST /v1/instruments/fetch`

该服务只负责从 AKShare 获取并标准化市场数据，不承担业务指标计算或计划配置。
