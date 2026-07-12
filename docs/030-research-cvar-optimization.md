# 组合研究 CVaR 优化

- 状态：已完整实施
- 适用范围：组合研究普通回测、寻找最优组合、结果应用及复制到 FIRE 计划后的验证
- 目标版本：`research_backtest_v3`、`research_optimizer_v4`
- CVaR 算法版本：`empirical_cvar_v1`

## 0. 实施与验证状态

本能力已按本文契约落地到 migration、repository、service、API、worker 和 Web。当前实现以
`ComputeEmpiricalCVaR` 作为普通回测与自动调优的唯一 CVaR 计算入口，集合、快照、输入哈希、
优化结果和应用事务均冻结同一尾部风险口径。

完成门禁：

- `make ci`：Go build/test/lint、Web lint/test/build、sidecar test、integration test 全部通过；
- Web：80 个测试文件、587 个用例通过；
- sidecar：187 个用例通过，12 个按既有配置 deselect；
- 性能 fixture：10 个资产、2,000 个候选、2,520 个有效收益日；开启 CVaR 后完整候选回测耗时增加
  约 `1.44%`，分配内存增加约 `0.31%`，采样峰值堆内存未增加，低于 `25% / 20%` 门槛；
- 浏览器：`1440x900` 与 `390x844` 验证集合参数、优化结果、普通回测，无重叠或异常裁切；
- 端到端测试：所有优化候选使用相同有效收益日和尾部场景数；应用后普通回测继续按正权重资产生成自身样本。

`research_optimizer_v4` 针对集合 `rc_0111ff60-fd0c-41e7-8f57-d8b53870c6c7` 的同数据副本完成
861 个候选实测：成功 861、跳过 0，四个榜单 Top 20 均为 `1006` 个有效收益日和 `987` 个
20 日场景。最低尾部损失组合的 CVaR 为 `5.1769%`，低于最低回撤组合的 `5.3537%`；前者
年化收益和最大回撤更差，符合两个目标函数衡量不同风险维度的定义。

## 1. 结论与实施决策

当前系统应在“组合研究”中接入 CVaR（Conditional Value at Risk，条件风险价值，也称 Expected Shortfall）优化，而不是把 CVaR 直接塞入 FIRE Monte Carlo 的运行按钮。

原因是当前系统已经具备完整的离散权重优化闭环：

```text
研究集合
  -> 冻结共同历史窗口与 FX
  -> 枚举最多 20,000 个候选权重
  -> 对每个候选运行同一回测引擎
  -> 按 CAGR / 最大回撤 / Calmar 排序
  -> 原子应用候选权重与回测窗口
  -> 复制到 FIRE 计划并运行前瞻模拟
```

CVaR 应成为这条链路中的第四个目标“最低尾部损失”。它使用候选组合已经生成的实际回测收益路径，衡量最差一部分持有期场景的平均损失。这样能够补足最大回撤只观察单一峰谷、年化波动率对上下波动同等处理的缺陷。

本方案作出以下不可变决策：

1. v1 实现**历史经验 CVaR**，不引入正态分布假设、参数化 VaR、Monte Carlo 重采样或外部预测模型。
2. 优化域继续使用现有离散权重网格、锁定权重和候选上限，不引入连续 LP/QP 求解器。
3. CVaR 基于现有回测引擎输出的基准币种组合收益，因此自然继承资产历史、FX、前值填充、再平衡和共同窗口语义。
4. 普通回测和自动调优必须调用同一个 CVaR 纯函数，不能分别计算。
5. CVaR 口径由“置信度 + 持有期”唯一确定并持久化到研究集合；新集合默认 `95% + 20 个有效交易日`。
6. CVaR 优化只产生研究结果，不自动修改研究集合或 FIRE 计划；用户点击“应用”后才原子写回集合。
7. 应用任何该次调优结果时，同时同步调优冻结的回测窗口和 CVaR 口径；优化结果保留其冻结样本口径，随后普通回测按应用后的正权重资产重新生成有效日。
8. FIRE 模拟不新增一个名为 CVaR 的随机过程。用户把结果复制到计划后，仍使用现有 FIRE 成功率、真实财富分位数和失败时间验证长期效果。

## 2. 使用 CVaR 的位置

### 2.1 组合研究普通回测

每个新建普通回测 run 的 `BacktestSummary` 增加一组尾部风险指标：

- 置信度；
- 持有期（有效交易日）；
- 场景数量；
- VaR loss；
- CVaR loss；
- 最差持有期 loss。

结果页新增“尾部风险”指标卡和解释区，让用户在应用优化权重后使用相同窗口、相同口径重新回测并核对 CVaR。

### 2.2 寻找最优组合

现有一次优化继续同时生成多组 Top K：

```text
最高收益       max_cagr
最低回撤       min_drawdown
最低尾部损失   min_cvar
收益回撤平衡   max_calmar
```

`min_cvar` 在现有可行候选集合中选择 CVaR loss 最小的候选。该实现是“离散可行域上的精确枚举优化”，不是近似地给现有指标改名。

### 2.3 应用优化结果

应用 `min_cvar` 与现有目标使用同一个原子接口。除既有权重、启用状态、锁定状态和窗口同步外，还要同步：

```text
tail_risk_confidence
tail_risk_horizon_days
```

应用其他三个目标也同步这两个字段，因为它们的结果表同样展示了该次冻结口径下的 CVaR。历史 `research_optimizer_v2` snapshot 没有 CVaR spec，应用这类旧结果时不得覆盖集合当前口径。

