# td/032、td/033 实施 Review

## 1. Review 结论

本次实现已完成月度收益率、完整年度指标、区间收益率、短历史模拟、基金名称缓存及部分超时治理等主体工作，后端与 Web 现有测试均通过。

但当前仍存在 6 项 P1 缺陷和 6 项 P2 缺陷。其中，基金类型判定仍可能把场外基金误识别为场内基金，请求链路仍可能突破既定 deadline，fetch 阶段仍会重新请求名称数据，模拟运行时也未完整校验冻结快照语义。因此：

- `td/032` 尚未完整实施。
- `td/033` 尚未完整实施。
- 本次不将两份文档迁移到 `docs/`。
- 应完成本文全部 P1、P2 修复并重新 review 后再归档。

## 2. Review 范围

- `td/032-td031-implementation-review.md`
- `td/033-monthly-volatility-and-limited-history-simulation.md`
- 当前工作区中与上述方案相关的 Go、Web、sidecar、数据库及测试变更

## 3. P1 缺陷

### P1-1 场内基金降级逻辑仍可能把场外基金误判为 ETF

**位置**

- `sidecars/market-provider/fireman_market_provider/adapters/resolve.py`
  - `_SH_FUND_PREFIXES`
  - `_SZ_FUND_PREFIXES`
  - `_fallback_bare_cn_exchange_fund`
  - `_resolve_cn_exchange_fund`
- `sidecars/market-provider/fireman_market_provider/adapters/names.py`
  - `lookup_cn_exchange_fund_name`

**问题**

当 ETF/LOF spot 数据源加载失败时，当前逻辑会根据 `15/16/17/18` 等代码前缀推断场内基金，再通过雪球名称查询补齐名称并返回 `index_etf` 候选。

代码前缀和雪球返回的基金名称都不能证明资产具有场内交易身份。部分 `16/17/18` 开头的场外基金可能因此被错误导入为 ETF。现有回归测试只覆盖了 `270042`，没有覆盖其他前缀场外基金在 spot 部分失败或全部失败时的行为。

**修复方案**

移除“代码前缀 + 雪球名称”作为场内基金身份依据的降级路径。只有 ETF/LOF 权威列表、带明确市场标识的权威数据源或已验证的场内缓存记录才能生成 `cn_exchange_fund` 候选。

雪球或场外基金名称信息只能用于确认 `instrument_type_mismatch`，不得用于创建场内基金候选。权威场内数据源超时时，应返回稳定的 `market_provider_timeout`；只有已经从权威数据中得到多个合法身份时才能返回歧义结果。

**验收逻辑**

1. 选择具有 `16/17/18` 前缀的场外基金，模拟任一及全部 ETF/LOF spot 数据源超时。
2. 结果必须是 `instrument_type_mismatch` 或 `market_provider_timeout`，不得返回沪深 ETF/LOF 候选。
3. 权威列表中的真实 ETF 和 LOF 在相同条件下仍能正确解析。
4. `270042` 始终按场外基金处理，且能获取基金名称。

### P1-2 resolve 请求仍可能同步刷新完整基金名称表并突破 deadline

**位置**

- `sidecars/market-provider/fireman_market_provider/adapters/resolve.py`
  - `_maybe_raise_mutual_fund_mismatch`
- `sidecars/market-provider/fireman_market_provider/adapters/names.py`
  - `lookup_cn_mutual_fund_name`
  - `_load_mutual_fund_name_map`
  - `_refresh_mutual_fund_name_map_sync`
  - `_fetch_mutual_fund_name_map_from_upstream`

**问题**

类型不匹配检查会调用 `lookup_cn_mutual_fund_name`。当内存或磁盘缓存失效时，该调用会同步执行完整 `fund_name_em` 刷新，并使用独立的 60 秒超时，而不是请求剩余 deadline。

