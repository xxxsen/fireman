# LOF 权威映射与超时收敛

- 更新：2026-06-17

---

## 1. 目标

解决场内基金 resolve 中两类错误行为：

1. `LOF` 权威 `market-id` 查询失败时，不能伪造 `SZ` / `SH` 候选
2. 同码 `ETF / LOF` 二义场景中，不能在 `LOF` 权威查询失败时静默收窄成单一 `ETF`

当前规则已经统一为：

- `LOF` 交易所身份只能来自权威 `fund_lof_code_id_map_em`
- 只要 `LOF` 名称命中但权威查询失败或超时，请求整体返回 `market_provider_timeout`

---

## 2. 行为规则

### 2.1 纯 LOF 场景

当代码只命中 `LOF` 名称表时：

- 若 `fund_lof_code_id_map_em` 成功返回：按权威 market-id 生成 `shxxxxxx` 或 `szxxxxxx`
- 若 `fund_lof_code_id_map_em` 失败或超时：返回 `504 / upstream timeout`

不得：

- 依据裸码前缀推断交易所
- 默认回退为 `SZ`
- 仅凭显式前缀直接认定成功

### 2.2 ETF / LOF 同码二义场景

当同一 bare code 同时命中 `ETF` 与 `LOF` 名称表时：

- 若 `LOF` 权威 market-id 成功返回：保留正确的歧义候选集
- 若 `LOF` 权威 market-id 失败或超时：整体返回 `504 / upstream timeout`

不得：

- 保留单一 `ETF` 结果
- 以部分成功结果替代未确认的 `LOF` 身份

---

## 3. 实现位置

### Sidecar

- `sidecars/market-provider/fireman_market_provider/adapters/cn_code.py`
  - `lof_market_id_with_status()`
- `sidecars/market-provider/fireman_market_provider/adapters/resolve.py`
  - `_parse_authoritative_lof()`
  - `_resolve_cn_exchange_fund()`

### 回归测试

- `sidecars/market-provider/tests/test_td035_regression.py`
  - 纯 LOF 超时与恢复
- `sidecars/market-provider/tests/test_td036_regression.py`
  - dual-code timeout 与恢复

---

## 4. 结果语义

| 场景 | 返回 |
| --- | --- |
| LOF 名称命中 + 权威 market-id 成功 | 成功返回权威交易所候选 |
| LOF 名称命中 + 权威 market-id 超时/失败 | `504 / upstream timeout` |
| ETF/LOF 双命中 + 权威 market-id 成功 | 正确歧义结果 |
| ETF/LOF 双命中 + 权威 market-id 超时/失败 | `504 / upstream timeout` |

---

## 5. 约束

当前实现明确禁止以下行为：

1. 用裸码前缀推断 `LOF` 交易所
2. 在 `LOF` 权威失败时默认回退 `SZ`
3. 在 dual-code 场景下用部分 `ETF` 成功掩盖 `LOF` 身份未确认

这保证了场内基金 resolve 的身份语义与超时语义一致。