### 2.4 FIRE 计划联动

CVaR 不直接出现在 FIRE 参数设置中，也不在运行 FIRE 模拟时重新优化权重。完整用户流程为：

1. 在组合研究中运行 CVaR 优化；
2. 查看收益、回撤、CVaR 和权重集中度；
3. 显式应用候选；
4. 使用现有“复制到计划”流程生成计划持仓草稿；
5. 在计划中确认持仓、资产分类和金额；
6. 运行 FIRE 模拟，对比成功率、失败月份和真实财富分位数。

CVaR 下降不等于 FIRE 成功率必然上升。结果页必须明确两者的指标边界，但不增加营销式承诺。

### 2.5 不使用 CVaR 的位置

- 不加入资产目录同步或单资产行情更新；
- 不加入已移除的资产筛选器；
- 不写入全局 simulation assumption profile；
- 不替换 FIRE 的 Student-t、相关性、通胀或现金流模型；
- 不将 CVaR 作为“预计最大亏损”，因为它描述的是尾部场景平均损失，不是最大损失。

## 3. 数学契约

### 3.1 收益序列来源

CVaR 只能使用 `RunResearchBacktest` 生成的 `effReturns`：

- 已按共同窗口裁剪；
- 已转换为集合基准币种；
- 已使用现有前值填充和 FX 规则；
- 已按候选权重和集合再平衡规则生成组合路径；
- 仅包含 `effective=true` 的收益日；
- 不包含 `points[0]` 的起始零收益。

不得从原始价格另算一套收益，也不得直接对各资产 CVaR 做权重加总。CVaR 是组合损失分布的非线性指标，必须从组合路径计算。

普通回测与优化候选具有不同的资产集合语义，必须明确区分：

- 普通回测仅由正权重资产及其 FX 触发 `effective`，零权重资产不得改变波动率或 CVaR；
- `research_optimizer_v4` 的每个候选必须保留优化快照中的全部启用资产，即使候选权重为 0；
- 优化专用 `FreezeEffectiveCalendar` 使全部冻结资产及其 FX 的真实 observation 共同触发 `effective`；
- 零权重资产在自身不可用日期使用中性 value-grid 占位，不影响 NAV，只影响所有候选共享的样本日期；
- 优化循环以第一个成功候选冻结 `effective_return_days` 和 `scenario_count`，后续候选不一致时以 `candidate_sample_mismatch` 失败，禁止在不同样本上排名；
- relevant 的基准币种现金没有行情；全现金普通回测使用窗口内工作日，外币现金由其 FX observation 触发。

因此，优化结果页中的所有候选可以直接横向比较。应用结果后，零权重资产会被禁用，普通回测的有效日可能减少，波动率、VaR/CVaR 等依赖有效收益样本的指标不保证与优化结果逐位相同；这不是公式误差。CAGR、最大回撤等基于完整 NAV 路径的指标仍按各自定义计算。需要复核优化排名时应查看该次不可变优化结果，而不是要求应用后的普通回测重建已丢弃资产的冻结日历。

全基准币种现金还需要明确窗口来源：若没有任何正权重 bounded asset，`WindowStart` 和 `WindowEnd` 必须同时非空，engine 直接使用该显式窗口并继续执行现有最少 365 天校验；缺少任一边界时返回 `research_no_common_window`。优化候选已有冻结窗口，应用后又会同步为 `custom_range`，因此该规则不会阻塞全现金结果运行。

### 3.2 持有期场景

设有效单期简单收益为：

```text
r_1, r_2, ..., r_T
```

持有期 `h` 只允许：

```text
h in {1, 20}
```

对每个可用终点 `t >= h` 构造重叠持有期收益：

```text
R_t(h) = product(j=t-h+1..t, 1 + r_j) - 1
L_t(h) = -R_t(h)
```

场景数固定为：

```text
N = T - h + 1
```

`h=20` 的展示名称为“20 个有效交易日”，不是自然月。使用重叠窗口可以在有限历史中保留足够尾部场景；UI tooltip 必须说明相邻场景会共享部分收益日。

若任一 `1+r_j <= 0`、复合结果为 NaN/Inf，返回 `cvar_return_invalid`，不 clamp。

### 3.3 Loss 方向

所有 VaR/CVaR 字段存储为 loss：

```text
loss = -holding_period_return
```

示例：

- 持有期收益 `-8%` -> loss `+8%`；
- 持有期收益 `+2%` -> loss `-2%`。

因此 CVaR 越小越好。若所有尾部场景仍为正收益，VaR/CVaR 可以为负数，禁止强行截断为 0。

### 3.4 经验 VaR 与 CVaR

置信度 `alpha` 只允许：

```text
alpha in {0.90, 0.95, 0.99}
```

将 `N` 个 loss 按降序排列：

```text
L[1] >= L[2] >= ... >= L[N]
```

先把合法 confidence 规范化为整数尾部百分数：

```text
alpha=0.90 -> tail_pct=10
alpha=0.95 -> tail_pct=5
alpha=0.99 -> tail_pct=1
```

然后只用整数商和余数确定尾部质量：

```text
tail_units = tail_pct * N
k          = tail_units / 100       // integer division
remainder  = tail_units % 100
f          = remainder / 100.0
m          = k + f
tail_count = (tail_units + 99) / 100 // integer ceil

VaR_loss = L[ceil(m)]

CVaR_loss = (sum(i=1..k, L[i]) + f * L[k+1]) / m
```

