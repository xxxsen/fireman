# td/037 实施 Review

## 1. 结论

本轮实现已经完成了 `td/037` 中一部分高优先级工作：

- sidecar 错误返回改为结构化 `error_code`
- Go 侧已改为按结构化错误分类，不再依赖 message 子串
- 异步导入链路已经把 `instrument_kind` 传入 fetch，请 sidecar 按 identity-safe 源集合选源
- 已新增错误契约、fetch identity、名称自愈等定向测试

但当前实现仍未完整关闭 `td/037`，至少还有 1 项 P1 和 1 项 P2：

- `P1`：已有资产的 refresh 路径仍未传递 `instrument_kind`，identity-safe fetch 只覆盖了 import job，没有覆盖 refresh
- `P2`：sidecar 仍使用 `@app.on_event("startup")`，且请求体验证错误仍走 FastAPI 默认 `422`，没有纳入统一结构化错误契约

因此：

- `td/037` 不能视为完成；
- 本次 review 结果继续保留在 `td/`；
- 不迁移到 `docs/`。

## 2. Review 范围

- [td/037-sidecar-stability-refactor-plan.md](/home/sen/work/fireman/td/037-sidecar-stability-refactor-plan.md)
- 当前工作区中与 `td/037` 对应的 Go / sidecar / 测试变更

## 3. Findings

### P1-1 refresh 路径仍未携带 `instrument_kind`，已有资产刷新仍可能走 legacy full chain 混用数据

**位置**

- [instrument_service.go](/home/sen/work/fireman/internal/service/instrument_service.go:473)
- [registry.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/registry.py:238)

**问题**

本轮确实把 `instrument_kind` 打通到了异步导入 job：

- `InstrumentFetchPayload` 已新增 `instrument_kind`
- `InstrumentFetchRunner.Run()` 构造 fetch 请求时会带上 `InstrumentKind`

但同步 refresh 路径 `fetchAndProcessForInstrument()` 仍然只传：

- `market`
- `instrument_type`
- `source_code`
- `adjust_policy`
- `resolved_name`

没有传 `instrument_kind`。

而 sidecar 的 `_fetch_cn_exchange_fund()` 只有在 `req.instrument_kind` 为：

- `lof`
- `etf`
- `index_etf`

时，才会启用 identity-consistent source set。否则会退回 legacy full chain：

- `ETF -> TX -> SINA -> LOF -> ETF_INFO`

这意味着：

1. 新导入资产的首次抓取已经部分安全；
2. 但已有资产 refresh 仍可能在同码 `ETF / LOF / stock` 场景下跨类型回退；
3. `td/037` 现象-4“name 用 A、净值用 B”的风险在 refresh 上仍然存在。

这不是测试空白，而是实际链路未闭合。

**修复方案**

把 refresh 路径也纳入同一套 resolved identity 约束。

实现要求：

1. 在 instruments 表或可等价恢复的持久化位置保存该资产的 resolved `instrument_kind`；
2. `fetchAndProcessForInstrument()` 构造 fetch 请求时必须带上该 `instrument_kind`；
3. sidecar refresh 请求不得再走“unknown kind -> legacy full chain”；
4. 对历史遗留、确实缺少 `instrument_kind` 的资产，先通过一次受控 resolve/self-heal 补齐 identity，再允许 refresh。

**验收逻辑**

1. 构造一个同码冲突样本，例如可区分 ETF/LOF 的 6 位裸码。
2. 先导入为明确 `ETF`，持久化其 `instrument_kind=index_etf`。
3. 模拟 refresh：
   - `ETF` 源空
   - `LOF` 源有数据
4. 断言 refresh 必须失败为身份冲突或源不可用：
   - 不得回退到 `LOF` 数据成功
   - 不得更新市场数据
5. 对 `LOF` 资产重复同样测试，断言不会回退到 `ETF` 数据。
6. 增加 Go 集成测试，覆盖 refresh 链路下 `instrument_kind` 的传递与约束。

### P2-1 sidecar 结构化错误契约仍未完全覆盖请求体验证和启动生命周期

**位置**

- [app.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/app.py:59)
- [test_error_contract.py](/home/sen/work/fireman/sidecars/market-provider/tests/test_error_contract.py:130)

**问题**

`td/037` 方案要求统一结构化错误契约，并明确指出：

- sidecar 应成为错误契约单一事实源
- 生命周期应迁移到 FastAPI `lifespan`

但当前还留有两个缺口：

1. `@app.on_event("startup")` 仍在使用，没有迁移到 `lifespan`
2. 非法 `metadata/refresh` 请求仍由 FastAPI/Pydantic 直接返回默认 `422`，测试也明确承认“未进入统一 handler”

这意味着当前“统一错误契约”只覆盖了业务异常，不覆盖请求体验证错误；同时生命周期改造也还未落实。

**修复方案**

完成 sidecar 入口层收敛：

1. 将 `startup` 预热逻辑迁移到 `lifespan`
2. 为 `RequestValidationError` 增加统一异常处理，输出同样的结构化 envelope：
   - `code`
   - `error_code=invalid_request`
   - `message`
   - `data=null`
3. 调整相关契约测试，不再接受 FastAPI 默认 `422` body

**验收逻辑**

1. 向 `/v1/metadata/refresh` 发送非法 `target`：
   - HTTP 状态可保持 `422` 或转 `400`，但 body 必须为统一结构化 envelope
   - `error_code` 必须是 `invalid_request`
2. 对 resolve/fetch/metadata 三个入口分别构造请求体验证失败，断言 body 结构完全一致。
3. 启动 sidecar 时不再出现 `on_event` 弃用告警。
4. 原有预热逻辑保持不变：
   - 进程启动后仍会异步预热基金名缓存
   - `/healthz` 不被预热阻塞

## 4. 已完成项

以下改动本轮已确认有效：

- sidecar `ProviderError` / `error_code` 契约
- Go `classifyProviderError()` 与 typed predicate 分类
- `defaultHTTP=30s` 陷阱已移除
- `candidate_id` 已在 Go 侧 DTO 中落地，不再 silent drop
- 名称自愈策略 `shouldUpgradeInstrumentName()` 已接入 refresh 更新逻辑

## 5. 测试情况

已执行并通过：

- `go test ./internal/marketdata ./internal/service ./internal/jobs`

本轮未完整执行 sidecar pytest：

- 当前环境直接 `pytest` 仍缺 Python 运行依赖
- 受网络限制，无法可靠拉起完整 `uv run pytest`

因此本次 review 结论基于代码路径核查与新增测试覆盖分析。

## 6. 总结

`td/037` 已经完成了“错误契约结构化”和“异步导入链路 identity-safe fetch”的一大半工作，但 refresh 身份闭环仍未完成，sidecar 入口层契约也还没完全统一。本轮不能归档。
