# Fireman 已实现功能总览

- 基线设计：`td/001-fireman-complete-design.md`
- 梳理日期：2026-06-11
- 状态：**功能主体已实现**；`td/002` 人工浏览器验收部分步骤仍待签字

本文按实施阶段与后续 td 改造项归纳**当前代码已具备的能力**，便于 onboarding 与发布前核对。

---

## 1. 工程与部署（td/001 Stage 1；td/003～td/004 修复）

| 能力 | 说明 |
| --- | --- |
| Go 模块化单体 | `cmd/fireman` + `internal/*`，Gin HTTP API |
| SQLite | `modernc.org/sqlite`，版本化 migration（0001～0005） |
| 三镜像 Docker Compose | `fireman` / `fireman-web` / `fireman-market-provider` |
| Web API 代理 | 构建时 `API_PROXY_TARGET=http://backend:8080`（td/003 P0-2） |
| Makefile & CI | `make ci`：Go test/lint、Vitest、Next build、sidecar pytest、集成测试 |
| 三镜像构建 | `make build-images` 独立保留 `MARKET_PROVIDER_IMAGE`（td/004 P1-1） |

---

## 2. 计划、权重与调仓（td/001 Stage 2）

| 能力 | 说明 |
| --- | --- |
| 计划 CRUD | 创建、列表、删除、版本 `config_version` 乐观锁 |
| 三层权重 | 大类 → 地区组内 → 标的组内；容差 0.0001 |
| 内置场景 | 积累期 / 接近 FIRE / 已 FIRE / 保守（不可删） |
| 目标配置 | 只读展开标的目标权重与金额 |
| 调仓检查 | 偏离百分点、建议动作（未启用 / 不动 / 增配 / 减配） |
| 持仓快照 | 创建快照时可回写 `current_amount_minor`（td/003 返工） |
| 只读字段校验 | 持仓/标的 metadata 客户端不可写（`holding_fields_read_only` 等） |

### 2.1 新建计划向导（td/003 P0-1）

- 原子接口 `POST /api/v1/plans/wizard`：一次提交 FIRE 参数、场景、持仓、未分配现金处理
- 前端四步向导：前三步本地草稿，最后一步单次提交并启动默认 10000 次模拟
- 失败时不残留半成品计划或快照

---

## 3. 资产资料库与市场数据（td/001 Stage 3；td/005～td/007）

| 能力 | 说明 |
| --- | --- |
| AKShare sidecar | `POST /v1/instruments/fetch`、`POST /v1/instruments/resolve` |
| 支持市场 | CN（场内 ETF/LOF、A 股、公募基金）、HK、US |
| 数据清洗 | 日收益异常检测、年度收益、CAGR/波动/回撤指标 |
| 模拟快照 | 纳入日最近最多 20 个完整自然年度；`source_hash` 审计 |
| 系统标的 | 系统现金、USDCNY/HKDCNY 汇率（migration 0003） |
| 数据过期提示 | 距最近交易日 >7 自然日返回 stale 警告 |
| Refresh | 24h 节流；源切换或 force 时全量替换 |

### 3.1 中国场内代码规范（td/005～td/009）

- 持久化 `code` / `provider_symbol` 为带前缀完整形式（`sh510300`、`sz159915`、`bj…`）
- 东方财富接口用裸六位码，腾讯/新浪 fallback 用带前缀码
- 北交所 market-id 映射修正（td/007）
- LOF market-id 映射纳入 resolve 总 deadline（td/008～td/009）
- 显式前缀 LOF 独立解析路径（td/010）
- 子进程硬超时 + 进程内 spot 表 TTL 缓存（td/005～td/007）

### 3.2 公募基金名称缓存（近期增强，未单独 td）

- 名称表 **永久进程内缓存** + 磁盘快照（Docker volume `/cache`）
- 启动后台预热；`POST /v1/metadata/refresh` 手动刷新
- `fund_name_em` 专用 60s 超时，不受 5s resolve deadline 限制

详见 [asset-import.md](./asset-import.md)。

---

## 4. 资产异步录入（td/006；td/007～td/011）