当 `remainder=0` 时不读取 `L[k+1]`。服务准入保证 `m >= 5`，所以不存在 `k=0` 或除零分支。禁止直接用 `(1-alpha)*N` 的浮点结果执行 `floor/ceil`，否则边界样本可能因二进制误差多取一个场景。

这一分数是经验分布上最差 `(1-alpha)` 概率质量的平均 loss；分数内部不年化、不乘平方根时间。

### 3.5 样本门槛

每种置信度至少要求 5 个完整等价尾部观测，固定门槛为：

| alpha | 最少持有期场景数 N | 最少有效收益数 T |
| ---: | ---: | ---: |
| 0.90 | 50 | `50 + h - 1` |
| 0.95 | 100 | `100 + h - 1` |
| 0.99 | 500 | `500 + h - 1` |

不得用浮点 `ceil(5/(1-alpha))` 动态推导，以免 `0.90` 的二进制浮点误差产生 51。代码使用常量 lookup。API 输入先在 `1e-12` 容差内映射到 `0.90/0.95/0.99` 的 canonical 值，持久化、hash 和计算均使用 canonical 值。

不足时 readiness 返回 `cvar_sample_insufficient`，普通回测和自动调优均阻断。不会自动降低置信度、缩短持有期或回退到单日 CVaR。

### 3.6 最低 CAGR 门槛

自动调优请求支持可选 `minimum_cagr`：

- `null`：所有成功候选均可进入 CVaR 榜单；
- 非 null：只有 `summary.CAGR >= minimum_cagr` 的候选进入 CVaR 榜单；
- 合法范围 `[-0.95, 2.0]`，必须为有限数；
- 该门槛只筛选 `min_cvar`，不影响 CAGR、回撤、Calmar 三个既有榜单。

默认必须为 `null`，不能用 0 冒充“未设置”。用户需要限制低风险结果过度偏向现金时，显式启用该门槛。

若完成后没有候选满足门槛：

- optimization run 仍为 `succeeded`；
- `best_by_cvar=[]`；
- result warning 写入 `cvar_minimum_cagr_unmet`；
- 其他榜单正常返回；
- UI 展示明确空态，不回退到不满足门槛的候选。

### 3.7 排序规则

CVaR tracker 内部仍遵循“score 越大越好”，因此：

```text
score = -CVaR_loss
```

最终稳定排序键固定为：

```text
primary:    CVaR_loss ASC
secondary:  VaR_loss ASC
tertiary:   CAGR DESC
quaternary: abs(max_drawdown) ASC
final:      canonical weight vector ASC
```

前端“得分”列展示正向语义的 `CVaR_loss`，不展示内部负 score。

## 4. 数值金标

### 4.1 分数尾质量

输入 25 个单期 loss，`alpha=0.90`：

```text
losses = [0.20, 0.10, 0.05, -0.01 x 22]
N = 25
m = 2.5
k = 2
f = 0.5
```

固定结果，容差 `1e-12`：

```text
tail_count = 3
VaR_loss   = 0.05
CVaR_loss  = (0.20 + 0.10 + 0.5 * 0.05) / 2.5
           = 0.13
worst_loss = 0.20
```

### 4.2 20 日复合

20 个有效收益全部为 `-1%`：

```text
R(20) = 0.99^20 - 1
loss  = 1 - 0.99^20
      = 0.18209306240276923
```

实现不能错误使用 `20 * 1% = 20%`。

### 4.3 边界金标

- `h=1` 时持有期收益必须逐位等于 `effReturns`；
- 有效收益长度 `T=h` 时场景数为 1，但会被样本门槛阻断；
- loss 全相同时 `VaR=CVaR=worst_loss`；
- 尾部 loss 全为负时 CVaR 保持负数；
- 输入顺序不影响排序后的 VaR/CVaR；
- 计算函数不修改调用方 slice。

## 5. 数据模型与迁移

### 5.1 Migration 编号

CVaR 按当前实施优先级占用：

```text
migrations/0029_research_cvar.sql
```

后续 Black-Litterman migration 使用 `0030_plan_black_litterman_views.sql`，避免编号冲突。

### 5.2 research_collections

新增：

```sql
ALTER TABLE research_collections
  ADD COLUMN tail_risk_confidence REAL NOT NULL DEFAULT 0.95;

ALTER TABLE research_collections
  ADD COLUMN tail_risk_horizon_days INTEGER NOT NULL DEFAULT 20;
```

SQLite 列只保存值，枚举合法性由 service 统一校验。旧集合迁移后固定获得 `0.95 + 20`，旧 run 的 `input_snapshot_json` 和 `summary_json` 不修改。

### 5.3 无需新增结果列

以下内容继续存 JSON：

- 普通回测的 CVaR 在 `research_backtest_runs.summary_json`；
- 优化配置在 `research_optimization_runs.config_json/input_snapshot_json`；
- CVaR Top K 在 `result_json`。

不向 `research_optimization_runs` 增加重复投影列，也不向 `research_asset_metrics` 增加 CVaR。当前资产筛选器已移除，而且单资产 CVaR 不是本次组合优化的查询条件。

## 6. 后端代码设计

### 6.1 纯 CVaR 模块

新增：

```text
internal/service/research_cvar.go
internal/service/research_cvar_test.go
```

