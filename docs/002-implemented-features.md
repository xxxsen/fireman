# Fireman 已实现功能总览

- 状态：**功能主体已实现**

本文按实施阶段与后续功能收敛归纳**当前代码已具备的能力**，便于 onboarding 与发布前核对。

---

## 1. 工程与部署

| 能力 | 说明 |
| --- | --- |
| Go 模块化单体 | `cmd/fireman` + `internal/*`，Gin HTTP API |
| SQLite | `modernc.org/sqlite`，单一 DDL 基线 migration `0001_init.sql` |
| 三镜像 Docker Compose | `fireman` / `fireman-web` / `fireman-market-provider` |
| Web API 代理 | 构建时 `API_PROXY_TARGET=http://backend:8080` |
| Makefile & CI | `make ci`：Go test/lint、Vitest、Next build、sidecar pytest、集成测试 |
| 三镜像构建 | `make build-images` 独立保留 `MARKET_PROVIDER_IMAGE` |

---

## 2. 计划、权重与调仓

| 能力 | 说明 |
| --- | --- |
| 计划 CRUD | 创建、列表、删除、版本 `config_version` 乐观锁 |
| 三层权重 | 大类 → 地区组内 → 标的组内；容差 0.0001 |
| 内置场景 | 积累期 / 接近 FIRE / 已 FIRE / 保守（不可删） |
| 目标配置 | 只读展开标的目标权重与金额 |
| 调仓检查 | 偏离百分点、建议动作（未启用 / 不动 / 增配 / 减配） |
| 持仓快照 | 创建快照时可回写 `current_amount_minor` |
| 只读字段校验 | 持仓/标的 metadata 客户端不可写（`holding_fields_read_only` 等） |

### 2.1 新建计划向导

- 原子接口 `POST /api/v1/plans/wizard`：一次提交 FIRE 参数、场景、持仓、未分配现金处理
- 前端四步向导：前三步本地草稿，最后一步单次提交；模拟默认关闭，可选后台运行 10000 次
- 失败时不残留半成品计划或快照
- **step 2 按大类分组**：权益/债券/现金容器、大类内搜索、预期资金公式、大类组内权重 100%、场景切换 prune
- **step 1/2 支持地区目标**：权益/债券国内外配比、按 `region` 分区选标、`region_targets` 写入计划

详见 [003-mutual-fund-cache-wizard-holdings.md](./003-mutual-fund-cache-wizard-holdings.md)、[006-wizard-region-domestic-foreign-allocation.md](./006-wizard-region-domestic-foreign-allocation.md) 与 [019-fire-simulation-forward-engine-and-plan-controls.md](./019-fire-simulation-forward-engine-and-plan-controls.md)。

---

## 3. 市场资产目录与市场数据

| 能力 | 说明 |
| --- | --- |
| 市场数据 worker sidecar | 纯任务 worker（对外仅 `GET /healthz`）；Go 建任务、sidecar 执行、Go post-process 落库，见 [021-market-data-task-worker-architecture.md](./021-market-data-task-worker-architecture.md) |
| 支持市场 | CN（场内 ETF/LOF、A 股、公募基金）、HK、US |
| TickFlow 优先行情源 | 可选（默认关闭）：已解析 A 股与场内 `etf/index_etf` 在未复权口径下优先 `tickflow.klines:1d`，未命中自动回退 AKShare；LOF/公募基金/resolve 不接入；官方 SDK 接入，支持 free/paid（API key）两种模式 |
| 数据清洗 | 日收益异常检测、年度收益、CAGR/波动/回撤指标 |
| 模拟快照 | 纳入日最近最多 20 个完整自然年度；`source_hash` 审计 |
| 系统标的 | 系统现金、USDCNY/HKDCNY 汇率（应用启动时由 Go bootstrap 幂等初始化） |
| 数据过期提示 | 距最近交易日 >7 自然日返回 stale 警告 |
| Refresh | 手工刷新立即执行；源切换时全量替换 |
| 自动更新管理 | 可配置的定时扫描器，为目录单元和资产历史维度创建刷新任务；复用既有 worker/sidecar 链路，乐观锁并发控制和 active-dedupe 保证幂等，见 [029-market-data-auto-update-scheduler.md](./029-market-data-auto-update-scheduler.md) |
| 任务取消与执行中断 | active task 可由业务页面或管理后台立即取消；Go worker 协作停止，Sidecar 终止独立进程组；取消不保存部分结果、不重试且立即释放同类任务门禁，见 [035-worker-task-cancellation.md](./035-worker-task-cancellation.md) |

TickFlow 配置与 fallback 规则详见 `sidecars/market-provider/README.md`。

### 3.1 中国场内代码规范

