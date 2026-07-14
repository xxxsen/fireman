# 研究组合自动调优回测

- 状态：已实施
- 当前优化器版本：`research_optimizer_v6`
- 使用的回测版本：`research_backtest_v4`
- CVaR 算法版本：`empirical_cvar_v1`
- 定位：在研究集合冻结的数据与计算口径上枚举离散权重候选，分别保留最高收益、最低回撤、最高收益回撤比和最低尾部损失的 Top K。调优不会自动修改集合或 FIRE 计划。

## 1. 使用规则

- 只有启用资产参与普通回测和自动调优，自动调优最多支持 10 个启用资产。
- 已锁定资产的权重保持不变；锁定权重合计不能超过 100%。
- 未锁定资产均可重新分配，包括当前正权重资产和零权重资产。
- 普通回测要求启用资产权重合计为 100%；自动调优不要求当前权重已经配平。
- 一个可调资产会取得全部剩余权重；全部资产均锁定且合计 100% 时仍可生成一个固定候选。
- 所有候选都使用同一冻结窗口、有效收益日历、基准、交易成本和 CVaR 口径，避免不同候选因样本不同而不可比较。

集合权重表使用以下状态帮助用户理解候选范围：

- `固定权重`：启用且已锁定；
- `参与调优`：启用、未锁定、当前权重为 0；
- `可被调优`：启用、未锁定、当前权重大于 0；
- `不参与`：未启用。

## 2. 入口与流程

集合详情页提供两个独立入口：

- `运行回测`：使用当前正权重配置创建普通不可变 run；
- `寻找最优组合`：使用自动调优 readiness 打开配置窗口。

自动调优配置包括：

- 权重步长，默认 5%，界面提供 1%、2.5%、5%、10%；
- CVaR 置信度：90%、95% 或 99%；
- CVaR 持有期：1 或 20 个有效收益日；
- 可选的最低历史 CAGR，只过滤最低 CVaR 榜单；
- 每类目标保留的 Top K，范围 1 到 100，默认 20；
- `max_candidate_count` 候选规模建议阈值，默认 20000；超过时给出性能 warning，但不改变候选集合。

配置窗口按当前锁定权重、可调资产和步长展示精确候选数量，同时展示有效收益日、CVaR 场景数、最少样本数、blocking reasons 和 warnings。创建成功后进入：

```text
/research/collections/{collectionId}/optimizations/{optimizationId}
```

结果页跟踪统一 worker task 状态，展示冻结窗口、基准币种、再平衡策略、权重步长、CVaR 口径、最低 CAGR、候选数与已评估数。成功后展示四个 tab：

- 最高收益；
- 最低回撤；
- 收益回撤平衡；
- 最低尾部损失。

## 3. 四类优化目标

### 3.1 最高收益

按 CAGR 从高到低排序。

### 3.2 最低回撤

按最大回撤从大到小排序。最大回撤使用负数表示，因此 `-5%` 优于 `-20%`。

### 3.3 收益回撤平衡

按 Calmar 从高到低排序。Calmar 不可用时使用 `CAGR / abs(max_drawdown)`；最大回撤为 0 或指标不可定义的候选不进入该榜单。

### 3.4 最低尾部损失

按 CVaR loss 从低到高排序。CVaR 相同时依次比较 VaR、CAGR、最大回撤和 canonical 权重向量。可选 `minimum_cagr` 只决定候选是否有资格进入该榜单，不影响另外三类榜单。

CVaR 使用候选扣除交易成本后的实际组合收益路径，不能由单资产 CVaR 加权得到。VaR/CVaR 都以 loss 表示；当尾部场景仍为正收益时允许为负数。详细公式与样本门槛见 [030-research-cvar-optimization.md](./030-research-cvar-optimization.md)。

## 4. 候选权重生成

后端按启用资产生成候选：

1. 拆分锁定资产和可调资产；
2. 固定锁定权重并计算 `remaining = 1 - locked_sum`；
3. 按权重步长和 residual 枚举非空可调资产子集及其整数 composition；
4. 未选中的可调资产显式取 0；
5. 按 `item_id` 排序的 12 位 canonical 权重向量去重；
6. 将每个候选确定性归一到 100%，同时保持锁定权重不变。

`remaining` 小于一个步长时，每个可调资产分别形成一个独占剩余权重的候选；`remaining` 在 `1e-12` 内为 0 时只生成一个固定候选。候选计数与惰性生成器遵循同一组合语义；候选数量超过 `max_candidate_count` 时 readiness 给出性能 warning，但当前实现不据此阻断创建。

## 5. readiness 与限制

自动调优使用独立接口：

```http
GET /api/v1/research/collections/{id}/optimization-readiness
```

查询参数可覆盖 `weight_step`、`cvar_confidence` 与 `cvar_horizon_days`。主要阻断条件包括：