保持在 `service` 包是为了直接复用当前未拆包的 `BacktestSummary/RunResearchBacktest` 类型，不额外制造跨包 DTO。文件内只包含纯函数，不访问数据库、网络、时间或随机数。

核心类型：

```go
type TailRiskSpec struct {
    Confidence  float64 `json:"confidence"`
    HorizonDays int     `json:"horizon_days"`
}

type BacktestTailRisk struct {
    AlgorithmVersion string  `json:"algorithm_version"`
    Confidence       float64 `json:"confidence"`
    HorizonDays      int     `json:"horizon_days"`
    ScenarioCount    int     `json:"scenario_count"`
    TailCount        int     `json:"tail_count"`
    VaRLoss          float64 `json:"var_loss"`
    CVaRLoss         float64 `json:"cvar_loss"`
    WorstLoss        float64 `json:"worst_loss"`
}
```

唯一入口：

```go
func ComputeEmpiricalCVaR(
    effectiveReturns []float64,
    spec TailRiskSpec,
) (BacktestTailRisk, error)
```

内部步骤固定为：

1. `ValidateTailRiskSpec`；
2. 校验所有收益有限且 `r > -1`；
3. 使用滚动乘积构造持有期 loss；
4. 校验 lookup 对应的最少场景数；
5. 用容量为 `tail_count` 的最小堆保留最大的 tail losses；
6. 只复制并降序排序堆内 tail losses；
7. 按第 3.4 节计算 VaR/CVaR；
8. 返回 `algorithm_version=empirical_cvar_v1`。

最小堆使用标准库 `container/heap`：先放入前 `tail_count` 个 loss，之后仅当新 loss 大于堆顶时执行 replace/fix；相等值不替换。最终用 `sort.Float64s` 排序堆副本并从尾部按降序读取。这样得到的集合与全量降序排序前 `tail_count` 项完全相同，不引入第三方数值依赖。不得为 CVaR 再运行一次回测。

### 6.2 回测引擎

修改：

```text
internal/service/research_backtest.go
internal/service/research_backtest_test.go
```

`BacktestInput` 新增：

```go
TailRisk *TailRiskSpec
```

v3 生产快照始终传入非 nil spec；指针只用于读取和展示未计算 CVaR 的旧 run，以及隔离既有纯引擎测试。

`BacktestSummary` 新增：

```go
TailRisk *BacktestTailRisk `json:"tail_risk,omitempty"`
```

`simulatePortfolio` 已经在 `collectEffectiveResearchReturns` 得到 `effReturns`。固定在 `buildSummary` 内调用 `ComputeEmpiricalCVaR(effReturns, in.TailRisk)`，成功后写入 summary。CVaR 失败是本次 run 失败，不允许用 nil 冒充成功；`omitempty` 仅用于读取旧 v2 JSON。

`ResearchEngineVersion` 升为：

```text
research_backtest_v3
```

旧 v2 run 继续只读、导出和展示；不补算、不覆盖、不参与 v3 input-hash 复用。

### 6.3 集合模型、CRUD 与 snapshot

修改：

```text
internal/repository/research.go
internal/service/research_service.go
internal/service/research_readiness.go
```

涉及结构：

- `repository.ResearchCollection`
- `ResearchCollectionInput`
- collection patch DTO
- `researchSnapshotParams`
- collection INSERT/SELECT/UPDATE/scan
- collection JSON import/export（若存在字段缺失，使用默认 0.95/20）

`buildResearchSnapshot` 冻结 CVaR spec；`computeResearchInputHash` 增加：

```text
tail_risk:confidence|horizon_days|algorithm_version
```

`source_hash` 不包含 CVaR 配置，因为它只描述市场数据事实。

所有新建/更新集合请求都调用同一个 `CanonicalTailRiskSpec`。字段缺失只在新建和旧 JSON 导入时应用默认；PATCH 缺失表示不修改。

### 6.4 Readiness

普通回测 readiness 和 optimization readiness 都增加精确样本检查。

准入计算复用以下 helper：

```go
func evaluateResearchTailRisk(ds *researchDataset, out *ResearchReadiness, block func(ResearchReadinessIssue))
func relevantEffectiveObservationDays(ds *researchDataset, lo, hi int) map[int]bool
```

它复用回测的数据准备、共同窗口、FX value grid 和 v3 relevant-asset `effective` 规则，只计算有效日数量，不生成 NAV、指标或数据库记录。

optimization readiness 必须对每个可能取得正权重的资产做保守准入：

- 所有 tunable asset；
- `locked weight > ResearchWeightTolerance` 的 locked asset；
- 基准币种现金按窗口工作日计数；
- 外币现金按 FX observation 计数。

任一上述资产单独可用的 scenario count 不达门槛，即以该 `asset_key` 返回 blocker。该规则可能比多资产 observation 并集更保守，但能保证每个可生成候选都具备足够 CVaR 样本，不会运行到中途才跳过候选。

稳定阻断码：

| reason/code | 场景 |
| --- | --- |
| `cvar_confidence_invalid` | confidence 不是 0.90/0.95/0.99 |
| `cvar_horizon_invalid` | horizon 不是 1/20 |
| `cvar_sample_insufficient` | 场景数低于第 3.5 节门槛 |
| `cvar_return_invalid` | 收益非有限或小于等于 -100% |

readiness 响应 details 增加：

