# 资产录入（Resolve → 确认 → 异步抓取）

- 设计来源：`td/006-asset-import-async-resolve.md` 及 td/007～td/011 评审修复
- 更新：2026-06-11（用户指定资产类别、名称保留、公募基金名称缓存）

## 1. 用户流程

```text
1. 选择市场 + 标的类型 + 输入代码
2. POST /api/v1/instruments/resolve  →  名称 / 交易所 / 候选列表
3. （若歧义）选择正确候选
4. 确认页选择资产类别：股票/权益 | 债券 | 现金/货币
5. POST /api/v1/instruments/import-async  →  占位记录 + 后台 job
6. 跳转 /assets/{id} 查看抓取进度；完成后 status=active
```

### 前端路由

`/assets/import` — 四阶段 UI：`search` → `disambiguate`（可选）→ `confirm` → 跳转详情

### 类型不匹配

场外公募基金（如 `007194` 长城短债 A）若误选「场内 ETF/LOF」：

- API 返回 `instrument_type_mismatch`，建议 `cn_mutual_fund`
- 前端自动切换为「公募基金」并提示重新查询

## 2. API 契约

### Resolve

```http
POST /api/v1/instruments/resolve
Content-Type: application/json

{
  "market": "CN",
  "instrument_type": "cn_mutual_fund",
  "code": "007194"
}
```

响应（无歧义）含 `resolved.ticket_id`（15 分钟有效）、`candidate_id`、`name` 等。

### Import Async

```http
POST /api/v1/instruments/import-async
Content-Type: application/json

{
  "ticket_id": "tkt_...",
  "asset_class": "bond"
}
```

`asset_class` 必填，取值：`equity` | `bond` | `cash`。

### 抓取状态与重试

- `GET /api/v1/instruments/{id}/fetch-status`
- `POST /api/v1/instruments/{id}/retry-fetch`（仅 `fetch_failed`）

## 3. 后端行为

| 环节 | 行为 |
| --- | --- |
| Ticket | resolve 时创建；import-async 时 consume；防重放 |
| 占位记录 | `status=pending_fetch`，`asset_class` 为用户所选 |
| Worker | 单次全量 fetch；`UserAssetClass` 覆盖 provider 自动分类 |
| 名称 | job payload 保存 `resolved_name`；fetch 若只返回代码则保留 resolve 名称 |
| 幂等 | partial unique index on `(type, input_hash)` where queued/running |
| 计划选用 | `EnsureInstrumentReadyForPlan` 要求 `active` + 可用质量 |

## 4. Market Provider

### Resolve

- 中国场内：并行加载 ETF/LOF/股票 spot（5s 总 deadline）
- 公募基金：独立名称 lookup（1 天 TTL 磁盘/内存缓存 + singleflight，60s 上游超时）
- 类型检测：裸码不在场内表但在公募表 → `instrument_type_mismatch`

### 公募基金名称缓存

| 项 | 说明 |
| --- | --- |
| TTL | `MARKET_PROVIDER_MUTUAL_FUND_CACHE_TTL`（默认 86400s）；过期忽略磁盘并同步刷新 |
| 路径 | `MARKET_PROVIDER_MUTUAL_FUND_CACHE_PATH`（Compose 为 `/cache/mutual_fund_names.json`） |
| 加载顺序 | 内存（fresh）→ 磁盘（fresh）→ AKShare `fund_name_em`（singleflight） |
| 手动刷新 | `POST /v1/metadata/refresh` body `{"target":"cn_mutual_fund_names"}` |
| 启动预热 | sidecar startup 后台线程加载 |

完整说明见 [003-mutual-fund-cache-wizard-holdings.md](./003-mutual-fund-cache-wizard-holdings.md)。

### Fetch

Worker 调用 `POST /v1/instruments/fetch`；中国场内代码在 adapter 内转换为各 AKShare 接口所需格式。

## 5. 典型示例：007194

| 步骤 | 操作 |
| --- | --- |
| 类型 | 公募基金 `cn_mutual_fund` |
| Resolve | 名称「长城短债A」 |
| 资产类别 | **债券**（用户手动选择） |
| 抓取后 | 名称保持「长城短债A」，`asset_class=bond` |

## 6. 相关测试

- Go：`internal/api/instrument_async_*_test.go`
- Python：`sidecars/market-provider/tests/test_resolve.py`、`test_names.py`
- Web：`web/app/assets/import/page.test.tsx`
