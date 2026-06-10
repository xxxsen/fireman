# 公募基金名称缓存、删除刷新与计划向导选标

- 设计来源：`td/013-mutual-fund-cache-delete-refresh-plan-wizard.md`
- 实施复审：`td/014-td013-implementation-review.md`（本地，未入 Git）
- 更新：2026-06-11

---

## 1. 公募基金名称缓存（1 天 TTL + singleflight）

### 目标行为

| 时机 | 行为 |
| --- | --- |
| 服务启动 | 读磁盘；`refreshed_at` 超过 TTL 则忽略磁盘、拉上游并写回 |
| 用户请求 | 内存缓存过期则**同步**刷新（singleflight 合并并发） |
| 刷新失败 | 有未过期旧数据继续服务；无任何有效缓存则向上抛错 |

### 配置

| 环境变量 | 默认 | 说明 |
| --- | --- | --- |
| `MARKET_PROVIDER_MUTUAL_FUND_CACHE_TTL` | `86400`（秒） | 1 天 TTL |
| `MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH` | `/tmp/fireman/mutual_fund_names.json` | Compose 通常为 `/cache/mutual_fund_names.json` |

### 实现要点

- 核心：`sidecars/market-provider/fireman_market_provider/adapters/names.py`
- 磁盘 JSON：`{ "version": 1, "refreshed_at": "<ISO8601>", "names": { "007194": "长城短债A", ... } }`
- 并发刷新：`threading.Lock` + `Event` singleflight， follower 等待 leader 完成后共享结果
- 状态查询：`cn_mutual_fund_name_cache_status()` 返回 `ttl_seconds`、`expires_at`、`is_fresh`
- Resolve 路径：`get_cn_mutual_fund_name()` 传播上游错误；`lookup_cn_mutual_fund_name()` 为 best-effort 吞异常
- 手动刷新：`POST /v1/metadata/refresh` body `{"target":"cn_mutual_fund_names"}` → `refresh_cn_mutual_fund_names()`

### 单测

`sidecars/market-provider/tests/test_names.py`：TTL、过期磁盘、singleflight、强制刷新等。

---

## 2. 删除资产后资料库列表刷新

### 行为

删除成功后：

1. `removeQueries(["instrument-detail", id])` 清除详情缓存
2. `await invalidateQueries({ queryKey: ["instruments"] })` 失效列表
3. `router.push("/assets")` 跳转资料库

### 实现

- `web/app/assets/[id]/page.tsx` — `deleteMut.onSuccess`
- 单测：`web/app/assets/[id]/page.test.tsx`

---

## 3. 新建计划向导 step 2：按大类分组选标

### 范围

**仅** `web/app/plans/new/page.tsx` 及抽取组件；计划内 `/plans/[id]/instruments` 编辑页未改。

### UI 行为

1. 按 **权益 → 债券 → 现金/其他** 顺序渲染容器；场景大类权重为 0 的容器不显示
2. 每容器内搜索仅匹配对应 `asset_class` 的 active 标的
3. **预期资金** = `round(总资产 × 场景大类权重 × 组内占比)`
4. **组内权重**按大类（非大类+地区）合计须 100% 方可进入 step 3
5. **全组合目标权重**在 step 3 由 `buildWizardPortfolioReview` 校验，通过后方可创建计划
6. **场景切换**：离开 step 1 进入 step 2 时，`pruneSelectedByScenario` 移除权重为 0 大类下的已选标的

### 关键文件

| 文件 | 职责 |
| --- | --- |
| `web/components/plans/AssetClassHoldingPicker.tsx` | 单大类搜索、选标、权重/金额输入 |
| `web/lib/wizard-allocation.ts` | `computeExpectedAmountMinor`、`pruneSelectedByScenario`、`buildWizardPortfolioReview` |
| `web/app/plans/new/page.tsx` | 向导编排、`activeScenarioClasses`、`groupWeightChecks` |

### 单测

- `web/app/plans/new/page.test.tsx` — 三容器、搜索过滤、预期金额、创建流程
- `web/lib/wizard-allocation.test.ts` — 嵌套金额公式、组合 review 缺大类提示

---

## 4. 不在本次范围

- 计划内标的编辑页 UI
- holdings API / 数据库 schema
- ETF/LOF/股票 spot 名称缓存 TTL（仍为短 TTL 进程内缓存）