```json
{
  "tail_risk": {
    "confidence": 0.95,
    "horizon_days": 20,
    "effective_return_count": 252,
    "scenario_count": 233,
    "minimum_scenario_count": 100
  }
}
```

优化 readiness 使用请求中的 CVaR spec；普通 readiness 使用集合保存值。前端不得只根据候选数量判断可运行。

### 6.5 自动调优配置与结果

修改：

```text
internal/service/research_optimization.go
internal/service/research_optimization_service.go
internal/service/research_optimization_test.go
```

`OptimizationConfig` 增加：

```go
TailRisk   TailRiskSpec `json:"tail_risk"`
MinimumCAGR *float64    `json:"minimum_cagr,omitempty"`
```

API request 与执行 config 分开定义，避免用零值猜测字段是否缺失：

```go
type ResearchOptimizationRequest struct {
    WeightStep        float64       `json:"weight_step"`
    MaxCandidateCount int           `json:"max_candidate_count"`
    TopK              int           `json:"top_k"`
    TailRisk          *TailRiskSpec `json:"tail_risk,omitempty"`
    MinimumCAGR       *float64      `json:"minimum_cagr,omitempty"`
}
```

`TailRisk=nil` 时 service 在加载 collection 后填入 collection 保存值；非 nil 时 confidence/horizon 必须同时合法，不能只补其中一个。默认填充发生在 validation 和 input hash 之前。`OptimizationConfig.NormalizeDefaults` 只处理 weight step/candidate limit/Top K，不覆盖已经解析的 TailRisk/MinimumCAGR。

readiness service 改用结构化参数：

```go
type OptimizationReadinessRequest struct {
    WeightStep  float64
    Confidence  *float64
    HorizonDays *int
}

func (s *ResearchService) GetOptimizationReadiness(
    ctx context.Context,
    collectionID string,
    req OptimizationReadinessRequest,
) (OptimizationReadiness, error)
```

handler 只负责严格解析 query 并保留 nil；service 加载 collection 后，用集合保存值填充缺失项，再调用 `CanonicalTailRiskSpec`。只提供其中一个时，另一个使用 collection 值。query 非数字由 handler 返回 HTTP 400 `invalid_request`；数字不属于枚举由 service 返回 HTTP 400 `cvar_confidence_invalid` 或 `cvar_horizon_invalid`。

`optimizationInputSnapshot.Config` 冻结完整 spec 和 minimum CAGR；`computeOptimizationInputHash` 必须加入所有字段及 `empirical_cvar_v1`。

新增 objective：

```go
ObjectiveMinCVaR OptimizationObjective = "min_cvar"
```

`OptimizationResult` 增加：

```go
BestByCVaR      []OptimizationResultItem `json:"best_by_cvar"`
CVaREligibleCount int                    `json:"cvar_eligible_count"`
Warnings        []OptimizationWarning    `json:"warnings,omitempty"`
```

候选执行仍只调用一次：

```go
RunResearchBacktest(buildBacktestInputForCandidate(...))
```

其中 `BacktestInput.TailRisk=snapshot.Config.TailRisk`。回测成功后：

1. 三个旧 tracker 始终正常评分；
2. `summary.TailRisk == nil` 视为候选失败，不允许进入任何 v4 tracker；
3. minimum CAGR 未启用或候选达到门槛时，进入 CVaR tracker 并增加 eligible count；
4. 全部候选完成后 eligible count 为 0，写 warning，不将 run 置为 failed。

`OptimizationEngineVersion` 升为：

```text
research_optimizer_v4
```

### 6.6 稳定 Top K 与 Apply

`TopKTracker` 对旧目标保持原比较器；`ObjectiveMinCVaR` 使用第 3.7 节专用比较器。禁止只依赖 `score` 后复用 CAGR tie-break，因为 CVaR 相同时必须先比较 VaR。

以下位置加入 `ObjectiveMinCVaR`：

- `validOptimizationApplyRequest`
- `findOptimizationResult`
- tracker 创建与收集
- API/前端 objective union
- apply 测试

`applyOptimizationSelection` 的同一事务增加更新 collection CVaR spec。事务顺序固定为：

1. 校验 run succeeded、objective/rank 和结果身份；
2. 校验 `expected_collection_updated_at`；
3. 校验当前 items 与冻结资产 identity；
4. 写入全部 item 权重/启用/锁定状态；
5. 写入冻结的 window/start policy；
6. 写入冻结的 confidence/horizon；
7. 更新一次 collection `updated_at`；
8. commit。

任一步失败整体回滚。继续复用：

- `research_collection_changed`
- `research_optimization_result_stale`

不新增第二个应用接口。

### 6.7 API

#### 集合 API

现有 collection GET/POST/PATCH 增加：

```json
{
  "tail_risk_confidence": 0.95,
  "tail_risk_horizon_days": 20
}
```

#### Optimization readiness

现有接口扩展 query：

```http
GET /api/v1/research/collections/:id/optimization-readiness
  ?weight_step=0.05
  &cvar_confidence=0.95
  &cvar_horizon_days=20
```

handler 必须严格解析；非法数字返回 HTTP 400 `invalid_request`，不能像当前 `weight_step` 那样解析失败后静默使用默认值。此次修改同时收紧 `weight_step` 的非法 query 行为。

#### Create optimization

