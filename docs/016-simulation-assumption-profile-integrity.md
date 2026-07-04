# 模拟假设 Profile：版本、审计与回归保护

## 目的

Fireman 的前瞻收益、波动率、相关性、厚尾与情景参数由全局 versioned profile 管理。计划只引用 profile、版本与情景；模拟 run 会冻结最终使用的内容和来源，以保证结果可以回放和审计。

当前计划模拟基准币种固定为 `CNY`。创建和更新计划时，非 CNY 基准币种会被拒绝。

## 当前系统 profile

| 状态 | Profile 内容 | 用途 |
| --- | --- | --- |
| 当前默认 | `system_cma_v3@1` | 新计划的默认前瞻假设；可设为全局默认 |
| 历史回放 | `system_cma_v1@1` | 只用于历史 pin/replay，不可重新设为全局默认 |
| 历史回放 | `system_cma_v2@1`（TD 064 内容） | 只用于历史 pin/replay，不可重新设为全局默认 |
| 历史回放 | `system_cma_v2@1`（TD 065 内容变体） | 按 content hash 识别，保留其独立 evidence hash |

系统 identity 是 append-only 的。改变收益公式、证据、参数或相关性时必须发布新的 system profile identity；已发布 profile 的 canonical JSON 不更新、不删除。

## CMA 收益计算

v3 以 committed evidence artifact 生成收益先验。对实际几何收益、通胀和按期末资产比例收取的费用，使用：

```text
nominal_after_fee = (1 + real_geometric_return)
                  * (1 + expected_inflation)
                  * (1 - annual_fee_rate) - 1
```

FX 长期漂移使用相对购买力平价：

```text
fx_change_to_cny = (1 + cny_inflation) / (1 + quote_inflation) - 1
```

只有在写入 profile canonical JSON 时才按四位小数舍入。`internal/assumptions/cma_evidence_v3.json` 保存每个先验的来源、日期、输入和公式约定；artifact SHA-256 与 v3 canonical SHA-256 都被 registry 固定。

## 系统 namespace 与升级行为

`system_cma_` 是保留 id 前缀。用户创建的 profile 由服务端分配 `user_<uuid>` id，不能使用系统前缀。

启动以及所有需要解析 profile 的读取路径都会执行完整性检查：

1. 当前 `system_cma_v3@1` 必须是 system-owned，且存储的 canonical bytes/hash 与 registry 完全一致。
2. 任何旧的 user `system_cma_*` profile 会复制为 `user_legacy_<旧 canonical hash 前 16 位>`；计划 pin 与全局默认在同一事务中重定向。冻结的历史 run snapshot 不修改。
3. 所有 system `system_cma_*` rows 都必须由 `(id, version, content_hash)` 历史内容 registry 识别，且 canonical bytes 的 SHA-256 必须等于存储 hash。未知或篡改内容返回 `system_profile_identity_conflict`，不会被静默覆盖。
4. 只有首次发布 v3 时才将全局默认从直接前驱 v2 迁移到 v3；后续检查不会覆盖用户主动选择的默认 profile。

## Run provenance

每个新 run 的 `input_snapshot_json` 会冻结：

- `assumption_profile_id`
- `assumption_profile_version`
- `assumption_profile_content_hash`
- `assumption_evidence_hash`（仅限已识别的 system content）

用户 profile 不会获得系统 CMA evidence hash。未知 system content 不能创建 run。

## 固定回归基线

API 端到端测试固定了 50 年 horizon、seed `424242`、1,000 条路径和固定持仓/现金流 fixture：

- v3 terminal P50：`577,080,841` minor
- v1/v2 historical control terminal P50：`527,399,522` minor

测试同时校验两次运行的 input hash/P50 相同，以及持久化 snapshot 的四项 provenance。若收益模型、采样引擎或 profile 内容发生有意改变，必须发布新 system identity 并显式更新经审核的回归基线。

## 相关接口

- `GET /api/v1/simulation-assumptions/profiles`：profile 列表、全局默认与可选资格。
- `POST /api/v1/simulation-assumptions/profiles`：创建 user draft；服务端分配 id。
- `POST /api/v1/simulation-assumptions/profiles/{id}/{version}/activate`：激活 user draft version。
- `PUT /api/v1/simulation-assumptions/preferences`：设置合格的全局默认。

历史 system profile 仍可被显式 pin 以复现旧模型，但不会出现在可重新选择的全局默认候选中。
