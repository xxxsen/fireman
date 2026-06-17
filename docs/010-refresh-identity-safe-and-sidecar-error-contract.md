# Refresh identity-safe 与 sidecar 错误契约收口

## 背景

`td/037` review 暴露了两个遗留问题：

1. 已有资产的 refresh 路径没有传递 `instrument_kind`，`cn_exchange_fund` 在 `ETF/LOF/stock` 同码场景下仍可能取错历史数据源。
2. sidecar 入口层仍保留 `@app.on_event("startup")`，且请求体验证错误没有进入统一结构化错误契约。

`td/038` 的目标就是把这两个遗留问题彻底收口。

## 最终实现

### 1. `instrument_kind` 持久化并贯穿 refresh

- 为 `instruments` 表新增 `instrument_kind` 字段：
  - `migrations/0013_instrument_kind.sql`
- repository 层已支持该字段的读取、创建和更新：
  - `internal/repository/instrument.go`
- 导入占位资产时直接写入 resolve ticket 中的 `instrument_kind`：
  - `internal/service/instrument_import_tx.go`
- refresh 前对历史遗留资产执行一次受控 backfill；refresh fetch 请求始终携带 `instrument_kind`：
  - `internal/service/instrument_service.go`

这样 `cn_exchange_fund` refresh 不再依赖 sidecar 的 legacy fallback 链，可以稳定命中 identity-consistent 的历史数据源。

### 2. sidecar 生命周期与错误契约统一

- mutual fund cache warmup 已迁移到 FastAPI `lifespan`
- `RequestValidationError` 统一转换为结构化响应：

```json
{
  "code": 1,
  "error_code": "invalid_request",
  "message": "...",
  "data": null
}
```

- 实现位置：
  - `sidecars/market-provider/fireman_market_provider/app.py`

## 回归覆盖

### Go

- `internal/api/instrument_refresh_identity_integration_test.go`
  - 验证导入后的 refresh 会继续透传已解析 `instrument_kind`
  - 验证老资产缺失 `instrument_kind` 时会先 resolve backfill，再 refresh

### Python

- `sidecars/market-provider/tests/test_app.py`
  - 验证请求体未知字段/缺失字段时返回统一错误结构
- `sidecars/market-provider/tests/test_error_contract.py`
  - 验证 resolve/fetch/metadata 三类请求的 body validation 均进入统一错误契约

## 本轮 review 结论

`td/038` 已完成对 `td/037` 遗留问题的修复闭环。本轮未再发现新的缺陷或实现缺失，可视为该事项完成。