```json
POST /api/v1/research/collections/:id/optimizations
{
  "weight_step": 0.05,
  "top_k": 20,
  "max_candidate_count": 20000,
  "tail_risk": {
    "confidence": 0.95,
    "horizon_days": 20
  },
  "minimum_cagr": 0.03
}
```

`minimum_cagr` 缺失/null 均表示关闭。response 在既有 result 中增加 `best_by_cvar` 等字段，不新增路由和 job type。

#### Apply

请求保持：

```json
{
  "objective": "min_cvar",
  "rank": 1,
  "expected_collection_updated_at": 1234567890
}
```

### 6.8 文件级改造清单

| 文件 | 动作 | 职责 |
| --- | --- | --- |
| `migrations/0029_research_cvar.sql` | 新增 | 集合 CVaR 口径 |
| `migrations/embed.go` | 校验 | migration 通过 embed 自动包含，无手工列表则不改 |
| `internal/service/research_cvar.go` | 新增 | 纯 CVaR 数学与样本门槛 |
| `internal/service/research_backtest.go` | 修改 | summary 接入 tail risk，engine v3 |
| `internal/service/research_readiness.go` | 修改 | 样本准入和 details |
| `internal/repository/research.go` | 修改 | collection 字段 round-trip 与 apply 持久化 |
| `internal/service/research_service.go` | 修改 | DTO、校验、snapshot/hash |
| `internal/service/research_optimization.go` | 修改 | config、objective、tracker、排序 |
| `internal/service/research_optimization_service.go` | 修改 | readiness/create/worker/apply |
| `internal/api/research_handlers.go` | 修改 | 严格 query 解析 |
| `web/lib/api/research.ts` | 修改 | collection/summary/optimization 类型与 API |
| `web/components/research/CollectionParamsForm.tsx` | 修改 | 尾部风险口径设置 |
| `web/components/research/OptimizationConfigDialog.tsx` | 修改 | CVaR config 和 minimum CAGR |
| `web/components/research/BacktestPanel.tsx` | 修改 | readiness details |
| `web/app/research/collections/[id]/optimizations/[optimizationId]/page.tsx` | 修改 | CVaR tab、列、apply preview |
| `web/app/research/collections/[id]/runs/[runId]/page.tsx` | 修改 | 普通回测尾部风险展示 |
| `web/lib/terms.ts` | 修改 | VaR/CVaR 术语定义 |

## 7. 前端 UIUX

### 7.1 集合参数

在 `CollectionParamsForm` 的风险相关参数区域增加“尾部风险口径”，使用两个 segmented control：

```text
置信度：90% | 95% | 99%
持有期：1 日 | 20 个有效交易日
```

默认选中集合保存值。该区域不是卡片嵌套；与无风险利率、交易成本保持同一设置层级。

置信度 tooltip：

```text
95% CVaR 表示在历史上最差 5% 的持有期场景中，平均损失是多少。
```

持有期 tooltip：

```text
20 日按回测有效收益日滚动复合，相邻场景会共享部分交易日。
```

保存前即时校验枚举。切换口径会改变后续普通回测的 input hash，旧 run 保持历史值并按现有规则显示为旧结果。

### 7.2 寻找最优组合对话框

现有对话框调整为以下顺序：

1. 权重步长下拉；
2. CVaR 置信度 segmented control；
3. CVaR 持有期 segmented control；
4. “限制最低历史年化收益” toggle；
5. toggle 开启时显示百分比输入；
6. Top K 数字输入；
7. readiness 摘要；
8. 开始调优按钮。

readiness 摘要增加：

```text
有效收益日       252
CVaR 场景        233 / 最少 100
候选数量         1,245
```

只有影响 readiness 的权重步长、confidence、horizon 进入 React Query key 并重新请求；Top K 和 minimum CAGR 不触发 readiness。请求期间保留上一份摘要，在数值旁显示小型 loading 状态，禁止卸载整个 dialog 造成抖动。

最低 CAGR 输入使用可连续编辑的小数百分比字符串状态；输入 `3.25` 表示 `0.0325`。不得在每次 keypress 用 `Number` 回写导致小数点无法输入。失焦但值未变化时不触发 readiness 请求。

按钮命令文案保持“开始调优”。对话框不展示算法宣传文字，只展示当前口径和准入事实。

### 7.3 优化结果页

tab 顺序固定为：

```text
最高收益 | 最低回撤 | 最低尾部损失 | 收益回撤平衡
```

窄屏允许 tab 行横向滚动，不压缩文字、不换成下拉菜单。

顶部 run facts 增加：

```text
CVaR 口径：20 日 / 95%
最低 CAGR：3.00% 或 未限制
```

所有结果表增加两列：

```text
VaR loss | CVaR loss
```

`最低尾部损失` tab 的“得分”列改名为“CVaR”，其他 tab 仍显示原得分。loss 的颜色规则：

- `>0` 使用 danger；
- `<=0` 使用 positive；
- 不反转符号，不把 `8% loss` 显示为 `-8%`。

表格 tooltip 展示 confidence、horizon、scenario count 和 tail count。CVaR 空榜单时：

```text
没有候选达到最低 CAGR 门槛。降低门槛或关闭该限制后重新运行调优。
```

### 7.4 应用确认

确认框在现有权重和启停预览外增加：

```text
回测窗口：YYYY-MM-DD ~ YYYY-MM-DD
尾部风险口径：20 日 / 95%
最低 CAGR：仅用于本次筛选，不写入集合
```

