# Fireman 已实现功能总览

- 梳理日期：2026-06-11
- 状态：**功能主体已实现**；人工浏览器验收记录仍有待补签字项

本文按实施阶段与后续功能收敛归纳**当前代码已具备的能力**，便于 onboarding 与发布前核对。

---

## 1. 工程与部署

| 能力 | 说明 |
| --- | --- |
| Go 模块化单体 | `cmd/fireman` + `internal/*`，Gin HTTP API |
| SQLite | `modernc.org/sqlite`，版本化 migration（0001～0019） |
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

## 3. 资产资料库与市场数据

| 能力 | 说明 |
| --- | --- |
| AKShare sidecar | `POST /v1/instruments/fetch`、`POST /v1/instruments/resolve` |
| 支持市场 | CN（场内 ETF/LOF、A 股、公募基金）、HK、US |
| TickFlow 优先行情源 | 可选（默认关闭）：已解析 A 股与场内 `etf/index_etf` 在未复权口径下优先 `tickflow.klines:1d`，未命中自动回退 AKShare；LOF/公募基金/resolve 不接入；官方 SDK 接入，支持 free/paid（API key）两种模式 |
| 数据清洗 | 日收益异常检测、年度收益、CAGR/波动/回撤指标 |
| 模拟快照 | 纳入日最近最多 20 个完整自然年度；`source_hash` 审计 |
| 系统标的 | 系统现金、USDCNY/HKDCNY 汇率（migration 0003） |
| 数据过期提示 | 距最近交易日 >7 自然日返回 stale 警告 |
| Refresh | 手工刷新立即执行；源切换时全量替换 |

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
- 启动后台预热；`POST /v1/metadata/refresh` 手动强制刷新
- `fund_name_em` 专用 60s 超时，不受 5s resolve deadline 限制

详见 [003-mutual-fund-cache-wizard-holdings.md](./003-mutual-fund-cache-wizard-holdings.md)、[001-asset-import.md](./001-asset-import.md) 与 [017-market-data-quality-and-return-metrics.md](./017-market-data-quality-and-return-metrics.md)。

---

## 4. 资产异步录入

| 能力 | 说明 |
| --- | --- |
| 轻量 resolve | 只查 spot/名称，不拉全量历史 |
| 编码去歧义 | 多候选时前端选择；`candidate_id` 稳定标识 |
| Resolution ticket | 15 分钟 TTL，一次性消费（migration 0005） |
| 异步抓取 | `POST /import-async` → 占位 `pending_fetch` → Worker 单次全量 fetch |
| 任务幂等 | 同一 `(market, type, code, adjust)` 仅一条 queued/running job |
| 计划门禁 | 非 `active` 或质量不足标的不可加入 FIRE 计划 |
| 抓取状态 | `GET /fetch-status`；失败可 `POST /retry-fetch` |
| 取消恢复 | 运行中取消 → `fetch_failed` + 可重试 |
| 类型不匹配提示 | 场外公募基金误选场内类型 → `instrument_type_mismatch` + 前端自动切换 |

### 4.1 用户指定资产类别（近期增强）

- 确认页必选 **equity / bond / cash**，不再依赖 AKShare 自动分类
- 抓取完成后以用户选择为准写入 `asset_class`
- resolve 阶段名称（如「长城短债A」）在 fetch 返回裸码时 **不会被覆盖**

详见 [asset-import.md](./asset-import.md)。

---

## 5. Monte Carlo 模拟

| 能力 | 说明 |
| --- | --- |
| 多元 Student-t 因子 | 按冻结月度因子、相关矩阵和 profile 厚尾参数抽样 |
| Seed 复现 | 根 seed + 路径号派生；非负 int64 契约 |
| 结果同事务 | 汇总、路径索引、分位序列同一 SQLite 事务提交 |
| 路径详情 | 按 seed 重算单条路径；列表与详情 seed 一致 |
| 交易成本 | 现金支出不计费；卖出补现金与调仓双边计费 |
| Job + Worker | 异步 `simulation` job，进度与 SSE/轮询 |
| 参数过期 | 计划参数变更后旧模拟 run 标记 stale |
| 版本化模拟假设 | CNY 基准的 CMA v3 profile、历史 profile 回放、canonical/evidence hash provenance、固定 seed P50 回归 |
| 前瞻收益校准 | 历史收益向长期先验收缩，支持资产级 override、FX 因子和真实购买力序列 |

