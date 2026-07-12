# FIRE 快算

## 目的与边界

`/quick-fire` 提供无需计划、持仓、行情或模拟任务的确定性 FIRE 估算。它回答：在用户指定的固定年化收益率、储蓄、退休稳定收入、支出与通胀下，计划退休年龄是否可行，以及资产可完整支付多久。

该工具不是 Monte Carlo 的简化“成功率”。页面和 API 不提供概率字段；市场波动、相关性、FX、税务、交易费、再平衡和收益顺序风险仍由完整计划模拟处理。

## 输入与校验

接口为 `POST /api/v1/fire/quick-calculations`。首版固定 `base_currency=CNY`，引擎版本为 `quick_fire_v1`。金额均为 CNY minor unit，金额字段必须为 JSON integer，rate 必须为有限小数。

| 字段 | 含义 | 范围 |
| --- | --- | --- |
| `current_age` / `planned_fire_age` / `end_age` | 当前、计划 FIRE、目标年龄 | `18 <= current <= fire < end <= 120` |
| `current_assets_minor` | 当前可投资净资产 | `0..999_999_999_999_00` |
| `annual_savings_minor` | FIRE 前年净投入 | `0..99_999_999_999_00` |
| `annual_spending_minor` | 当前购买力下的退休首年年支出 | `1..99_999_999_999_00` |
| `annual_retirement_income_minor` | FIRE 后首年税后稳定净收入 | `0..99_999_999_999_00` |
| `terminal_wealth_floor_minor` | 目标年龄的最低名义资产 | `0..999_999_999_999_00` |
| 两个收入增长率 | 储蓄、稳定收入的年度增长率 | `[-0.5, 0.5]` |
| `annual_return_rate` | 用户指定的名义几何年化收益 | `[-0.99, 1]` |
| `inflation_rate` | 确定性年化通胀 | `[-0.02, 0.2]` |

请求体上限为 32 KiB。handler 使用严格 JSON decoder：拒绝未知字段、类型错误、尾随对象和尾随内容。参数错误返回 `400 quick_fire_parameters_invalid`；中间数值超出 int64 或非有限返回 `422 quick_fire_result_out_of_range`。

## 计算契约

年化收益和通胀均用几何月率，而非年率除以 12：

```text
monthly_return = (1 + annual_return_rate)^(1/12) - 1
monthly_inflation = (1 + inflation_rate)^(1/12) - 1
```

月下标 `m=0` 是当前年龄起的第一个月。每月固定按“收入、支出、收益”结算：

```text
pre_return = start_wealth + income_m - spending_m
end_wealth = pre_return * (1 + monthly_return)
```

FIRE 前仅计入年净储蓄，按 `floor(m / 12)` 应用储蓄增长；FIRE 后仅计入稳定收入，按退休后的完整年数应用收入增长。退休期支出从当前购买力按 `monthly_inflation^m` 增长，因此首月支出正好是 `annual_spending / 12`。

若某退休月资产加收入不足以支付支出，状态为 `insufficient_funds`，记录未支付缺口并停止；若完整支付后 `round(end_wealth) <= 0`，状态为 `wealth_depleted`。即使正好在目标年龄归零也不是可持续。所有金额均在输出边界统一四舍五入为 minor unit；年度账本以反推投资收益确保严格满足：

```text
end = start + income - spending + investment_gain
```

### 所需资本与最早 FIRE

计划 FIRE 月所需资本从目标年龄向前递推，使用同一退休期现金流：

```text
required_end = max(terminal_wealth_floor_minor, 1)
required_start = max(0, required_end / (1 + monthly_return) + spending_m - income_m)
```

最低 1 minor 与“零资产即耗尽”保持一致。引擎同时生成继续工作的积累资产序列，并扫描第一个“积累资产不少于候选退休所需资本”的月份，作为 `earliest_fire_*`。输出还包含计划 FIRE 时资产、资金富余/缺口、可完整支付月数、耗尽年龄、名义与真实期末资产，以及以当前购买力折算的逐年账本。

## 与完整计划的连接

计划参数和 simulation snapshot 在 3.3.0 增加：

- `annual_retirement_income_minor`
- `annual_retirement_income_growth_rate`

稳定收入从 FIRE 月开始先进入现金池，再支付支出；剩余收入参与收益并写入月度/年度账本。字段迁移默认值均为零，进入 config hash，修改后旧 run 会 stale。3.2.0 及更早 snapshot 的缺失字段按零值回放。

快算页面使用版本化 localStorage 保存输入。点击“创建完整计划”后，年龄、资产、支出、储蓄、稳定收入、其增长率、通胀和期末目标经一次性 sessionStorage transfer 带入 `/plans/new`；手工 `annual_return_rate` 不会带入或变成资产收益 override。

## 验收

自动化回归应覆盖：

- 年化几何复利、负收益、通胀首月和第 13 月时点；
- 零收益耗尽、最后一个月归零、稳定收入、积累期、反推所需资本与最早 FIRE 月；
- 连续重复计算的 JSON 确定性以及每行年度账本恒等式；
- 严格 API JSON、金额小数、所有 rate/age 边界、32 KiB 上限和投影溢出；
- retirement income 基线默认值、参数 round-trip、config hash、FIRE 前后收入时点、旧 snapshot 回放和确定性 simulation 对拍；
- Web 默认计算、防抖、取消旧请求、非法输入隐藏旧结论、错误重试、草稿损坏恢复、移动年度列表和一次性 transfer。

工程门禁：

```bash
make build
make test
make install-golangci-lint
make lint-go
make web-install
make web-lint
make web-test
make web-build
make integration-test
go test ./internal/quickfire ./internal/simulation ./internal/service ./internal/api -count=1
```

人工浏览器验收使用 375x812、768x1024、1440x900 三种 viewport：首屏可编辑且显示结论；图表三条序列与年度表一致；无页面级横向滚动；键盘可完成输入、展开高级项、重置及创建完整计划；页面不将确定性结论表述为成功率。

输入编辑中间态、结果区稳定更新和新建计划 transfer 的实现约定见
[028-quick-fire-input-render-and-transfer.md](./028-quick-fire-input-render-and-transfer.md)。
