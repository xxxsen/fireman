# 研究组合自动调优回测方案设计

## 背景

当前研究组合回测依赖用户手动给每个启用基金配置权重，然后按这组固定权重生成回测结果。这个流程适合验证用户已有配置，但不能帮助用户在一组候选基金中寻找更优权重组合。

新增“自动调优回测”能力：用户只需要放入一组启用基金，可以不给部分基金配置权重。系统在回测时自动枚举候选基金子集与权重分配，跑多轮回测，并一次给出三组结果：

- 最高收益组合
- 最低回撤组合
- 收益回撤平衡组合

## 已确认产品规则

1. 仅启用资产参与自动调优和回测。
2. 启用资产中，权重为 0 表示该资产需要参与自动调优。
3. 启用资产中，只有点击“锁定”的权重才必须保持不变。
4. 启用资产中，权重大于 0 但未锁定，也可以被自动调优调整。
5. 自动调优一次运行给出 3 组结果：最高收益、最低回撤、收益回撤平衡。
6. 默认权重步长为 5%，用户可以手动调整。
7. 一期约束：开启自动调优时，启用资产数量最多 10 个。
8. 一期只展示调优结果，不自动写回当前集合权重。

## 核心定义

### 普通回测

普通回测沿用现有逻辑：

- 启用资产权重合计必须为 100%。
- 每个资产按当前集合权重进入 `RunResearchBacktest`。
- 生成一个不可变 run。

### 自动调优回测

自动调优回测是独立流程：

- 不要求启用资产原始权重合计为 100%。
- 锁定资产的权重固定。
- 未锁定资产均可被自动调优调整，包括当前权重大于 0 的资产和权重为 0 的资产。
- 系统为未锁定资产分配锁定资产之外的剩余权重。
- 系统从未锁定资产中选取 1-N 个资产参与每个候选组合。
- 每个候选组合调用现有研究回测引擎计算结果。
- 最终按不同目标筛选出三组最优结果。

示例：

启用资产 A/B/C：

- A 权重 20%，已锁定
- B 权重 0%，未锁定
- C 权重 0%，未锁定

自动调优时：

- A 固定 20%
- B/C 分配剩余 80%
- 候选组合包括：
  - A 20% + B 80%
  - A 20% + C 80%
  - A 20% + B 40% + C 40%
  - A 20% + B 30% + C 50%
  - 其他按权重步长生成的组合

如果 A 权重 20% 但未锁定，则 A 也属于可调资产，其最终权重可以被调整。

## 目标函数

一次自动调优运行同时输出三类结果。

### 最高收益

排序指标：

```text
CAGR 最大
```

如 CAGR 不可用，可回退到累计收益 `cumulative_return`。

### 最低回撤

排序指标：

```text
max_drawdown 绝对值最小
```

由于当前回测中 `max_drawdown` 为负值，排序可以按 `max_drawdown` 从大到小，即 `-5%` 优于 `-20%`。

### 收益回撤平衡

排序指标：

```text
Calmar 最大
```

当前 `BacktestSummary` 已包含 `calmar`，优先复用。若 Calmar 为空，可用：

```text
CAGR / abs(max_drawdown)
```

作为兜底，但需要跳过 `max_drawdown = 0` 或不可定义的候选。

## 权重搜索规则

输入：

- `enabledAssets`：启用资产列表，最多 10 个。
- `lockedAssets`：启用且 `weight_locked = true` 的资产。
- `tunableAssets`：启用且 `weight_locked = false` 的资产。
- `lockedSum`：锁定资产权重合计。
- `remaining = 1 - lockedSum`。
- `weightStep`：权重步长，默认 0.05。

准入规则：

- 启用资产数量必须在 `[1, 10]`。
- 锁定权重合计不能超过 100%。
- 至少存在一个未锁定资产，或者锁定权重刚好等于 100%。
- 未锁定资产数量为 0 且锁定权重为 100% 时，自动调优退化为固定组合回测，但 UI 应提示用户普通回测即可。
- 每个候选组合总权重必须等于 100%。
- 每个候选组合中未选中的未锁定资产权重为 0。
- 锁定资产权重保持原值。

