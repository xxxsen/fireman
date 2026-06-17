# td/035 实施 Review

## 1. 结论

本轮 `td/035` 已经修掉了上一轮最直接的问题：

- 裸码 LOF 不再在 `fund_lof_code_id_map_em` 失败时硬编码伪造为 `SZ`
- `SH` / `SZ` LOF 的超时与恢复路径都补了回归测试
- LOF 交易所来源已经收敛到权威 `market-id` 映射

但当前实现仍存在 1 项 P1 缺陷：当同一裸码同时具备 `ETF` 与 `LOF` 候选时，如果 `LOF` 的权威 `market-id` 查询失败，系统会直接返回 `ETF` 单候选，而不是返回 `market_provider_timeout`。这会把“身份尚未确认”的二义代码错误收窄成单一 `ETF`。

因此：

- `td/035` 仍不能视为完整关闭；
- 本次 review 结果继续保留在 `td/`；
- 不迁移到 `docs/`。

## 2. Review 范围

- [td/035-td034-implementation-review.md](/home/sen/work/fireman/td/035-td034-implementation-review.md)
- 当前工作区内与 `td/035` 直接相关的 sidecar resolve 代码和回归测试

## 3. Findings

### P1-1 ETF / LOF 同码二义场景下，LOF 权威查询失败会被错误收窄成 ETF

**位置**

- [resolve.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/resolve.py:378)
- [resolve.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/resolve.py:397)
- [resolve.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/resolve.py:401)

**问题**

当前实现虽然修掉了“LOF 查询失败后伪造 `sz{bare}`”的问题，但在另一类场景中仍会返回错误结果：

1. 裸码同时出现在 `etf_map` 和 `lof_map` 中；
2. `ETF` 候选可正常解析；
3. `LOF` 名称命中，但 `fund_lof_code_id_map_em` 超时或失败；
4. 代码将 `lof_authoritative_failed=True`，但因为 `result` 中已经有 `ETF` 候选，最终不会进入 `_raise_resolve_timeout(...)`；
5. 接口直接返回单一 `ETF` 结果。

这会把一个本质上仍然“未完成 LOF 权威确认”的二义代码，错误地收窄为 `ETF`。这与 `td/035` 的目标不一致：只要发生了 `LOF` 名称命中且其权威 market-id 查询失败，就应返回稳定的 `market_provider_timeout`，而不是回落到部分成功结果。

典型场景就是已有测试样本 `150001` 一类同码 `ETF / LOF` 情况。当前测试只覆盖了：

- 纯 LOF 裸码 / 前缀码超时返回 504
- 纯 LOF 恢复后能按权威交易所解析

但没有覆盖“ETF 候选仍存在时，不得把 LOF 超时静默收窄成 ETF”这一分支。

**修复方案**

在 `_resolve_cn_exchange_fund()` 中，只要本次请求出现以下条件之一：

1. `bare in lof_map`
2. 且 `LOF` 权威 `market-id` 查询失败或超时

则本次请求必须整体返回 `market_provider_timeout`，不得因为同时存在 `ETF` 候选就返回单一 `ETF` 或继续输出局部结果。

实现上应将 `lof_authoritative_failed` 的优先级提升到高于结果输出：

- 对 prefixed / bare 两条分支都统一执行：
  - 一旦 `lof_authoritative_failed=True`，先返回 timeout；
  - 之后才允许处理 `result`、fallback 和 mismatch 分支。

**验收逻辑**

1. 使用一个真实或测试构造的同码 `ETF / LOF` 标的，例如 `150001`。
2. 让：
   - `fund_etf_spot_em` 正常返回该代码；
   - `fund_lof_spot_em` 也正常返回该代码；
   - `fund_lof_code_id_map_em` 超时或失败。
3. 对裸码 resolve：
   - 必须返回 `504 / upstream timeout`
   - 不得返回单一 `ETF`
   - 不得返回 `candidates`
4. 对显式前缀 code resolve：
   - 同样必须返回 `504 / upstream timeout`
   - 不得因为 `ETF` 分支可解析而成功
5. 恢复 `fund_lof_code_id_map_em` 后：
   - 应恢复到正确的二义返回，或按权威结果返回单候选
6. 新增 sidecar 回归测试，覆盖：
   - bare dual-code timeout
   - prefixed dual-code timeout

## 4. 测试情况

本轮 review 主要涉及 sidecar Python 代码。

当前环境下未能完整执行定向 pytest：

- 直接 `pytest` 缺少 `pandas`
- `uv run pytest` 受网络限制，无法拉取 `hatchling`

因此本次 review 以代码路径核查和已有回归测试覆盖分析为主。

## 5. 总结

`td/035` 已经修正了“LOF 超时后伪造 SZ 候选”的主问题，但仍遗漏了“同码 ETF/LOF 二义时被静默收窄成 ETF”的分支。这个问题会直接影响资产身份判定，必须补上后才能关闭 `td/035`。