因此，一个只剩少量时间的 resolve 请求仍可能额外等待 60 秒。这违背 `td/032` 中“请求路径只读缓存，完整基金名称表仅后台刷新”的约束，也可能再次造成前端、后端、sidecar 各层超时依次触发，最终表现为基金名称获取异常。

**修复方案**

将 `_maybe_raise_mutual_fund_mismatch` 改为只调用只读缓存查询和受当前 deadline 约束的单基金查询：

1. 先调用 `lookup_cn_mutual_fund_name_readonly`。
2. 未命中时调用 `resolve_cn_mutual_fund_name(deadline)`。
3. resolve 请求路径禁止调用任何会同步刷新完整基金名称表的方法。
4. 完整 `fund_name_em` 刷新只能由后台任务触发。

**验收逻辑**

1. 清空内存缓存并让磁盘缓存过期。
2. 阻塞 `fund_name_em`，执行基金 resolve。
3. 断言请求过程中 `fund_name_em` 调用次数为 0。
4. 单基金名称请求超时必须等于 `min(15 秒, 请求剩余时间)`。
5. sidecar resolve 总耗时不得超过 70 秒，且基金名称成功路径仍能返回真实名称。

### P1-3 fetch 阶段仍会发起名称网络请求

**位置**

- `sidecars/market-provider/fireman_market_provider/adapters/registry.py`
  - `_fetch_cn_exchange_fund`
  - 港股 fetch 适配器

**问题**

场内基金历史数据抓取完成后仍会调用 `resolve_cn_exchange_fund_name`，港股 fetch 也会调用 `resolve_hk_name`。这些名称解析可能重新加载 spot 数据或调用外部名称接口，并且没有继承 fetch 请求的剩余 deadline。

当前 Go 侧已经向 fetch 传递 `resolved_name`，但上述适配器没有优先使用它。这会让“resolve 已成功、fetch 历史数据也成功”的任务在最后一次名称请求上失败，重新引入反复出现的“基金抓取成功但基金名获取失败”问题。

**修复方案**

所有 fetch 适配器统一执行以下名称策略：

1. 优先使用请求中的 `resolved_name`。
2. 没有 `resolved_name` 时，只允许使用本次历史数据响应中携带的名称或只读缓存。
3. fetch 阶段禁止调用任何名称网络接口或触发名称表刷新。
4. refresh 任务继续使用数据库中已存储的名称作为 `resolved_name`。

**验收逻辑**

1. 对所有名称上游接口设置调用计数并强制失败。
2. 使用合法 `resolved_name` 执行场内基金、场外基金和港股 fetch。
3. 历史数据抓取必须成功，返回名称必须与 `resolved_name` 一致。
4. 所有名称上游接口调用次数必须为 0。
5. fetch 总耗时不得因名称处理突破 240 秒。

### P1-4 场外基金会因名称包含 LOF 而走场内 LOF 抓取接口

**位置**

- `sidecars/market-provider/fireman_market_provider/adapters/classification.py`
  - `detect_cn_mutual_fund_source_kind`
- `sidecars/market-provider/fireman_market_provider/adapters/registry.py`
  - `_fetch_cn_mutual_fund`

**问题**

当前代码只要在基金名称中发现 `LOF` 就返回 `lof` source kind。类似“LOF 联接 A”的场外基金仍属于场外基金，但会被路由到 `fund_lof_hist_em`。

同时，该分支构造的元数据名称使用代码本身，可能覆盖 resolve 阶段已经得到的真实基金名称。这既破坏资产类型边界，也会再次导致基金名丢失。

**修复方案**

从 `cn_mutual_fund` 的 source kind 中移除 `lof`：

- `cn_mutual_fund` 只允许根据权威元数据选择 open、money、financial 等场外基金数据源。
- 真正的 LOF 必须在 resolve/import 阶段被权威场内数据识别为 `cn_exchange_fund`，并由场内适配器抓取。
- 场外基金 fetch 始终保留请求中的 `resolved_name`。