候选生成：

1. 对 `tunableAssets` 生成所有非空子集。
2. 对每个子集，将 `remaining` 按 `weightStep` 拆分成整数份。
3. 每个子集成员至少分到 1 份权重，避免选中资产实际权重为 0。
4. 对每个拆分生成完整权重向量。
5. 对完整权重向量调用现有回测引擎。

示例：`remaining = 0.8`，`weightStep = 0.05`，则剩余权重有 16 份。

如果选择 B/C 两个资产，合法拆分包括：

- B 0.05 / C 0.75
- B 0.10 / C 0.70
- ...
- B 0.75 / C 0.05

## 组合数量控制

一期上限：启用资产最多 10 个。

仍需在启动前计算候选组合数量，并在 UI 中展示预估数量。若候选数量超过系统上限，则拒绝创建任务。

建议默认系统上限：

```text
max_candidate_count = 20000
```

超过时提示用户：

- 增大权重步长
- 减少启用资产
- 锁定部分资产

候选数量计算必须和实际生成算法一致，避免 UI 预估可运行但后端拒绝。

## 后端设计

### 新增 Job Type

新增 worker job type：

```text
research_optimization_backtest
```

自动调优任务与普通回测任务分开，避免污染现有 run 语义。

### 新增接口

创建自动调优：

```http
POST /api/v1/research/collections/{id}/optimizations
```

请求体：

```json
{
  "weight_step": 0.05,
  "max_candidate_count": 20000,
  "top_k": 20
}
```

字段说明：

- `weight_step`：权重步长，默认 0.05。
- `max_candidate_count`：本次允许评估的最大候选数，默认 20000，不能超过服务端硬上限。
- `top_k`：每个目标保留的候选数量，默认 20。

查询自动调优：

```http
GET /api/v1/research/optimizations/{optimizationId}
```

返回调优状态、进度、配置、三组结果。

### 数据模型

建议新增表：

```text
research_optimization_runs
```

字段：

```text
id
collection_id
job_id
status
input_hash
source_hash
engine_version
base_currency
rebalance_policy
window_start
window_end
config_json
input_snapshot_json
candidate_count
evaluated_count
result_json
error_code
error_message
created_at
completed_at
```

`status` 复用 run 状态：

```text
queued | running | succeeded | failed | canceled
```

`result_json` 结构：

```json
{
  "candidate_count": 1280,
  "evaluated_count": 1280,
  "best_by_cagr": [],
  "best_by_drawdown": [],
  "best_by_calmar": []
}
```

单个 result：

```json
{
  "rank": 1,
  "objective": "max_calmar",
  "score": 0.82,
  "weights": [
    {
      "item_id": "rci_a",
      "asset_key": "CN|fund||A",
      "name": "基金A",
      "weight": 0.2,
      "locked": true
    },
    {
      "item_id": "rci_b",
      "asset_key": "CN|fund||B",
      "name": "基金B",
      "weight": 0.35,
      "locked": false
    }
  ],
  "summary": {
    "cumulative_return": 1.2,
    "cagr": 0.12,
    "annual_volatility": 0.18,
    "max_drawdown": -0.16,
    "sharpe": 0.7,
    "calmar": 0.75
  }
}
```

一期只保存结果摘要和权重，不要求保存每个候选的完整日度曲线。

### Input Snapshot

自动调优必须冻结输入，保证可审计和可复用。

`input_snapshot_json` 建议包含：

```json
{
  "engine_version": "research_optimizer_v1",
  "source_hash": "...",
  "common_start": "2020-01-01",
  "common_end": "2026-07-01",
  "window_start": "2020-01-01",
  "window_end": "2026-07-01",
  "collection": {},
  "assets": [],
  "fx": [],
  "locked_weights": {},
  "tunable_item_ids": [],
  "config": {
    "weight_step": 0.05,
    "top_k": 20,
    "max_candidate_count": 20000
  }
}
```

