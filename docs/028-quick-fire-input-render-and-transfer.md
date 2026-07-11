# FIRE 快算输入、稳定渲染与计划转入

## 目的

本文记录 FIRE 快算页面已经落地的输入编辑、结果更新和新建计划转入契约。相关计算公式和 API 见 [027-quick-fire-calculator.md](./027-quick-fire-calculator.md)。

本次交互修复不改变 Quick FIRE 后端公式或 Monte Carlo 收益假设，解决以下问题：

- 百分比输入无法连续录入 `2.29`；
- 参数变化时结果区、图表和年度表被卸载后重建；
- 金额输入未修改、仅 focus/blur 也会触发业务更新；
- React Strict Mode 下 transfer 可能被初始化逻辑提前消费，导致 FIRE 时长等字段回到默认值；
- 768px 下年度宽表可能撑开页面主列，产生横向溢出。

## 公共输入契约

`PercentInput` 和 `MoneyInput` 均区分用户正在编辑的字符串 draft 与父组件保存的业务数值。

### 百分比

- focus 后展示并维护原始 draft；
- `2.`、`.`、`-`、`-.` 等编辑中间态不会被格式化删除；
- 可解析且数值真正变化时才调用 `onChange`；
- blur 不重复提交相同数值，未完成内容恢复为当前 prop；
- 百分比仍以 decimal 传递，例如输入 `2.29` 得到 `0.0229`；
- 范围校验由具体业务表单负责，公共组件不 clamp。

### 金额

- 金额仍以元为输入、minor unit 为业务值；
- 可解析且 minor unit 真正变化时才调用 `onChange`；
- 未修改 focus/blur 不通知父组件；
- 输入阶段已经通知的新值不会在 blur 时重复通知；
- 空值 blur 继续表示 0，已为 0 时不会重复通知；
- 非法或未完成 draft 在 blur 后恢复当前 prop。

该契约对所有公共组件使用方生效，包括 FIRE 快算、新建计划、计划设置、配置模板、向导持仓、持仓校正和调仓执行。计划设置还在页面 update 边界拒绝等价值，避免意外 dirty 状态。

## 快算结果更新

页面维护当前输入 key、最后一次成功结果对应的 input key、请求状态和 request sequence。

合法输入变化后：

1. 保留最后一次成功结果和 ECharts 容器；
2. 固定状态行依次显示等待计算、正在计算；
3. 300ms debounce 后请求 API；
4. AbortController 取消旧请求，sequence 阻止晚到响应覆盖新结果；
5. 新响应到达时一次替换结果；
6. 请求失败时明确标注当前展示的是上次结果，并提供重试。

输入非法时，旧结果 DOM 保留以维持尺寸，但使用 `aria-hidden` 和不可见样式，不再作为当前有效结论暴露。没有历史结果时使用固定尺寸占位，避免 loading 造成主栏坍塌。

`QuickFireSummary`、`QuickFireChart` 和 `QuickFireYearTable` 均具有 memo 边界；图表 option 只在 years 引用变化时重建。输入变化但结果未变化时，重组件不重新 render。

页面 grid、结果容器和年度表 section 使用 `min-w-0`。年度宽表只能在自己的 `overflow-x-auto` 容器内滚动，不能撑开 768px 页面主列。

## 新建计划转入

sessionStorage transfer 使用非破坏性读取：

```text
readQuickFireTransfer   读取、校验、返回精确字段，不删除
clearQuickFireTransfer  在应用成功提交后确认删除
```

新建计划不在 render 或 state initializer 中访问或删除 transfer。挂载后的 effect 读取数据，使用挂载级 ref 防止 Strict Mode 重复应用，批量设置向导状态；状态提交后再清理 transfer。损坏数据会被清理并显示可关闭错误提示。

字段映射集中在纯函数 `quickFireTransferToWizardPatch`：

| 快算字段 | 计划字段 | 规则 |
| --- | --- | --- |
| `current_age` | `current_age` | 原值 |
| `planned_fire_age` | `retirement_age` | 原值 |
| `end_age` | `fireDurationYears` / `end_age` | 时长为 `end_age - planned_fire_age`，提交时恢复原 end age |
| `current_assets_minor` | `total_assets_minor` | 原值 |
| `annual_savings_minor` | 同名字段 | 原值 |
| `annual_savings_growth_rate` | 同名字段 | 原值 |
| `annual_spending_minor` | 同名字段 | 原值 |
| `annual_retirement_income_minor` | 同名字段 | 原值 |
| `annual_retirement_income_growth_rate` | 同名字段 | 原值 |
| `inflation_rate` | `fixed_inflation_rate` | 同时设置 `inflation_mode=fixed_real` |
| `terminal_wealth_floor_minor` | 同名字段 | 原值 |
| `annual_return_rate` | 不转入 | 完整计划收益由持仓和模拟假设生成 |

计划名称、估值日、配置模板、持仓、地区目标、提款策略、税率、交易费和模拟开关继续使用向导默认值或由用户选择。

## 验证覆盖

自动化测试覆盖：

- `2`、`2.`、`2.2`、`2.29` 连续编辑和最终 decimal；
- 未完成百分比、负数、小于 1 的百分比和外部 prop 更新；
- 金额未修改 blur、只提交一次、零值和非法 draft；
- 快算未修改 focus/blur 不请求 API、不写 localStorage、不重绘 ECharts；
- debounce/loading 时结果和图表节点保持挂载；
- 新响应单次更新、旧响应晚到不覆盖、错误重试、非法输入 aria 隐藏；
- Strict Mode 下 transfer 只应用一次；
- UI 全字段映射和最终 `createPlanWizard` payload；
- `end_age=91`、`planned_fire_age=49` 得到 FIRE 时长 42；
- 手工收益率不进入 transfer、向导 payload 或收益 override；
- 计划设置未修改 money/percent focus/blur 不进入 dirty 状态。

发布前门禁：

```bash
make build
make test
make lint-go
make web-lint
make web-test
make web-build
make integration-test
```

响应式验收使用 375x812、768x1024 和 1440x900：输入、状态行、结果指标和图表无页面级横向溢出，768px 宽表不会撑开主列，移动端保持紧凑结论优先显示。