| 能力 | 说明 |
| --- | --- |
| 轻量 resolve | 只查 spot/名称，不拉全量历史 |
| 编码去歧义 | 多候选时前端选择；`candidate_id` 稳定标识（td/011） |
| Resolution ticket | 15 分钟 TTL，一次性消费（migration 0005） |
| 异步抓取 | `POST /import-async` → 占位 `pending_fetch` → Worker 单次全量 fetch |
| 任务幂等 | 同一 `(market, type, code, adjust)` 仅一条 queued/running job |
| 计划门禁 | 非 `active` 或质量不足标的不可加入 FIRE 计划 |
| 抓取状态 | `GET /fetch-status`；失败可 `POST /retry-fetch` |
| 取消恢复 | 运行中取消 → `fetch_failed` + 可重试（td/008） |
| 类型不匹配提示 | 场外公募基金误选场内类型 → `instrument_type_mismatch` + 前端自动切换 |

### 4.1 用户指定资产类别（近期增强）

- 确认页必选 **equity / bond / cash**，不再依赖 AKShare 自动分类
- 抓取完成后以用户选择为准写入 `asset_class`
- resolve 阶段名称（如「长城短债A」）在 fetch 返回裸码时 **不会被覆盖**

详见 [asset-import.md](./asset-import.md)。

---

## 5. Monte Carlo 模拟（td/001 Stage 4；td/003～td/004）

| 能力 | 说明 |
| --- | --- |
| Student-t 独立因子 | 按标的完整年度估计参数 |
| Seed 复现 | 根 seed + 路径号派生；非负 int64 契约（td/004 P1-1） |
| 结果同事务 | 汇总、路径索引、分位序列同一 SQLite 事务提交（td/004 P1-2） |
| 路径详情 | 按 seed 重算单条路径；列表与详情 seed 一致 |
| 交易成本 | 现金支出不计费；卖出补现金与调仓双边计费（td/003 P1-1） |
| Job + Worker | 异步 `simulation` job，进度与 SSE/轮询 |
| 参数过期 | 计划参数变更后旧模拟 run 标记 stale |

---

## 6. 前端页面（td/001 Stage 5）

| 路由 | 功能 |
| --- | --- |
| `/plans/new` | 四步 FIRE 向导 |
| `/plans/{id}/dashboard` | 仪表盘、进入模拟分析 |
| `/plans/{id}/parameters` | FIRE 与组合参数 |
| `/plans/{id}/scenarios` | 场景复制与编辑 |
| `/plans/{id}/instruments` | 标的配置与组内权重 |
| `/plans/{id}/targets` | 目标配置（只读） |
| `/plans/{id}/rebalance` | 调仓检查 |
| `/plans/{id}/analysis` | 模拟 / 压力 / 敏感性分析中心 |
| `/assets` | 全局资产资料库 |
| `/assets/import` | AKShare 解析 → 选类 → 异步抓取 |
| `/assets/{id}` | 详情、年度收益、抓取进度 |
| `/settings` | 备份与恢复 |

策略枚举前后端一致（td/003 P1-2）；分析页按任务类型分别重试（td/004 P1-2）。

---

## 7. 压力与敏感性（td/001 Stage 6）

| 能力 | 说明 |
| --- | --- |
| 压力测试 | 历史情景冲击；恢复期 P50 等指标（td/004） |
| 敏感性测试 | Tornado、参数曲线、热力图 |
| 异步 Job | 与模拟共用 Worker 框架 |

---

## 8. 系统与运维（td/001 Stage 7）

| 能力 | 说明 |
| --- | --- |
| 备份 / 恢复 | `GET /system/backup`、`POST /system/restore`（维护模式） |
| 计划导出 | JSON、targets CSV、rebalance CSV |
| Worker 优雅关闭 | 等待进行中 job 或超时（td/004～td/005） |
| Ticket 清理 | 过期 resolution ticket 后台清理（td/008） |
| 集成测试 | `internal/api/stage7_integration_test.go` 等覆盖主链路 |

---

## 9. td 评审闭环状态

| 文档 | 主题 | 评审结论 |
| --- | --- | --- |
| td/003 | 首轮实施评审 | 主体已修复；后续 td 继续收敛 |
| td/004 | seed、模拟事务、镜像构建等 | P1 已关 |
| td/005 | Makefile、分析重试、HK 规范化 | P1 已关 |
| td/006 | 异步 resolve/导入设计 | 已实施 |
| td/007 | ticket、migration、硬超时 | P1 已关 |
| td/008 | 取消、系统现金、同毫秒迁移 | P1 已关 |
| td/009 | ROW_NUMBER 去重、LOF deadline | P1 已关 |
| td/010 | 显式前缀 LOF | P1 已关 |
| td/011 | candidate_id 唯一标识 | **全部关闭** |

---

## 10. 已知限制

1. **人工浏览器验收**（`td/002`）：10 步主链路表格仍为「待验」，需产品负责人在发布前补签字。
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