`input_hash` 应包含：

- collection 参数
- enabled assets
- locked weights
- tunable asset list
- optimization config
- source hash
- optimizer engine version

相同输入可以复用已有 succeeded/running optimization。

### Readiness

新增 optimization readiness，不复用普通回测的权重合计限制。

可选实现：

```http
GET /api/v1/research/collections/{id}/optimization-readiness?weight_step=0.05
```

也可以在创建接口内部执行同样检查，一期 UI 可根据已有 detail + readiness 做本地预估，后端仍必须兜底。

优化准入检查：

- 集合必须 active。
- 启用资产数量 `1..10`。
- 锁定权重合计 `<= 1`。
- 普通数据依赖必须满足：
  - 无缺历史
  - 无活跃同步阻塞
  - FX 数据满足要求
  - 共同窗口满足最短要求
- 候选数量不能超过上限。

普通 readiness 中的 `weight_sum_invalid` 不应阻塞自动调优。

### Worker 执行流程

1. 根据 job payload 读取 optimization run。
2. 反序列化 input snapshot。
3. 重新加载原始点位并验证 source hash。
4. 生成候选权重。
5. 逐个候选构造 `BacktestInput`。
6. 调用 `RunResearchBacktest`。
7. 提取 summary 和权重。
8. 维护三组 top K：
   - `best_by_cagr`
   - `best_by_drawdown`
   - `best_by_calmar`
9. 定期更新 `evaluated_count`，供前端显示进度。
10. 成功后写入 `result_json` 和 completed 状态。

失败处理：

- 单个候选因为数值异常失败，应记录 skipped count 和原因；如果所有候选失败，则任务 failed。
- source hash 变化时任务 failed，提示用户重新创建调优。
- 用户取消任务时状态设为 canceled。

## 前端设计

### 集合页入口

在回测卡片中新增按钮：

```text
寻找最优组合
```

按钮和普通“运行回测”并列。

按钮禁用条件：

- readiness 仍在加载。
- 数据依赖未就绪。
- 启用资产数量为 0。
- 启用资产数量超过 10。
- 锁定权重合计超过 100%。
- 候选组合数量超过上限。

按钮不应因为普通权重合计不足 100% 禁用。

### 权重编辑提示

在权重表中增强状态表达：

- 启用 + 已锁定：`固定权重`
- 启用 + 未锁定 + 权重 0：`参与调优`
- 启用 + 未锁定 + 权重 > 0：`可被调优`
- 未启用：`不参与`

这可以减少用户误解：未锁定的正权重不是固定权重。

### 配置弹窗

点击“寻找最优组合”后打开配置弹窗。

字段：

- 权重步长：默认 5%，可选 1%、2.5%、5%、10%。
- 每组保留数量 Top K：默认 20。
- 候选数量预估：只读展示。

文案：

```text
自动调优会保持锁定资产权重不变，并在未锁定资产之间分配剩余权重。
权重为 0 且启用的资产会参与调优。
一期最多支持 10 个启用资产。
```

### 调优结果页

新增页面：

```text
/research/collections/{id}/optimizations/{optimizationId}
```

展示：

- 状态、进度、候选数量、已评估数量。
- 配置摘要：步长、Top K、窗口、基准币种、再平衡策略。
- 三个结果区块或 Tab：
  - 最高收益
  - 最低回撤
  - 收益回撤平衡

每个候选展示：

- 排名
- CAGR
- 累计收益
- 最大回撤
- 波动率
- Sharpe
- Calmar
- 权重分配条形图或表格

一期不提供“应用权重到集合”能力。

### 列表入口

集合详情页可以展示最近一次自动调优结果入口。

后续可新增：

```text
/research/collections/{id}/optimizations
```

作为全部自动调优历史列表。一期可不做，若不做，需要从创建成功后直接跳转到结果页。