**验收逻辑**

1. 使用名称包含“LOF 联接”的场外基金执行 fetch。
2. 必须调用场外开放式基金接口，不得调用 `fund_lof_hist_em`。
3. 抓取结果必须保留真实基金名称。
4. 权威列表中的真实 LOF 必须通过 `cn_exchange_fund` 路径成功抓取。

### P1-5 模拟运行时未完整校验冻结快照

**位置**

- `internal/service/simulation_snapshot_build.go`
  - 模拟输入构建逻辑
- `internal/marketdata/eligibility.go`
  - `EvaluateSimulationEligibility`

**问题**

当前模拟输入只检查非现金资产的 `MonthlyReturnCount >= 12`，没有验证：

- `quality_status` 是否为 `available`
- `metrics_version` 是否为 `monthly_log_return_v1`
- `volatility_method` 是否为约定的月度年化算法
- `monthly_return_count` 是否严格等于 `complete_year_count * 12`
- `complete_year_count` 是否至少为 1

因此，旧版本、字段不一致或人工构造的异常快照仍可能进入模拟，导致 UI 展示、快照语义和实际模拟计算不一致。

**修复方案**

新增唯一的 `ValidateSimulationSnapshot` 校验入口：

1. 非现金资产严格校验状态、版本、算法、完整年度数和月收益数一致性。
2. `monthly_return_count` 必须等于 `complete_year_count * 12`。
3. 系统现金资产使用独立的固定收益快照约束，不套用市场历史数据规则。
4. 快照创建和模拟输入构建必须复用同一校验函数。
5. `EvaluateSimulationEligibility` 使用相同规则，不再只判断月收益数量下限。

**验收逻辑**

1. 2 个完整年度但只有 12 个月收益的快照必须被拒绝。
2. 旧 `metrics_version`、错误 `volatility_method`、不可用状态快照必须被拒绝。
3. 1 个完整年度且正好 12 个月收益的快照可以运行模拟。
4. 系统现金资产不依赖市场历史数据，仍能正常进入模拟。
5. API 返回稳定、可定位具体资产的校验错误。

### P1-6 计划页面展示的是资产资料库实时指标，而不是计划冻结快照

**位置**

- `internal/repository/holdings.go`
- `web/app/plans/[id]/parameters/page.tsx`
- `web/app/plans/[id]/analysis/page.tsx`

**问题**

计划参数页和模拟前提示通过 `listInstruments` 获取资产资料库中的最新 `history_depth`、完整年度数、月收益数和指标版本，只从计划快照中读取创建时间。

当资产资料库刷新或修正后，页面会向用户展示最新指标，但实际模拟仍使用计划创建时冻结的旧快照。用户无法确认本次模拟真正使用了哪组数据，短历史警告也可能与模拟输入不一致。

**修复方案**

扩展计划持仓查询和 API DTO，直接返回该计划冻结快照中的：

- `complete_year_count`
- `monthly_return_count`
- `history_depth`
- `metrics_version`
- 解析后的 `warnings`
- `created_at`

计划参数页和模拟前提示只能使用这些冻结字段。资产资料库最新指标不得混入计划快照区域；计划执行显式同步后，冻结字段再整体更新。

**验收逻辑**

1. 创建计划并记录冻结指标。
2. 刷新资产资料库，使最新指标发生变化。
3. 计划参数页和模拟前提示仍显示原冻结指标和警告。
4. 模拟使用的数据与页面展示完全一致。
5. 执行显式同步后，页面和模拟输入同时更新为新快照。

## 4. P2 缺陷与实现缺失

### P2-1 Web 超时层级未按方案闭环

**位置**

- `web/next.config.ts`
- `web/lib/api/client.ts`
- 资产导入页面 resolve 调用

**问题**

当前 Next proxy timeout 被设置为 105 秒，但没有浏览器操作级 105 秒超时。`td/032` 要求的层级是：

