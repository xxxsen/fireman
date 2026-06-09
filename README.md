# Fireman

Fireman 是一个**单用户、本地优先**的 FIRE 资产配置与风险模拟系统。
它解决四个连续问题：当前持仓画像、目标配置、调仓建议、以及在退休时点、
支出、通胀、收益不确定时的 FIRE 成功率与风险。

只有完整实现并通过验收的功能文档才会进入 [`docs/`](docs/) 归档。

本仓库当前实现 **Stage 1：工程骨架**。
核心业务逻辑（计划、场景、调仓、AKShare 行情、Monte Carlo、压力 / 敏感性）
将在后续阶段补齐。Stage 1 提供：

- 可编译的 Go 模块化单体（`cmd/fireman` + `internal/*`）。
- SQLite 完整初始 migration，包括业务表、内置 scenario、系统现金标的与快照。
- `GET /healthz`，启动时即执行 `SELECT 1` 验证数据库可用。
- Next.js 16 App Router + React 19 + TypeScript + Tailwind 4 + Vitest 前端骨架。
- Python 3.12 + FastAPI + AKShare + uv 的 sidecar 骨架和请求/响应 schema。
- 三镜像 Docker / Docker Compose。
- 文档要求的 Makefile target 全集。
- 无外网、无浏览器的 CI（GitHub Actions）。

## 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.25、Gin、`database/sql` |
| 数据库 | SQLite 单文件，`modernc.org/sqlite`（CGO 关闭） |
| 日志 | `log/slog`，JSON 输出 |
| 前端 | Next.js 16 App Router、React 19、TypeScript、Tailwind CSS 4 |
| 前端表单 / 查询 / 图表 | React Hook Form + Zod、TanStack Query、Apache ECharts |
| 前端测试 | Vitest + Testing Library |
| 市场数据 sidecar | Python 3.12、FastAPI、AKShare、uv |
| 容器 | Docker + Docker Compose |

锁文件 `go.sum`、`web/package-lock.json`、`sidecars/market-provider/uv.lock`
均会随仓库提交，CI 在无外网环境下使用它们重建依赖。

## 目录结构

```
fireman/
├── docs/                        # 已完整实现并通过验收的功能文档
├── cmd/fireman/                 # Go 入口
├── internal/
│   ├── api/                     # Gin 路由 / 中间件（Stage 1: /healthz）
│   ├── app/                     # 依赖组装与生命周期
│   ├── config/                  # JSON 配置文件加载
│   ├── db/                      # SQLite 连接、PRAGMA、migration
│   ├── domain/                  # 领域模型（Stage 2 起）
│   ├── repository/              # database/sql 仓储（Stage 2 起）
│   ├── service/                 # 业务编排（Stage 2 起）
│   ├── jobs/                    # SQLite job queue + worker（Stage 4 起）
│   ├── marketdata/              # AKShare 客户端 / 快照（Stage 3 起）
│   ├── simulation/              # Monte Carlo 引擎（Stage 4 起）
│   ├── stress/                  # 压力测试（Stage 6 起）
│   └── sensitivity/             # 敏感性测试（Stage 6 起）
├── migrations/                  # embed 的 SQLite migration（0001_init.sql）
├── web/                         # Next.js 前端
├── sidecars/market-provider/    # Python AKShare sidecar
├── docker/
│   ├── docker-compose.yml       # 三服务编排
│   └── config.json              # 容器部署配置
├── config.json                  # 本地开发配置（`fireman run --config=./config.json`）
├── scripts/dev.sh               # `make dev` 使用的本地启动脚本
├── .github/workflows/ci.yml     # CI（make ci）
├── Dockerfile                   # 后端镜像 fireman
├── Makefile
├── go.mod / go.sum
└── README.md
```

## 本地开发

需要的工具：Go 1.25、Node.js 20、npm、Python 3.12（建议通过 `uv` 管理）、
`uv` 0.11+，Docker（运行容器栈时需要）。

```bash
# 一次性安装依赖
make web-install
make market-provider-install

# 三个进程并行启动（市场 sidecar :18081 / 后端 :8080 / 前端 :3000）
make dev
```

`make dev` 使用 `scripts/dev.sh`，**不依赖 Dev Container CLI**。后端通过
`go run ./cmd/fireman run --config=./config.json` 启动；开发 SQLite 默认放在
`.dev-data/fireman.db`（见根目录 `config.json`）。

## 容器编排

```bash
make build-images   # 构建 fireman、fireman-web、fireman-market-provider 三个镜像
make docker-up      # docker compose up -d --build（持久卷 fireman-data:/data）
make docker-down
```

服务名固定：`backend`、`web`、`market-provider`；持久卷固定 `fireman-data:/data`；
后端镜像内置 `docker/config.json`，以 `fireman run --config=/app/config.json` 启动。
默认端口见 `docker/docker-compose.yml`。

## CI

`make ci` 在无外网环境下顺序执行：

1. `backend-check`：`go build`、`go test ./...`、`gofmt -l` + `go vet`。
2. `web-check`：`npm ci`、`next lint`、`vitest --run`、`next build`。
3. `market-provider`：`uv sync --frozen` + `pytest`。
4. `integration-test`：`go test -tags=integration ./...`（Stage 1 暂为空，Stage 7 接入完整后端集成测试）。

CI 同时校验仓库**未引入** `.devcontainer/`、Playwright 或 Cypress 配置。

## 不在本期范围内

当前工程明确排除：

- **不引入** Dev Container：仓库不存在 `.devcontainer/`、`devcontainer.json`，
  也没有 Dev Container Dockerfile 或启动脚本。
- **不引入** E2E：仓库不存在 Playwright、Cypress 依赖与 `playwright.config.*`，
  Makefile 不提供 `e2e`、`e2e-install`、`devcontainer-*` target，
  CI 不启动浏览器。
- 完整业务链路在发布前通过人工浏览器验收清单验证。

## 后续阶段

当前实施进度：

1. ✅ Stage 1：工程骨架、SQLite migration、Makefile、三镜像、CI（本仓库当前状态）。
2. ⏭ Stage 2：计划、场景、三层权重、目标配置、调仓公式。
3. ⏭ Stage 3：资产资料库、AKShare sidecar、本地市场数据、模拟历史快照。
4. ⏭ Stage 4：Monte Carlo 引擎、Job、结果聚合、按 seed 重新生成路径。
5. ⏭ Stage 5：六个主页面、首次向导、Dashboard、模拟分析中心。
6. ⏭ Stage 6：压力测试与敏感性测试。
7. ⏭ Stage 7：备份恢复、导出、性能优化、后端集成测试与人工浏览器验收。
