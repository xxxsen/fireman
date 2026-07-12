# 研究组合自动调优回测

- 定位：在组合研究集合中，对启用基金自动枚举候选权重组合，运行多轮历史回测，并给出最高收益、最低回撤、收益回撤平衡三组结果。用户可将任一调优结果应用回当前集合，使组合权重、启用状态、锁定状态和回测区间与该次调优结果保持一致。

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

两个入口始终独立展示，按钮宽度一致，并按各自准入条件决定是否可用：

- `运行回测` 要求普通回测 readiness 通过，包括启用资产权重合计为 100%。
- `寻找最优组合` 只要求自动调优 readiness 通过。一个可调资产会取得全部剩余权重；全部锁定且合计 100% 时允许生成一个固定候选并展示提示。
- 权重合计不为 100% 时，数据状态区不再展示为数据阻断；`运行回测` 禁用，`寻找最优组合` 可在满足调优 readiness 时继续执行。
- 禁用按钮通过 hover 展示当前不可用原因。

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
- 指标中文展示，包括年化收益率、最大回撤、夏普比率、卡玛比率。
- 夏普比率和卡玛比率提供 hover 解释。
- 每条结果支持一键应用到当前组合。

## 3. 应用调优结果

结果页每条调优结果提供 `应用` 操作。点击后会弹出确认窗口，展示目标组合、启用并锁定资产数量、取消启用资产数量、权重合计和该次调优的回测区间。

确认应用后，页面只发送一次 `POST /api/v1/research/optimizations/{optimizationId}/apply`：

- 调优结果中权重大于 0 的资产会自动启用、写入调优权重，并锁定权重。
- 调优结果中权重为 0 或未出现在结果中的资产会取消启用、取消锁定，并将权重重置为 0。
- 如果调优结果引用的资产已不在当前组合中，应用会被阻止，并提示重新运行调优。
- 组合回测区间会同步为该次调优任务的 `window_start ~ window_end`，`start_policy` 写为 `custom_range`。
- 应用成功后跳转回组合页，并展示一次性成功提示。
- 请求携带预览时的 `expected_collection_updated_at`。集合被并发修改时返回 `409 research_collection_changed`；结果身份与当前条目不匹配时返回 `409 research_optimization_result_stale`。
- 所有 item、锁定状态、权重、回测区间和集合版本在同一数据库事务中提交；任一步失败全部回滚，不会留下半应用状态。

同步回测区间是必要行为。调优结果的收益、回撤、夏普比率和卡玛比率都基于该次调优任务冻结的窗口；应用后同步区间可以保证用户回到组合页直接运行普通回测时，结果与调优页展示的数据口径一致。

## 4. 优化目标

一次自动调优运行同时产出三组榜单。

### 最高收益

按 CAGR 从高到低排序。CAGR 不可用时可回退到累计收益。

### 最低回撤

按最大回撤从大到小排序。由于最大回撤是负数，`-5%` 优于 `-20%`。

### 收益回撤平衡

按 Calmar 从高到低排序。Calmar 不可用时，后端使用 `CAGR / abs(max_drawdown)` 作为兜底；最大回撤为 0 或不可定义的候选会被跳过。

## 5. 候选权重生成

后端根据启用资产生成候选：

1. 将启用资产拆成锁定资产和可调资产。
2. 锁定资产保持原权重。
3. 计算剩余权重：`remaining = 1 - lockedSum`。
4. 计算 `full_parts=floor(remaining/weight_step)` 和不足一步长的 `residual`。
5. residual 为 0 时，对每个非空子集枚举正整数 composition；residual 大于 0 时，依次选择 residual 接收资产，并把完整步长按非负整数分配，非接收资产至少取得一个完整步长。
6. `full_parts=0` 时每个可调资产分别形成一个独占 residual 的候选；`remaining` 在 `1e-12` 内为 0 时只形成一个可调权重全 0 的固定候选。
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

## 6. 准入与限制

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
- 候选数量为 0（`candidate_count_zero`）。
- 存在缺历史资产。
- 存在历史同步中或同步失败且无可用旧数据。
- 存在 FX 缺失、同步中或缺口超限。
- 共同窗口为空、过短或有效估值日不足。

候选数量推荐控制在 20000 以内。超过推荐值时 readiness 返回性能 warning，配置弹窗展示实际数量并说明耗时和内存占用会急剧增加，但不阻止创建任务；真正无法生成候选时才阻断。

## 7. 数据模型与 API

相关表位于单一 schema 基线：

```text
migrations/0001_init.sql
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
POST /api/v1/research/optimizations/{optimizationId}/apply
```

创建请求示例：

```json
{
  "weight_step": 0.05,
  "max_candidate_count": 20000,
  "top_k": 20
}
```

## 8. Worker 与审计

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

优化器版本为 `research_optimizer_v2`。快照逐资产冻结 `item_id`、`asset_key`、权重和锁定标记；候选回测携带与普通回测相同的基准。Top K 使用目标分数降序、CAGR 降序、绝对回撤升序、12 位 canonical 权重向量升序的确定排序，并按 canonical 权重去重。

管理后台统一任务过滤支持：

```http
GET /api/v1/admin/worker-tasks?type=research_optimization_backtest
```

## 9. 已移除的筛选器能力

组合研究主流程不再提供资产筛选器入口：

- 组合研究列表页不再展示 `资产筛选器`。
- 组合详情页不再展示 `从筛选器添加`。
- `/research/screener` 页面、筛选器面板、候选池、候选比较弹窗、前端 saved filters API、后端 saved filters 接口和服务代码已移除。

保留的能力：

- `资产与权重` 中的 `添加资产` 仍可搜索并添加基金。
- `GET /api/v1/research/assets`、`listResearchAssets` 和底层资产搜索服务保留，作为添加资产和其他研究组件的基础能力。

## 10. 资产与权重交互

- 资产列表不再支持拖拽排序，按后端返回顺序展示。
- `添加资产` 弹窗高度固定，搜索结果数量变化时仅列表区域滚动，弹窗整体不再随结果数量跳动。

## 11. 验证

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
- 权重合计不足时，数据状态区不展示 `weight_sum_invalid` 为阻断，但普通回测按钮仍禁用并展示原因。
- 自动调优结果可应用回组合，正权重资产启用并锁定，零权重或未出现资产取消启用。
- 应用调优结果会同步该次调优任务的回测区间，保证后续普通回测口径一致。
- 筛选器入口和 saved filters 相关代码链路已移除，`添加资产` 搜索能力保持可用。
- 资产列表不可拖拽，添加资产弹窗高度固定。