- 持久化 `code` / `provider_symbol` 为带前缀完整形式（`sh510300`、`sz159915`、`bj…`）
- 东方财富接口用裸六位码，腾讯/新浪 fallback 用带前缀码
- 北交所 market-id 映射修正
- LOF market-id 映射纳入 resolve 总 deadline
- 显式前缀 LOF 独立解析路径
- 子进程硬超时 + 进程内 spot 表 TTL 缓存

### 3.2 公募基金名称缓存

- **1 天 TTL**（`MARKET_PROVIDER_MUTUAL_FUND_CACHE_TTL`，默认 86400s）+ 磁盘快照（Docker volume `/cache`）
- 过期同步刷新 + **singleflight** 去重；失败时未过期旧数据继续服务
- 启动后台预热（可用 `MARKET_PROVIDER_STARTUP_WARM_ENABLED=false` 关闭）
- `fund_name_em` 专用 60s 超时

详见 [003-mutual-fund-cache-wizard-holdings.md](./003-mutual-fund-cache-wizard-holdings.md) 与 [017-market-data-quality-and-return-metrics.md](./017-market-data-quality-and-return-metrics.md)。

---

## 4. 计划持仓直接引用市场资产

| 能力 | 说明 |
| --- | --- |
| 全局资产目录 | `market_assets` 由后台任务同步；无“用户录入/资产库”中间层 |
| 持仓引用 | `plan_holdings.asset_key` 直接引用目录；系统现金为内置资产（`SYS\|cash\|\|CNY` 等） |
| 用户指定分类 | 持仓的 `asset_class`（equity/bond/cash）与 `region` 由用户选择，不从资产类型硬编码推断 |
| 懒快照 | 缺历史的资产可先保存到计划；模拟前由 readiness 检查拦截（`market_asset_history_missing`，准入以快照试算为准并按原因细分，含资产身份冲突提示） |
| 一键补历史 | `POST /plans/{id}/sync-missing-asset-history` 仅为真正缺历史的资产创建/复用同步任务；已同步但不可模拟的资产返回 `blocked` |

详见 [021-market-data-task-worker-architecture.md](./021-market-data-task-worker-architecture.md)。

---

## 5. Monte Carlo 模拟

| 能力 | 说明 |
| --- | --- |
| 多元 Student-t 因子 | 按冻结月度因子、相关矩阵和 profile 厚尾参数抽样 |
| Seed 复现 | 根 seed + 路径号派生；非负 int64 契约 |
| 结果同事务 | 汇总、路径索引、分位序列同一 SQLite 事务提交 |
| 路径详情 | 按 seed 重算单条路径；列表与详情 seed 一致 |
| 交易成本与现金池 | 3.2.0 将全部同币种现金聚合为流动性池；现金支出不计费，仅非现金卖出补提款与调仓双边计费；费率范围 `[0,1)` |
| 退休后稳定收入 | 3.3.0 在 FIRE 月起将税后养老金、净租金或长期副业收入计入现金池；支持年增长率，进入快照、账本和 config hash |
| 失败状态 | 路径记录资金不足、资产耗尽、期末目标未达等可由账本证明的状态，不推断伪因果标签 |
| Job + Worker | 异步 `simulation` job，进度与 SSE/轮询 |
| 参数过期 | 计划参数变更后旧模拟 run 标记 stale |
| 版本化模拟假设 | CNY 基准的 CMA v4 profile、历史 profile 回放、canonical/evidence hash provenance、固定 seed P50 回归 |
| 前瞻收益校准 | 历史收益向长期先验收缩，支持资产级 override、FX 因子和真实购买力序列 |
| FIRE 达标前沿 | 基于指定正式模拟的冻结输入回答四类单变量边界问题；每个新的运行请求独立计算，只有同一 `Idempotency-Key` 且输入一致的请求重试才返回原任务，见 [036-fire-confidence-frontier.md](./036-fire-confidence-frontier.md) |

详见 [016-simulation-assumption-profile-integrity.md](./016-simulation-assumption-profile-integrity.md)、[019-fire-simulation-forward-engine-and-plan-controls.md](./019-fire-simulation-forward-engine-and-plan-controls.md)、[026-portfolio-research-and-simulation-logic-corrections.md](./026-portfolio-research-and-simulation-logic-corrections.md) 与 [032-simulation-assumption-lifecycle-and-effective-inputs.md](./032-simulation-assumption-lifecycle-and-effective-inputs.md)。

---

## 6. 前端页面