确认文案明确应用会同步窗口和尾部风险口径。成功后仍跳回集合页，使用现有一次性成功提示；不自动创建普通回测或 FIRE 计划。

### 7.5 普通回测结果页

总览指标增加：

```text
20 日 95% VaR
20 日 95% CVaR
最差 20 日损失
```

页面显示冻结在 run summary 中的口径，不读取集合当前设置重新标注旧 run。旧 v2 run `tail_risk` 缺失时显示：

```text
该历史回测版本未计算 CVaR
```

不得在浏览器根据 points 临时重算并伪装成旧 run 的持久化指标。

## 8. 性能与执行约束

CVaR 不改变现有硬限制：

- 最多 10 个启用资产；
- 最多 20,000 个候选；
- Top K `1..100`；
- worker 可取消、进度可查询；
- 一个候选只运行一次回测。

CVaR 对每个成功候选增加：

```text
O(T)   构造滚动持有期 loss
O(N log tail_count) 保留最大 tail losses
O(tail_count log tail_count) 排序尾部集合
O(1)   取 VaR/CVaR
```

不持久化每个候选的 loss 序列，只在 worker 内存中保留当前候选的 rolling loss、tail heap 和 Top K summary。候选结束后局部内存即可回收。

性能门禁使用固定 fixture：

- 10 个资产；
- 2,000 个候选；
- 2,520 个有效收益日；
- h=20、alpha=0.95；
- 单 worker。

比较同一提交中关闭/开启 tail-risk 计算的 benchmark，CVaR 计算阶段耗时不得超过完整候选回测耗时的 25%，峰值内存增加不得超过 20%。不满足时本功能不得验收，且不得通过降低场景数、抽样 loss 或放宽数值金标绕过。

## 9. 验证方案

### 9.1 纯数学测试

覆盖：

- 第 4 节全部金标；
- confidence 三个枚举；
- horizon 两个枚举；
- 50/100/500 场景门槛的前一项、边界项、后一项；
- fractional tail mass；
- CVaR 不小于 VaR（loss 降序定义下）；
- NaN、Inf、`r<=-1` 拒绝；
- 确定性和输入不变性。

### 9.2 回测引擎测试

- 固定 `effReturns` 的 summary tail risk 等于纯函数结果；
- 前值填充的无效日不增加场景数；
- 普通回测中零权重资产及其 FX 不改变 effective dates、CVaR 或波动率；
- 优化候选保留全部冻结资产，改变权重不会改变 effective return days 或 scenario count；
- 人为移除优化候选中的零权重资产会破坏冻结日历，必须由输入构造测试阻止；
- 全基准币种现金组合使用工作日零收益并得到 `VaR=CVaR=0`；
- 全现金且有显式窗口可运行，缺少显式窗口仍返回 `research_no_common_window`；
- FX 变化日进入有效序列；
- h=20 使用复合而非加总；
- 相同输入两次 summary JSON 完全相同；
- v3 普通 run input hash 与 v2 不同；
- v2 run 仍可读取、导出、比较。

### 9.3 Readiness 测试

- 普通和优化 readiness 使用各自正确的 spec；
- 样本不足返回 `cvar_sample_insufficient` 和精确 counts；
- 不静默改变 99%/20 日请求；
- query 非数字、未知 confidence/horizon 返回 HTTP 400；
- 数据同步后有效日增加，readiness 自动转为 ready。

### 9.4 优化器测试

- `ObjectiveMinCVaR` score 为 `-CVaR_loss`；
- CVaR、VaR、CAGR、回撤、canonical vector 五级排序稳定；
- minimum CAGR 只过滤 CVaR tracker；
- 没有 eligible candidate 时 run succeeded 且 warning/空榜单正确；
- candidate/evaluated/skipped/eligible 数量准确；
- CVaR config 和算法版本进入 input hash；
- 相同 input hash 复用 active/succeeded run；
- v2 和 v3 optimization 不互相复用；
- worker 取消、source changed 和 all candidates failed 保持现有行为。

### 9.5 Apply 测试

- `min_cvar` objective/rank 可定位正确结果；
- 应用后正权重资产启用并锁定，零权重资产禁用；
- window 和 CVaR spec 同一事务同步；
- minimum CAGR 不写入 collection；
- collection 并发变化返回 409；
- item identity 变化返回 stale；
- spec 写入失败时权重和窗口全部回滚；
- 应用旧 v2 结果仍保持原行为，不覆盖集合现有 CVaR spec。

### 9.6 Web 测试

- collection spec round-trip；
- segmented controls 与 readiness query 参数；
- minimum CAGR 小数连续输入、无修改失焦不请求；
- readiness 刷新不卸载 dialog；
- CVaR tab、列、tooltip、正负 loss 色彩；
- 空榜单 warning；
- apply preview 和成功跳转；
- 普通 run 指标；
- 旧 run 缺 tail risk 的兼容态；
- 桌面和移动端 tab/表格无重叠。

### 9.7 API 集成测试

- migration 后旧 collection 获得 0.95/20；
- collection create/get/patch/import/export 完整 round-trip；
- readiness/create/get/apply 全链路；
- result JSON 包含 `best_by_cvar`、eligible count 和 frozen summary；
- source hash 不因 spec 变化，input hash 必须变化；
- 所有成功候选的 effective return days 与 scenario count 完全一致；不一致时 run 以 `candidate_sample_mismatch` 失败。

## 10. 实施顺序与完成门禁