详见 [016-simulation-assumption-profile-integrity.md](./016-simulation-assumption-profile-integrity.md) 与 [019-fire-simulation-forward-engine-and-plan-controls.md](./019-fire-simulation-forward-engine-and-plan-controls.md)。

---

## 6. 前端页面

| 路由 | 功能 |
| --- | --- |
| `/plans/new` | 四步计划向导；模拟可选且不阻塞进入计划 |
| `/plans/{id}/overview` | 组合总览、大类/地区配置、偏离摘要、折叠式可选模拟 |
| `/plans/{id}/rebalance` | 调仓工作台；查看当前持仓、目标结构、结构偏差，并进入持仓校正、调仓计划或调仓执行 |
| `/plans/{id}/rebalance/executions` | 多日调仓执行列表 |
| `/plans/{id}/rebalance/executions/{executionId}` | 调仓执行工作区：登记卖出、买入、备注、完成或取消 |
| `/plans/{id}/settings` | 切换当前计划使用的配置模板、编辑计划参数、运行模拟 |
| `/assets` | 全局资产资料库 |
| `/assets/import` | AKShare 解析 → 选类 → 异步抓取 |
| `/assets/{id}` | 详情、年度收益、抓取进度 |
| `/scenarios` | 全局配置模板管理 |
| `/settings` | 备份与恢复 |

策略枚举前后端一致；分析页按任务类型分别重试。
旧计划内页面 URL 保留兼容重定向。详见
[004-portfolio-first-ui.md](./004-portfolio-first-ui.md) 与
[008-plan-settings-holdings-preview.md](./008-plan-settings-holdings-preview.md)。

调仓计划与执行详见 [018-rebalance-planning-and-execution.md](./018-rebalance-planning-and-execution.md)。
Web 信息架构、术语与可访问性规范详见 [020-web-ui-information-architecture-and-accessibility.md](./020-web-ui-information-architecture-and-accessibility.md)。

---

## 7. 压力与敏感性

| 能力 | 说明 |
| --- | --- |
| 压力测试 | 历史情景冲击；恢复期 P50 等指标 |
| 敏感性测试 | Tornado、参数曲线、热力图 |
| 异步 Job | 与模拟共用 Worker 框架 |

---

## 8. 系统与运维

| 能力 | 说明 |
| --- | --- |
| 备份 / 恢复 | `GET /system/backup`、`POST /system/restore`（维护模式） |
| 计划导出 | JSON、targets CSV、rebalance CSV |
| Worker 优雅关闭 | 等待进行中 job 或超时 |
| Ticket 清理 | 过期 resolution ticket 后台清理 |
| 集成测试 | `internal/api/stage7_integration_test.go` 等覆盖主链路 |

---

## 9. 实施收敛状态

以下主题已经完成主要实现并纳入当前代码：

- 原子计划向导与配置持久化
- 异步资产导入、解析票据与抓取状态管理
- 中国场内代码规范化、LOF 解析与硬超时
- 公募基金名称缓存与资料库删除刷新
- 组合优先 UI、结构偏差与规模偏差分拆
- 调仓工作台 / 持仓校正 / 全局配置模板收拢

---

## 10. 已知限制

1. **人工浏览器验收**：主链路验收记录仍有待签字项，需产品负责人在发布前补齐。
2. **无 E2E**：依赖 Go 集成测试 + Vitest + 人工浏览器。
3. **单用户本地优先**：无账号、权限、多租户。
4. **数据源**：除系统 FX/现金外，用户资产仅经 AKShare sidecar 录入。

---

## 11. 验证命令

```bash
make ci                                    # 全量 CI
go test -tags=integration ./internal/api/...  # 集成测试
cd sidecars/market-provider && uv run pytest  # sidecar
cd web && npm test -- --run                  # 前端单测
```