| 路由 | 功能 |
| --- | --- |
| `/plans/new` | 四步计划向导；模拟可选且不阻塞进入计划 |
| `/quick-fire` | 无需计划、持仓或行情的确定性 FIRE 快算；可将现金流参数一次性带入新建计划 |
| `/plans/{id}/overview` | 组合总览、大类/地区配置、偏离摘要、折叠式可选模拟 |
| `/plans/{id}/rebalance` | 调仓工作台；查看当前持仓、目标结构、结构偏差，并进入持仓校正或调仓执行 |
| `/plans/{id}/rebalance/executions` | 多日调仓执行列表 |
| `/plans/{id}/rebalance/executions/{executionId}` | 调仓执行工作区：登记卖出、买入、备注、完成或取消 |
| `/plans/{id}/settings` | 切换当前计划使用的配置模板、编辑计划参数、运行模拟 |
| `/plans/{id}/frontier` | 运行四类 FIRE 达标前沿，查看冻结计算依据、曲线点证据与历史运行 |
| `/assets` | 全市场资产目录（同步状态面板、筛选、分页）|
| `/assets/market/{assetKey}` | 市场资产详情、历史同步、年度收益 |
| `/research` | 组合研究首页：研究集合、最近回测运行、JSON 导入导出 |
| `/admin/auto-updates` | 自动更新管理：目录单元全量清单、资产历史规则列表、启停与周期编辑 |
| `/research/screener` | 资产筛选器、候选池与候选比较 |
| `/research/collections/{id}` | 研究集合编辑、readiness、批量数据更新、回测入口 |
| `/research/collections/{id}/runs/{runId}` | 确定性历史回测结果（图表/年度表/热力图/贡献/相关性/数据质量） |
| `/scenarios` | 全局配置模板管理 |
| `/settings` | 备份与恢复 |

策略枚举前后端一致；分析页按任务类型分别重试。
旧计划内页面 URL 保留兼容重定向。详见
[004-portfolio-first-ui.md](./004-portfolio-first-ui.md) 与
[008-plan-settings-holdings-preview.md](./008-plan-settings-holdings-preview.md)。

调仓执行详见 [018-rebalance-planning-and-execution.md](./018-rebalance-planning-and-execution.md)。
Web 信息架构、术语与可访问性规范详见 [020-web-ui-information-architecture-and-accessibility.md](./020-web-ui-information-architecture-and-accessibility.md)。
FIRE 快算的公式、API 和验收约定详见 [027-quick-fire-calculator.md](./027-quick-fire-calculator.md)。
组合研究模块详见 [024-portfolio-research.md](./024-portfolio-research.md)、[025-research-portfolio-auto-optimization.md](./025-research-portfolio-auto-optimization.md) 与 [026-portfolio-research-and-simulation-logic-corrections.md](./026-portfolio-research-and-simulation-logic-corrections.md)。

---

## 7. 压力与敏感性

| 能力 | 说明 |
| --- | --- |
| 压力测试 | 历史情景冲击；恢复期 P50 等指标 |
| 敏感性测试 | Tornado、参数曲线、热力图 |
| 异步任务 | 与模拟共用统一 Worker Task 框架；按任务类型独立恢复、轮询和重试 |

---

## 8. 系统与运维

| 能力 | 说明 |
| --- | --- |
| 备份 / 恢复 | `GET /system/backup`、`POST /system/restore`（维护模式） |
| 计划导出 | JSON、targets CSV、rebalance CSV |
| 统一 Worker Task | `worker_tasks` 是唯一任务表；Go 提供创建、查询、claim、心跳、取消、结果与恢复控制面，见 [031-unified-worker-task-architecture.md](./031-unified-worker-task-architecture.md) |
| 前端任务恢复 | HTTP 轮询为正确性来源、SSE 加速、刷新按业务 scope 恢复、stable active dedupe 防止重复创建，见 [034-async-task-tracking-and-recovery.md](./034-async-task-tracking-and-recovery.md) |
| Worker 优雅关闭 | 等待进行中 task 或超时 |
| Ticket 清理 | 过期 resolution ticket 后台清理 |
| 集成测试 | `internal/api/stage7_integration_test.go` 等覆盖主链路 |

---

## 9. 实施收敛状态

以下主题已经完成主要实现并纳入当前代码：

- 原子计划向导与配置持久化
- 任务化资产目录同步、历史同步与模拟 readiness 检查
- 中国场内代码规范化、LOF 解析与硬超时
- 公募基金名称缓存与目录同步刷新
- 组合优先 UI、结构偏差与规模偏差分拆
- 调仓工作台 / 持仓校正 / 全局配置模板收拢
- 市场数据自动更新管理与定时扫描器
- 异步任务前端跟踪、刷新恢复与重复创建防护

---

## 10. 已知限制

1. **无 E2E**：依赖 Go 集成测试 + Vitest + 人工浏览器。
2. **单用户本地优先**：无账号、权限、多租户。
3. **数据源**：除系统 FX/现金外，市场资产目录与历史数据均由任务化 worker sidecar（AKShare/TickFlow）同步。

---

## 11. 验证命令

```bash
make ci                                    # 全量 CI
go test -tags=integration ./internal/api/...  # 集成测试
cd sidecars/market-provider && uv run pytest  # sidecar
cd web && npm test -- --run                  # 前端单测
```