### 步骤 1：数学内核

- 新增 `research_cvar.go`；
- 完成公式、滚动复合、门槛和金标测试。

门禁：纯函数测试全部通过，金标容差 `1e-12`，无数据库和时间依赖。

### 步骤 2：集合配置和 migration

- migration 0029；
- repository/service DTO；
- CRUD、导入导出、默认值与校验；
- snapshot/hash。

门禁：fresh DB、0028 upgrade、round-trip、hash 测试全部通过。

### 步骤 3：普通回测 v3

- `BacktestInput/Summary` 接入；
- readiness 样本门槛；
- run result 展示所需 API 字段；
- v2 replay 兼容。

门禁：现有 v2 fixture 数值不变，新 v3 fixture CVaR 金标准确。

### 步骤 4：自动调优 v4

- config/hash/snapshot；
- CVaR tracker 和排序；
- minimum CAGR；
- result/apply 原子同步。

门禁：候选计数不变，四组 Top K、空榜单、复用、取消、apply/rollback 测试全部通过。

### 步骤 5：前端 UIUX

- collection settings；
- config dialog/readiness；
- result tab/table/apply；
- ordinary run tail risk。

门禁：Vitest 通过；使用 Playwright 分别截取桌面 `1440x900`、移动 `390x844`，确认无重叠、无裁切、无整区抖动。

### 步骤 6：全链路

执行：

```bash
gofmt -w <本次修改的 Go 文件>
go test ./...
go vet ./...
cd web && npm run lint
cd web && npm run test:ci
cd web && npm run build
make ci
```

性能执行：

```bash
go test ./internal/service -run '^$' -bench 'BenchmarkResearchOptimizationCVaR' -benchmem
```

全部自动测试、构建和第 8 节性能门禁通过后才能进入手工验收。

## 11. 手工验收流程

准备一个包含股票、债券、外币 ETF 和 CNY 现金、至少两年共同历史的研究集合。

1. 保存集合 CVaR 为 `95% / 20 日`，刷新后确认字段保持；
2. 运行普通回测，记录 VaR、CVaR、worst loss 与 scenario count；
3. 打开“寻找最优组合”，确认默认继承 95%/20 日；
4. 切换 99%，确认若样本不足 readiness 明确阻断且不回退；
5. 恢复 95%，设置 5% 权重步长、不开 minimum CAGR，运行优化；
6. 结果页确认四个 tab 均有稳定排名，CVaR tab 按 loss 升序；
7. 开启 minimum CAGR 并设置高于全部候选的值重跑，确认 CVaR 空榜单而其他榜单仍存在；
8. 使用合理 minimum CAGR 重跑，应用 CVaR 第一名；
9. 回到集合核对权重、启用/锁定、窗口和 CVaR spec 一次性同步；
10. 核对四个榜单中所有结果的 effective return days 与 scenario count 完全相同；应用后运行普通回测，若样本数因零权重资产禁用而变化，确认页面指标按普通回测口径展示且不宣称逐位复现；
11. 修改集合后尝试应用旧结果，确认返回并发/stale 错误且没有部分写入；
12. 将应用后的集合复制到 FIRE 计划草稿，确认没有隐式运行模拟；
13. 在计划中确认资产映射后运行 FIRE 模拟，对比原组合与 CVaR 组合的成功率和真实财富分位数；
14. 打开历史 `research_backtest_v2` / `research_optimizer_v2` 结果，确认可查看且明确无 CVaR。

验收记录至少包含：集合 id、run/optimization id、input/source hash、配置、四组第一名、应用前后集合 JSON、普通回测 CVaR 对比和 FIRE 对比结果。

## 12. 最终验收清单

- [x] CVaR 只使用回测 `effReturns`，没有第二套收益口径
- [x] VaR/CVaR loss 符号、滚动复合和 fractional tail 公式正确
- [x] 90/95/99% 与 1/20 日枚举和样本门槛无静默 fallback
- [x] 普通回测与优化复用同一纯函数
- [x] `min_cvar` 在现有离散候选域中稳定排序
- [x] 所有优化候选冻结相同有效收益日与尾部场景数
- [x] minimum CAGR 只影响 CVaR 榜单，null 语义正确
- [x] collection、snapshot、config/input hash 完整冻结
- [x] apply 原子同步权重、窗口和 CVaR spec
- [x] 旧 v2 run/result 可读、可应用且不被改写
- [x] 前端无小数输入问题、无整区抖动、无移动端重叠
- [x] 数学、service、API、worker、web、性能和运行态验收全部完成

## 13. 能力边界

实施后系统能够回答：

- 在相同历史窗口和再平衡规则下，哪个候选组合的尾部平均损失更低；
- 收益门槛存在时，哪个候选在满足门槛的集合中 CVaR 最低；
- 同一冻结样本上，哪个候选的历史尾部平均损失更低；
- 该权重进入 FIRE 前瞻模拟后，成功率和财富分位数如何变化。

系统不能据此断言：

- 历史 CVaR 等于未来 CVaR；
- CVaR 最低的组合一定具有最高 FIRE 成功率；
- 重叠历史场景相互独立；
- 99% CVaR 在样本刚达门槛时已经足够稳定；
- CVaR 可以替代最大回撤、流动性风险、相关性突变或现金流压力测试。

因此 CVaR 是组合研究的尾部风险优化目标，不是收益预测器，也不是 FIRE 成功率的替代指标。
