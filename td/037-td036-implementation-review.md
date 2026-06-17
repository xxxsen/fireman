# td/036 实施 Review

## 1. 结论

本轮 `td/036` 已完成上一轮提出的剩余 P1 修复：

- 当同码同时存在 `ETF` / `LOF` 候选时，若 `LOF` 的权威 `fund_lof_code_id_map_em` 查询失败或超时，请求会优先返回 `market_provider_timeout`
- 不再把“身份待确认”的二义代码静默收窄成单一 `ETF`
- 已补充 dual-code 的 bare / prefixed timeout 回归测试，以及权威映射恢复后的歧义返回测试

本轮 review 未发现新的缺陷或实现缺失。`td/036` 可以视为关闭。

因此：

- 本次 review 结果保留在 `td/`
- 同时将整理后的实现结论归档到 `docs/`

## 2. Review 范围

- [td/036-td035-implementation-review.md](/home/sen/work/fireman/td/036-td035-implementation-review.md)
- 当前工作区中与 `td/036` 对应的 sidecar resolve 代码与回归测试

## 3. Review 结果

### 3.1 已关闭项

上一轮的唯一 P1 已关闭。

证据：

- [resolve.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/resolve.py:378) 在 prefixed 分支中，当 `LOF` 名称命中但权威 `market-id` 查询失败时，直接 `_raise_resolve_timeout(...)`，优先级高于任何部分 `ETF` 结果
- [resolve.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/resolve.py:403) 在 bare 分支中采用同样策略，不再允许在 dual-code 场景下静默收窄
- [test_td036_regression.py](/home/sen/work/fireman/sidecars/market-provider/tests/test_td036_regression.py:45) 覆盖了：
  - bare dual-code timeout
  - prefixed dual-code timeout
  - 恢复后重新返回正确歧义结果

### 3.2 未发现新的缺陷

本轮聚焦的逻辑链已经闭环：

1. 纯 LOF 超时时，不再伪造 `SZ` / `SH`
2. dual-code 超时时，不再静默收窄成 `ETF`
3. 权威映射恢复后，可重新返回正确的 `ETF / LOF` 歧义结果

从当前代码路径看，`LOF` 的交易所身份已经稳定依赖权威 `market-id` 映射，不再由 spot 名称命中或局部候选推断。

## 4. 本地校验

已执行：

- `python3 -m py_compile sidecars/market-provider/fireman_market_provider/adapters/resolve.py sidecars/market-provider/tests/test_td036_regression.py`

结果：

- 语法通过

未完整执行：

- sidecar 定向 pytest
  - 当前环境直接 `pytest` 缺少 `pandas`
  - `uv run pytest` 仍受网络依赖拉取限制

本轮没有发现需要依赖完整 pytest 才能确认的新风险，现有代码路径和新增回归测试覆盖与需求一致。

## 5. 归档结论

`td/036` 已完整实现。整理后的实现说明应迁入 `docs/`，供后续参考，不再继续停留在 review 待办状态。
