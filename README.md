# Fireman

Fireman 是一个**单用户、本地优先**的 FIRE 资产配置与风险模拟系统。
它解决四个连续问题：当前持仓画像、目标配置、调仓建议、以及在退休时点、
支出、通胀、收益不确定时的 FIRE 成功率与风险。

只有完整实现并通过验收的功能文档才会进入 [`docs/`](docs/) 归档。

当前已归档：[功能总览](docs/002-implemented-features.md)、[市场数据任务化架构](docs/021-market-data-task-worker-architecture.md)。

## 当前实现状态

本仓库已实现以下能力（仍有未关闭评审项，**暂不可作为正式发布版本**）：

- Go 模块化单体：`cmd/fireman` + `internal/*`，Gin API，SQLite（`modernc.org/sqlite`）
- 计划生命周期：创建向导（原子 `POST /api/v1/plans/wizard`）、参数、三层权重、持仓、场景
- 资产资料库：AKShare sidecar、市场数据清洗、模拟历史快照与 `source_hash` 审计
- Monte Carlo 引擎：Student-t 独立因子、按 seed 复现、路径详情与代表路径
- 压力测试与敏感性分析（Tornado、参数曲线、热力图）
- Job 队列 + Worker 心跳、压力/敏感性/模拟异步任务
- Next.js 16 前端：计划向导、仪表盘、参数、分析中心、路径详情、资产详情
- 三镜像 Docker Compose（web 构建时注入 `API_PROXY_TARGET=http://backend:8080`）
- `make ci`：Go 测试/ lint、Vitest、Next.js 构建、market-provider pytest、集成测试

## 技术栈

| 层 | 选型 |
| --- | --- |
| 后端 | Go 1.25、Gin、`database/sql` |
| 数据库 | SQLite 单文件 |
| 前端 | Next.js 16、React 19、TypeScript、Tailwind CSS 4 |
| 图表 | Apache ECharts |
| 市场数据 sidecar | Python 3.12、FastAPI、AKShare、uv |
| 容器 | Docker + Docker Compose |

## 目录结构

```
fireman/
├── docs/                        # 已验收功能文档
├── cmd/fireman/                 # Go 入口
├── internal/                    # 后端模块（api、simulation、stress、jobs…）
├── migrations/                  # SQLite migration
├── web/                         # Next.js 前端
├── sidecars/market-provider/    # AKShare sidecar
├── docker/docker-compose.yml    # 三服务编排
├── Makefile
└── README.md
```

## 本地开发

```bash
make web-install
make market-provider-install
make dev    # backend :8080 / web :3000 / market-provider :18081
```

## 容器编排

```bash
make build-images
make docker-up    # web 健康检查同时验证页面与 /api/v1/plans
make docker-down
```

Docker 运行时 smoke（构建镜像、启动 compose、验证 web→backend API 代理）：

```bash
chmod +x scripts/docker-smoke.sh
./scripts/docker-smoke.sh
```

脚本会等待 backend 与 web 就绪，请求 `http://127.0.0.1:3000/api/v1/plans` 验证 Next.js 将 API 代理至 `backend:8080`（构建时通过 `API_PROXY_TARGET` 注入）。

## CI

```bash
make ci
```

在无外网环境下顺序执行：Go build/test/lint、前端 lint/Vitest/build、sidecar pytest、`-tags=integration` 集成测试。

## 不在本期范围内

- 不引入 Dev Container（仓库无 `.devcontainer/`）
- 不引入 E2E（无 Playwright/Cypress）
- 完整主链路通过集成测试 + Vitest + 人工浏览器验收
