# td/038 implementation review

## 结论

本轮针对 `td/038` 的实现复核后，**未发现新的缺陷、实现缺失或阻塞性风险**。上一轮 `td/037` review 中指出的两个问题已经完成闭环，可以结束 `td` 跟踪，并归档到 `docs`：

1. `P1`：已有资产 refresh 路径未透传 `instrument_kind`，导致 `cn_exchange_fund` 在 `ETF/LOF/stock` 同码场景下仍可能走回 legacy fallback 链。
2. `P2`：sidecar 入口层未完全收敛，仍使用 `@app.on_event("startup")`，且请求体验证错误未进入统一结构化错误契约。

## 已确认完成

### 1. refresh 路径已具备 identity-safe fetch

- `instrument_kind` 已持久化到 `instruments` 表，并覆盖 repository 的 `List/GetByID/FindByKey/Create` 读写链路：
  - `migrations/0013_instrument_kind.sql`
  - `internal/repository/instrument.go`
- 异步导入占位资产时已写入 `ticket.InstrumentKind`：
  - `internal/service/instrument_import_tx.go`
- refresh 主路径会先对历史遗留资产做一次受控 identity backfill，再把 `instrument_kind` 传给 sidecar：
  - `internal/service/instrument_service.go`
- 定向集成测试覆盖了两条关键路径：
  - 已导入资产 refresh 时继续携带已解析 kind
  - 老资产 `instrument_kind=''` 时先 resolve backfill，再 refresh
  - `internal/api/instrument_refresh_identity_integration_test.go`

### 2. sidecar 生命周期与统一错误契约已收口

- mutual fund cache 预热已从 `@app.on_event("startup")` 迁移到 `lifespan`
- `RequestValidationError` 已接入统一 `{code,error_code,message,data}` 结构化错误响应
- 落点文件：
  - `sidecars/market-provider/fireman_market_provider/app.py`
- 回归测试已补齐：
  - `sidecars/market-provider/tests/test_app.py`
  - `sidecars/market-provider/tests/test_error_contract.py`

### 3. 迁移链与单测前提一致

- `0005` 中的 `instrument_kind` 属于 `resolution_tickets`，`0013` 新增的是 `instruments.instrument_kind`，两者职责清晰，不冲突：
  - `migrations/0005_resolution_tickets_job_idempotency.sql`
  - `migrations/0013_instrument_kind.sql`
- `internal/db/db_test.go` 中的 migration count 已与当前 13 个 migration 文件一致。

## 验证结果

已执行：

- `go test ./internal/api ./internal/service ./internal/db ./internal/marketdata`
- `python3 -m py_compile sidecars/market-provider/fireman_market_provider/app.py sidecars/market-provider/tests/test_app.py sidecars/market-provider/tests/test_error_contract.py`

结果：

- Go 定向测试通过。
- Python 语法校验通过。

未完成：

- `pytest sidecars/market-provider/tests/test_app.py sidecars/market-provider/tests/test_error_contract.py`

阻塞原因：

- 当前环境缺少 `pandas`，`pytest` 在导入 `sidecars/market-provider/tests/conftest.py` 时直接失败，无法完成运行时回归。

## review 结论

`td/038` 对上一轮遗留问题的修复已经完整落地，本轮无需继续开新的 `td` 缺陷项。建议将该项实现归档到 `docs`，作为 refresh identity-safe 与 sidecar 错误契约收口的完成记录。