`sidecar 70 秒 < Go 90 秒 < Web 操作 105 秒 < Next proxy 120 秒`

当前实现将 Web 操作超时和代理超时混为一层，浏览器也无法稳定识别本次请求是操作超时还是普通网络异常。

**修复方案**

将 Next proxy timeout 恢复为 120 秒；在 Web API client 中支持可选的 `AbortSignal.timeout`，资产 resolve/import 操作使用 105 秒，并将 `AbortError` 映射为稳定的 `market_provider_timeout` 提示。其他 API 不继承该全局超时。

**验收逻辑**

1. 自动化测试断言四层超时严格递增。
2. sidecar 卡住时，Go 在 90 秒内结束，浏览器显示可重试的上游超时提示。
3. Go 未响应时，Web 操作在 105 秒结束。
4. Next proxy 不早于 Web 操作超时关闭连接。

### P2-2 超时可观测字段未完整实现

**问题**

方案要求关键日志统一包含 `operation`、`symbol`、`elapsed_ms`、`remaining_ms`、`layer`。当前 resolve、fallback、fetch 等日志没有稳定输出全部字段，发生 deadline 问题时仍难以判断是哪一层耗尽时间。

**修复方案**

为 sidecar、Go marketdata client 和 Web operation timeout 建立统一结构化日志字段；所有外部数据源调用、降级和 deadline 结束事件必须输出上述字段。

**验收逻辑**

构造 resolve 超时、fetch 超时和 Web 主动取消三种场景，日志必须能按同一 symbol 串联请求，并明确显示各层耗时与剩余时间。

### P2-3 历史快照 API 与 UI 信息不完整

**位置**

- `internal/repository/snapshot.go`
- `web/lib/api/instruments.ts`
- `web/app/assets/[id]/page.tsx`

**问题**

历史快照仍直接暴露 `warnings_json` 字符串，前端类型和页面只展示纳入日期、完整年度和创建时间，缺少方案要求的月收益观测数、历史深度、指标版本和结构化警告。

**修复方案**

由 service 层输出专用历史快照 DTO，将 `warnings_json` 解析为字符串数组，并完整返回、展示 `complete_year_count`、`monthly_return_count`、`history_depth`、`metrics_version`、`warnings` 和 `created_at`。

**验收逻辑**

API contract test 校验全部字段及警告解析；页面测试校验短历史快照的观测数、版本和警告均可见。

### P2-4 区间收益率的顶层基准日期可能与实际计算日期不一致

**位置**

- `internal/marketdata/trailing_returns.go`

**问题**

请求日期为周末、节假日或晚于最新净值日期时，区间计算会使用最近一个有效数据点，但顶层 `as_of_date` 仍返回请求日期。页面标题因此可能显示周日，而各周期实际以周五净值计算。

**修复方案**

找到 `endPoint` 后，将顶层 `as_of_date` 设置为 `endPoint.TradeDate`。所有周期的 `end_date` 与顶层日期保持一致。

同时使用 `sort.Slice` 完成一次排序，并使用 `sort.Search` 查找各周期起止点，移除当前 O(n²) 冒泡排序和重复线性扫描。

**验收逻辑**

1. 周日请求、周五为最新数据时，顶层和各周期 `end_date` 均为周五。
2. 长历史数据下各周期结果与现有正确样本一致。
3. 增加大数据量 benchmark，确认排序和查找复杂度不再为 O(n²)。

### P2-5 零完整年度被标记为 `one_year`

**位置**

- `internal/marketdata/eligibility.go`
  - `DetermineHistoryDepth`

**问题**

当前默认分支会把 0 个完整年度标记为 `one_year`。虽然模拟资格仍可能被拒绝，但 API 和 UI 会错误表达为“具有一年历史”。

**修复方案**