## API 类型设计

前端新增类型：

```ts
export interface ResearchOptimizationConfig {
  weight_step: number;
  max_candidate_count?: number;
  top_k?: number;
}

export interface ResearchOptimizationResultItem {
  rank: number;
  objective: "max_cagr" | "min_drawdown" | "max_calmar";
  score: number;
  weights: {
    item_id: string;
    asset_key: string;
    name: string;
    weight: number;
    locked: boolean;
  }[];
  summary: ResearchRunSummary;
}

export interface ResearchOptimizationRun {
  id: string;
  collection_id: string;
  job_id: string;
  status: ResearchRunStatus;
  config: ResearchOptimizationConfig;
  candidate_count: number;
  evaluated_count: number;
  result?: {
    best_by_cagr: ResearchOptimizationResultItem[];
    best_by_drawdown: ResearchOptimizationResultItem[];
    best_by_calmar: ResearchOptimizationResultItem[];
  };
  created_at: number;
  completed_at?: number | null;
}
```

## 测试计划

### 后端单元测试

- 候选生成：
  - 0 个启用资产拒绝。
  - 超过 10 个启用资产拒绝。
  - 锁定权重超过 100% 拒绝。
  - 锁定 20%，两个未锁定资产，5% 步长能生成合法权重。
  - 未锁定正权重资产会被调优，不保持原权重。
  - 所有候选权重合计为 100%。

- 目标排序：
  - CAGR 最大候选进入 `best_by_cagr[0]`。
  - 最大回撤最小候选进入 `best_by_drawdown[0]`。
  - Calmar 最大候选进入 `best_by_calmar[0]`。

- readiness：
  - 普通权重合计不足不阻塞自动调优。
  - 数据缺历史仍阻塞自动调优。
  - FX 缺失仍阻塞自动调优。

- job：
  - 创建 optimization run。
  - 相同 input hash 复用已有 running/succeeded optimization。
  - source hash 改变后不复用。

### 前端测试

- 权重表状态文案：
  - 已锁定显示固定权重。
  - 未锁定 0 权重显示参与调优。
  - 未锁定正权重显示可被调优。

- 按钮状态：
  - 权重不足 100% 时普通回测禁用，但自动调优可用。
  - 启用资产超过 10 个时自动调优禁用。
  - 锁定权重超过 100% 时自动调优禁用。

- 配置弹窗：
  - 默认步长为 5%。
  - 修改步长会更新候选数量预估。
  - 提交后调用创建接口并跳转结果页。

- 结果页：
  - running 状态展示进度。
  - succeeded 状态展示三组榜单。
  - failed 状态展示错误。

## 验收标准

- 用户可以在集合中启用最多 10 个基金，其中部分或全部权重为 0。
- 用户点击“寻找最优组合”可以启动自动调优。
- 自动调优保持锁定资产权重不变。
- 未锁定资产，包括当前权重大于 0 的资产，都允许被重新分配权重。
- 默认按 5% 步长生成候选，用户可以调整步长。
- 系统一次输出三组结果：最高收益、最低回撤、收益回撤平衡。
- 自动调优不要求当前权重合计为 100%。
- 自动调优仍要求历史数据、FX 和共同窗口满足回测要求。
- 一期不会自动修改当前集合权重。

## 推荐实施顺序

1. 实现纯候选生成与目标排序模块。
2. 实现后端 optimization run 数据模型、repository 和 migration。
3. 实现创建/查询 optimization API。
4. 接入 worker job 执行自动调优。
5. 前端新增“寻找最优组合”入口和配置弹窗。
6. 前端新增调优结果页。
7. 补齐前后端测试和端到端手工验收。

## 暂不纳入一期

- 自动把最优权重写回集合。
- 每个候选保存完整日度曲线。
- 随机搜索、遗传算法、贝叶斯优化等近似优化。
- 超过 10 个启用资产的自动调优。
- 多目标约束输入，例如“CAGR 至少 X 且回撤低于 Y”。
