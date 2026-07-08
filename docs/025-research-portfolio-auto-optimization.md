# 研究组合自动调优回测

- 方案来源：`td/103-research-portfolio-auto-optimization.md`
- 实施 review：`td/104-td103-implementation-review.md`、`td/105-td104-implementation-review.md`、`td/106-td105-implementation-review.md`
- 定位：在组合研究集合中，对启用基金自动枚举候选权重组合，运行多轮历史回测，并给出最高收益、最低回撤、收益回撤平衡三组结果。自动调优只展示结果，不写回当前集合权重。

## 1. 使用规则

- 仅启用资产参与普通回测和自动调优。
- 自动调优最多支持 10 个启用资产。
- 已锁定资产的权重保持不变。
- 未锁定资产均可被调优重新分配权重，包括当前权重大于 0 的资产。
- 启用且权重为 0 的资产表示参与调优，但当前没有手动配置权重。
- 锁定权重合计不能超过 100%。
- 普通回测仍要求启用资产权重合计为 100%；自动调优不要求当前权重合计为 100%。

权重表会提示每只资产在自动调优中的状态：

- `固定权重`：启用且已锁定。
- `参与调优`：启用、未锁定、权重为 0。
- `可被调优`：启用、未锁定、权重大于 0。
- `不参与`：未启用。

## 2. 入口与流程

集合详情页的回测区域提供两个入口：

- `运行回测`：按当前权重创建普通不可变 run。
- `寻找最优组合`：打开自动调优配置弹窗。

自动调优配置项：

- 权重步长：默认 5%，可选 1%、2.5%、5%、10%。
- Top K：每组目标保留的结果数量，默认 20。
- 候选数量预估：按当前启用资产、锁定权重和步长实时计算。

启动后跳转到：

```text
/research/collections/{collectionId}/optimizations/{optimizationId}
```

结果页展示：

- 状态、创建时间、回测窗口、基准币种、再平衡策略。
- 权重步长、Top K、候选数量、已评估数量。
- 运行中进度。
- 成功后三个结果 tab：
  - 最高收益
  - 最低回撤
  - 收益回撤平衡

## 3. 优化目标

一次自动调优运行同时产出三组榜单。

### 最高收益

按 CAGR 从高到低排序。CAGR 不可用时可回退到累计收益。

### 最低回撤

按最大回撤从大到小排序。由于最大回撤是负数，`-5%` 优于 `-20%`。

### 收益回撤平衡

按 Calmar 从高到低排序。Calmar 不可用时，后端使用 `CAGR / abs(max_drawdown)` 作为兜底；最大回撤为 0 或不可定义的候选会被跳过。

## 4. 候选权重生成

后端根据启用资产生成候选：

1. 将启用资产拆成锁定资产和可调资产。
2. 锁定资产保持原权重。
3. 计算剩余权重：`remaining = 1 - lockedSum`。
4. 从可调资产中枚举所有非空子集。
5. 对每个子集，按权重步长拆分剩余权重。
6. 子集内每个被选中的资产至少获得一个步长单位权重。
7. 未选中的可调资产权重为 0。
8. 每个候选总权重精确归一到 100%。

示例：

- A：20%，已锁定
- B：0%，未锁定
- C：0%，未锁定
- 步长：5%

后端会保持 A=20%，并在 B/C 间分配剩余 80%，例如：

- A 20% + B 80%
- A 20% + C 80%
- A 20% + B 40% + C 40%
- A 20% + B 30% + C 50%

## 5. 准入与限制

自动调优有独立 readiness：

```http
GET /api/v1/research/collections/{id}/optimization-readiness?weight_step=0.05
```

阻断条件：

- 集合已归档。
- 启用资产为 0。
- 启用资产超过 10。
- 锁定权重合计超过 100%。
- 候选数量超过本次 `max_candidate_count`。
- 存在缺历史资产。
- 存在历史同步中或同步失败且无可用旧数据。
- 存在 FX 缺失、同步中或缺口超限。
- 共同窗口为空、过短或有效估值日不足。

候选数量默认上限为 20000。调用创建接口时可通过 `max_candidate_count` 进一步收紧本次上限；超过上限会拒绝创建任务。

## 6. 数据模型与 API

新增 migration：

```text
migrations/0026_research_optimization.sql
```

新增表：

```text
research_optimization_runs
```

核心字段：

- `id`
- `collection_id`
- `job_id`
- `status`
- `input_hash`
- `source_hash`
- `engine_version`
- `config_json`
- `input_snapshot_json`
- `candidate_count`
- `evaluated_count`
- `result_json`
- `error_code`
- `error_message`
- `created_at`
- `completed_at`

新增 API：

```http
GET  /api/v1/research/collections/{id}/optimization-readiness
POST /api/v1/research/collections/{id}/optimizations
GET  /api/v1/research/collections/{id}/optimizations/latest
GET  /api/v1/research/optimizations/{optimizationId}
```

创建请求示例：

```json
{
  "weight_step": 0.05,
  "max_candidate_count": 20000,
  "top_k": 20
}
```

## 7. Worker 与审计

自动调优使用独立 job 类型：

```text
research_optimization_backtest
```

执行流程：

1. 创建 optimization run 和 job。
2. 冻结 input snapshot。
3. 计算 `source_hash` 和 `input_hash`。
4. 相同输入的 running/succeeded optimization 可复用。
5. worker 执行时重新加载冻结输入并校验 source hash。
6. 枚举候选权重。
7. 对每个候选调用现有 `RunResearchBacktest`。
8. 维护三组 Top K 结果。
9. 完成后写入 `result_json`。

管理后台 job 过滤支持：

```http
GET /api/v1/admin/jobs?type=research_optimization_backtest
```

## 8. 验证

主要自动化验证：

```bash
go test ./...
cd web && npm run lint
cd web && npm run test:ci
```

重点覆盖：

- 候选生成与计数一致。
- 锁定权重保持不变。
- 未锁定正权重资产可被调整。
- 候选权重合计为 100%。
- 三类目标排序正确。
- `max_candidate_count` 生效。
- 自动调优不受普通权重合计不足阻断。
- 缺历史和 FX 问题仍阻断自动调优。
- 管理后台 job 类型支持自动调优。
- 前端普通回测禁用原因和自动调优禁用原因互不覆盖。