新增明确的 `insufficient` 历史深度，0 个完整年度返回该值；后端 DTO、前端类型和文案同步支持该状态。

**验收逻辑**

0 个完整年度的资产必须显示“历史不足”，不得显示“一年历史”；1、2、3、5 个完整年度分别映射到约定的正确深度。

### P2-6 Go 代码未通过 gofmt

**位置**

包括但不限于：

- `internal/marketdata/constants.go`
- `internal/marketdata/snapshot.go`
- `internal/marketdata/source_hash.go`
- `internal/marketdata/types.go`
- `internal/repository/models.go`
- `internal/service/instrument_service.go`
- `internal/service/plan_instrument.go`
- `internal/service/simulation_snapshot_build.go`

**问题**

本次新增和修改的部分 Go 文件存在 gofmt 差异。现有测试能通过，但代码尚未满足仓库基础格式要求。

**修复方案**

对本次涉及的全部 Go 文件执行 `gofmt -w`，并在 CI/lint 中保留格式校验。

**验收逻辑**

`gofmt -d` 对本次变更的 Go 文件无输出，`git diff --check` 通过。

## 5. 测试基础设施问题

### Sidecar 完整 pytest 当前无法完成

**现象**

- 完整 pytest 在 360 秒内无结果。
- 排除 subprocess/live 的测试仍在 120 秒超时。
- 单独执行 `tests/test_app.py::test_healthz_returns_ok` 也会卡住。
- 堆栈停留在 Starlette `TestClient` 的事件 portal。

当前环境版本包括：

- FastAPI `0.136.3`
- Starlette `1.2.1`
- httpx `0.28.1`
- anyio `4.13.0`

Starlette `1.2.1` 的 TestClient 优先使用 `httpx2`，当前 `pyproject.toml` 开发依赖只有 `httpx>=0.28.0`。这使 sidecar 完整回归测试无法作为可靠验收门禁。

**修复方案**

将 sidecar 测试客户端依赖切换为与 Starlette `1.2.1` 匹配的 `httpx2`，更新并提交 `uv.lock`，由锁文件固定完整测试依赖组合。TestClient 测试继续使用 Starlette 官方适配入口，不在测试中自行维护 ASGI 启动逻辑。

**验收逻辑**

1. `tests/test_app.py::test_healthz_returns_ok` 在 2 秒内完成。
2. 完整 `.venv/bin/pytest -q` 能正常结束。
3. 测试输出不再包含 Starlette 回退到旧 httpx 的弃用警告。
4. CI 为 sidecar pytest 设置有界总超时，超时视为失败。

## 6. 已验证内容

以下检查已通过：

- `go test ./...`
- `go test -count=1 ./internal/marketdata ./internal/service ./internal/api ./internal/simulation ./internal/jobs`
- Web `npm test -- --run`：43 个测试文件、186 个测试通过
- Web `npm run lint`
- Web `npm run build`
- `git diff --check`
- sidecar 纯函数定向测试：
  - `tests/test_names.py`
  - `tests/test_names_negative_cache.py`
  - `tests/test_timeout_util.py`
  - `tests/test_cn_code.py`
  - `tests/test_cn_code_bj_lof.py`
  - `tests/test_timeout_elapsed.py`

Sidecar 完整 pytest 未通过，原因见“测试基础设施问题”。

## 7. 下一轮实施完成标准

下一轮实现必须同时满足：

1. 完成全部 P1、P2 修复。
2. 基金 resolve/fetch 全链路不再以名称或代码前缀推断场内身份。
3. fetch 阶段不发生任何名称网络请求。
4. 四层 deadline 严格递增，所有子调用受剩余时间约束。
5. 计划页面、警告和模拟统一使用同一份冻结快照。
6. sidecar、Go、Web 全量测试和 lint 均可稳定完成。
7. 新增针对本次每项缺陷的自动化回归测试。

完成后再对 `td/032`、`td/033` 进行归档 review。