- 集合已归档，或没有启用资产；
- 启用资产超过 10；
- 锁定权重超过 100%；
- 当前锁定权重与步长无法生成候选；
- 资产历史、FX、共同窗口或有效估值日不满足要求；
- 当前 CVaR 口径的场景样本不足。

候选数量超过 `max_candidate_count` 是 warning，不是 blocker。普通回测的 `weight_sum_invalid` 也不阻断自动调优。readiness 不会为凑足样本而降低置信度、缩短持有期或改用不同收益日历。

## 6. 冻结执行与确定排序

创建调优任务时，服务端冻结集合配置、条目身份、锁定权重、历史/FX/基准摘要、交易成本和 CVaR 口径，并计算 `source_hash` 与 `input_hash`。相同集合与 `input_hash` 的活动或已完成调优可复用。

新调优 run 与 `research_optimization_backtest` worker task 在同一事务内创建。worker 执行前校验冻结来源，使用受限并发逐个调用 `RunResearchBacktest`，并维护四组 Top K。所有成功候选必须具有相同有效收益日和 CVaR 场景数，否则本次调优失败，不允许跨样本排名。

除最低 CVaR 的专用比较器外，其余目标使用稳定排序键：目标分数、CAGR、绝对回撤和 canonical 权重向量。`research_optimizer_v6` 使用闭式候选计数和惰性生成，避免先完整物化权重网格；这只改变资源使用方式，不改变候选集合和排序口径。

调优 run 本身不持久化生命周期状态；API 以关联 `worker_tasks` 为权威状态，并通过统一 finalize 与取消协议发布结果。任务架构见 [031-unified-worker-task-architecture.md](./031-unified-worker-task-architecture.md) 和 [035-worker-task-cancellation.md](./035-worker-task-cancellation.md)。

## 7. 应用调优结果

结果页每条记录都可通过单一接口应用：

```http
POST /api/v1/research/optimizations/{optimizationId}/apply
```

请求携带 `objective`、`rank` 和预览时的 `expected_collection_updated_at`。服务端在同一事务内：

1. 校验调优任务已经成功且排名记录存在；
2. 校验集合和条目身份仍与冻结快照一致；
3. 将正权重条目启用、写入权重并锁定；
4. 将零权重或未选中的条目禁用、解锁并清零；
5. 同步该次调优冻结的回测窗口；
6. 对包含尾部风险快照的当前版本同步 CVaR 置信度与持有期；
7. 只更新一次集合 `updated_at` 后提交。

集合并发变化返回 `409 research_collection_changed`；结果身份过期返回 `409 research_optimization_result_stale`。任一步失败都整体回滚。历史 `research_optimizer_v2` 结果没有 CVaR 快照，应用时不得覆盖集合当前 CVaR 口径。

应用后，普通回测按正权重资产重新建立自身有效日历。零权重资产被禁用后，依赖有效收益样本的波动率或 VaR/CVaR 不保证与不可变调优结果逐位相同；调优排名应以该次冻结结果为准。

## 8. 数据模型与 API

`research_optimization_runs` 位于 `migrations/0001_init.sql`，保存：

- 集合、task、引擎版本和哈希；
- 基准币种、再平衡策略和冻结窗口；
- `config_json`、`input_snapshot_json`；
- 候选数、已评估数、`result_json`；
- 创建与完成时间。

状态、错误码、进度、执行尝试和取消事实来自关联的统一 worker task，而不是在 optimization 表中重复维护。

API：

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
  "top_k": 20,
  "tail_risk": {
    "confidence": 0.95,
    "horizon_days": 20
  },
  "minimum_cagr": 0.03
}
```

## 9. 当前产品边界

组合研究主流程不提供资产筛选器、候选池、候选比较或 saved filters：

- 不存在 `/research/screener` 页面；
- 集合列表和详情页没有筛选器入口；
- 前后端没有 saved filters API、service 或 repository 读写链路；
- 集合详情页仍可通过本地资产搜索添加资产，`GET /api/v1/research/assets` 保留。

`migrations/0001_init.sql` 仍含未被运行时代码使用的 `research_saved_filters` 旧表。它是 schema 遗留结构，不代表上述功能仍可用；后续若删除，应新增顺序 migration 兼容既有数据库。

## 10. 验证不变量

自动化测试必须持续覆盖：

- 候选计数与惰性生成数量一致，锁定权重不变且每个候选合计为 100%；
- 剩余权重小于步长、仅一个可调资产、锁定恰好 100% 等边界可运行；
- 四类目标及其 tie-break 稳定，minimum CAGR 只过滤最低 CVaR 榜单；
- 所有候选共享有效收益日、CVaR 场景、交易成本与基准口径；
- 缺历史、FX、窗口或 CVaR 样本时 readiness 阻断；
- 活动/完成任务复用、失败 finalize、取消和恢复行为一致；
- 应用结果原子同步权重、状态、窗口与适用的 CVaR 口径，并正确处理并发冲突；
- 已移除的筛选器运行时链路不再暴露，集合内资产搜索保持可用。

仓库级验证使用：

```bash
make ci
```
