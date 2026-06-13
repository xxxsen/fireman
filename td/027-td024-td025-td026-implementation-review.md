# Fireman `td/024` / `td/025` / `td/026` 实施复审报告

- 复审日期：2026-06-14
- 复审对象：当前工作区对 `td/024-plan-settings-rebalance-preview-refine.md`、`td/025-td024-implementation-review.md`、`td/026-asset-000001-cagr-review.md` 的实施结果
- 复审范围：场景模板完整化、持仓预览与资产变更收拢、`cn_mutual_fund` 数据源兼容修复
- 约束：本次只 review，不修改业务代码

## 1. 结论

本轮已经补上了 `td/025` 中指出的两项主缺口：

1. 场景模板已扩展为同时包含大类与地区目标；
2. `资产变更` 已支持显式编辑 `weight_within_group`，并去掉了原先的静默平均重分配逻辑。

同时，`td/026` 的主要方向也已落地：

1. `cn_mutual_fund` 抓取链路开始先识别 `source_kind`；
2. 后端增加了 `market_data_source_type_conflict` 校验；
3. 资产资料库与资产详情页的相关 UI 与测试已同步更新。

但当前实现仍有 5 个有效问题，尚不能视为 `td/024 + td/025 + td/026` 已完整闭环。

| 级别 | 数量 | 结论 |
| --- | ---: | --- |
| P0 | 0 | 未发现直接破坏数据库的一步到位数据破坏问题 |
| P1 | 2 | `cn_mutual_fund` 的混合基金路径仍不可用；资产变更提交流程存在半提交风险 |
| P2 | 3 | 资产录入页仍保留多余启用开关；`使用分项合计` 未与 `CNY` 视觉对齐；`待投入 ¥0.00` 仍然展示 |

## 2. 已完成项确认

本次确认已完成：

- `allocation_scenarios` 已支持 `region_targets` 持久化与回填；
- `web/app/scenarios/page.tsx` 已允许编辑场景的大类与地区目标；
- `web/components/plans/AllocationSettings.tsx` 已按完整模板预览大类与地区目标；
- `web/app/plans/[id]/asset-refresh/page.tsx` 已支持编辑组内配比；
- `web/lib/asset-refresh.ts` 已按真实差异统计“影响资产数量”；
- `web/app/assets/page.tsx` 与 `web/app/assets/[id]/page.tsx` 已完成资料库删除态、抓取状态模态框和自动刷新相关 UI；
- 后端与 sidecar 已新增 `source_kind` / `market_data_source_type_conflict` 相关测试。

## 3. P1 问题

### P1-1 `td/026` 要修复的 `000001` 路径仍未真正打通，混合基金会被直接判定为 unsupported

性质：bug。

定位：

- [classification.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/classification.py:25)
- [classification.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/classification.py:61)
- [classification.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/classification.py:84)
- [registry.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/registry.py:236)
- [registry.py](/home/sen/work/fireman/sidecars/market-provider/fireman_market_provider/adapters/registry.py:319)
- [test_cn_mutual_fund_source.py](/home/sen/work/fireman/sidecars/market-provider/tests/test_cn_mutual_fund_source.py:8)

问题：

这轮实现已经把 `华夏成长混合(000001)` 这样的资产识别为 `open_fund` 路径，不再错误回退到 `money_fund`。但 `classify_cn_mutual_fund()` 仍然把 `"混合"` 放在 `UNSUPPORTED_KEYWORDS` 里，导致任何名称或基金类型包含“混合”的基金，在拿到开放式基金净值数据后仍会被判定为 `asset_class=None`，随后在 `_fetch_cn_mutual_fund()` 中走到 `unsupported fund classification`。

也就是说，当前实现只是把错误的 `money_fund fallback` 切掉了，但并没有把 `000001` 这条真实业务路径修通。结果会从“导入/刷新成功但数据错”变成“导入/刷新直接失败”，依然不满足 `td/026` 的目标。

这不是边界情况。`td/026` 明确就是围绕 `华夏成长混合(000001)` 的异常展开，而当前代码与测试同时说明：

1. `000001` 会被识别为 `open_fund`；
2. `混合` 仍被视为不支持；
3. 因此正确源上的混合基金依然不可用。

用户的实际验证也已和这一结论一致：当前抓取 `000001` 必现报错

`fetch instrument history: market provider error: ak.fund_open_fund_info_em:累计净值走势: unsupported fund classification; ak.fund_open_fund_info_em:单位净值走势: unsupported fund classification: market provider rejected`

这说明问题不是理论风险，而是当前运行环境中的必现故障。

修复方案：

将 `cn_mutual_fund` 的分类规则从“按关键词整体排斥混合基金”改为“先识别基金子类型，再映射到支持的大类”：

1. 去掉 `UNSUPPORTED_KEYWORDS` 中对普通 `混合` 的整体屏蔽；
2. 对 `混合基金`、`股票基金`、`指数基金`、`ETF联接`、`QDII` 统一按 `equity` 处理；
3. 保留对 `FOF`、`REIT`、`商品`、`黄金`、`期货`、`另类` 这类当前系统明确不支持品类的拦截；
4. 让 `000001` 这类历史已存在的混合基金可以在 `open_fund` 路径上完成刷新与重建指标，而不是被卡在分类层。

验收逻辑：

1. 对 `000001 / 华夏成长混合`，`detect_cn_mutual_fund_source_kind()` 仍返回 `open_fund`。
2. 当 `fund_open_fund_info_em` 返回正常开放式基金净值数据时，`classify_cn_mutual_fund()` 不再返回 `asset_class=None`。
3. `000001` 刷新后不会再落到 `ak.fund_money_fund_info_em`，也不会再报 `unsupported fund classification`。
4. 对 `混合基金` 样本，资产详情页的年度收益、模拟窗口与 CAGR 能基于正确的开放式基金净值源重建。
5. 对 `FOF`、`REIT`、`商品` 等明确不支持品类，仍继续返回 unsupported。

### P1-2 `资产变更` 的单次提交不是原子操作，失败时会留下“场景已切换 / 结构已改，但页面提示提交失败”的半提交状态

性质：bug。

定位：

- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:275)
- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:285)
- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:295)
- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:304)

问题：

`提交资产变更` 现在会按顺序发起最多 3 次写操作：

1. `applyScenario()`
2. `updateHoldings()`
3. `submitAssetRefresh()`

这 3 步都在前端串行执行，彼此之间没有事务。只要第 1 步或第 2 步已经成功，而后续步骤失败，用户就会看到“提交失败”，但当前计划其实已经发生了部分变更：

1. FIRE 方案可能已经切换；
2. 持仓结构可能已经落库；
3. 但金额刷新与总资产同步没有完成。

这和页面模型是冲突的。`资产变更` 对用户表现为一次统一提交，因此必须具备“要么全部成功，要么全部不生效”的原子性。当前实现会留下半提交状态，用户很难判断系统到底改了哪些内容。

修复方案：

把 `资产变更` 向导的最终提交收敛为**一次后端原子事务**：

1. 扩展当前 `POST /plans/{id}/asset-refresh`，让它一次接收：
   - 目标 `scenario_id`
   - 完整持仓结构更新
   - 当前金额更新
   - 总资产同步信息
2. 后端在同一个事务里依次完成：
   - 场景切换
   - 持仓结构更新
   - 当前金额刷新
   - 总资产同步
   - 审计事件写入
3. 前端将当前的三段串行 mutation 合并为一次请求；
4. 任一步失败时整笔事务回滚，保证用户不会看到“提交失败但部分变更已落库”的状态。

验收逻辑：

1. 触发一个“场景切换成功，但后续金额校验失败”的用例后，计划的场景、持仓结构和总资产都保持提交前状态。
2. 触发一个“持仓结构更新成功，但事件写入失败”的用例后，结构与总资产都不会残留部分更新。
3. 前端最终只发起一次“提交资产变更”写请求，而不是前端串行调用多个写接口。
4. 用户看到“提交失败”时，重新刷新页面，计划状态与提交前一致。

### P2-1 `录入当前资产` 仍保留 `启用` 列，与本轮交互收敛方向冲突

性质：实现缺失。

定位：

- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:510)
- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:526)

问题：

当前 `录入当前资产` 表格仍然保留了单独的 `启用` 列和 checkbox。按本轮收敛后的交互模型，用户不需要某个资产时应直接执行 `移除`，而不是再维护一套“保留但禁用”的并行心智。

这会带来两个问题：

1. 页面上同时存在 `启用/停用` 与 `移除` 两套动作，用户难以判断何时该用哪一个；
2. `资产变更` 已经承担“维护当前真实持仓结构”的职责后，保留 disabled 持仓只会让状态空间继续膨胀。

修复方案：

移除 `录入当前资产` 表中的 `启用` 列与 checkbox，统一改为“保留即编辑，删除即移除”的单一路径：

1. 表格删除 `启用` 列；
2. 非系统资产仅保留 `移除` 操作；
3. 系统资产若必须常驻，则继续显示不可移除态，但不再暴露启用切换；
4. 提交 payload 只基于当前仍保留在草稿列表中的资产生成，不再依赖前端显式维护 `enabled=false`。

验收逻辑：

1. `录入当前资产` 页面不再出现 `启用` 列。
2. 用户移除一个资产后，该资产不会再出现在待提交列表中。
3. 用户重新添加同一资产时，按“新增资产”路径重新进入草稿。
4. 页面中不再同时出现“停用”和“移除”两套并行操作。

### P2-2 `使用分项合计` 仍未与 `CNY` / 金额输入区形成稳定对齐

性质：UI 缺陷。

定位：

- [MoneyInput.tsx](/home/sen/work/fireman/web/components/ui/MoneyInput.tsx:49)
- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/asset-refresh/page.tsx:581)

问题：

本轮虽然已经把金额单位提示移到输入框左侧，但 `资产总值` 区域里的 `使用分项合计` 仍然是独立按钮，和 `MoneyInput` 的 `CNY(...) + 输入框` 组合没有形成统一基线。用户当前观感仍然是“按钮飘在右边”，没有达到你要求的“与 `CNY` 垂直居中对齐”。

修复方案：

把 `资产总值` 区域改成一个单行对齐容器，以输入组件基线为主，`使用分项合计` 作为同一行内的次级动作对齐到金额输入行中心：

1. `MoneyInput` 外层输出稳定高度；
2. `使用分项合计` 放入同一行的 controls 区，而不是自由换行块；
3. 该行在桌面和窄屏下都保持垂直居中；
4. 不再依赖 `flex-wrap` 自然换行决定视觉位置。

验收逻辑：

1. `资产总值` 行中，`CNY(...)`、输入框、`使用分项合计` 在桌面宽度下垂直居中对齐。
2. 在 125% / 150% 浏览器缩放下，`使用分项合计` 不会下沉或偏离输入行中心。
3. 在移动端换行时，布局仍然保持明确的“金额输入主体 + 次级动作”关系，不出现视觉漂移。

### P2-3 `持仓预览` 仍然展示 `待投入 ¥0.00`，没有收敛零值噪音

性质：UI 缺陷。

定位：

- [page.tsx](/home/sen/work/fireman/web/app/plans/[id]/rebalance/page.tsx:66)

问题：

当前 `gapAmountCell()` 对持仓行无条件展示：

- `待投入 ¥0.00`
或
- `待减配 ¥0.00`

这与页面收敛目标不一致。零值差异不提供任何决策信息，只会让表格看起来更嘈杂，尤其是在大部分持仓已经对齐时，零值文案会挤占真正需要关注的偏差行。

修复方案：

对 `gap_amount_minor == 0` 的行统一隐藏零值动作文案：

1. 持仓行差异为 0 时显示 `—` 或空白；
2. 非 0 时才显示 `待投入` / `待减配`；
3. 大类与地区层级继续保持当前“默认不直出该列”的策略，不额外增加零值提示。

验收逻辑：

1. `持仓预览` 中不再出现 `待投入 ¥0.00` 或 `待减配 ¥0.00`。
2. 只有真实存在正负偏差的持仓行，才显示动作金额。
3. 零值行在视觉上明显降噪，用户能更快识别真正需要处理的资产。

## 4. 验证记录

已通过：

- `go test ./internal/api ./internal/service ./internal/jobs ./internal/marketdata ./internal/repository ./internal/db`
- `cd web && npm test -- --run`
- `cd web && npm run build`
- `cd web && npm run lint`

补充说明：

- sidecar 相关实现已结合新增测试文件与代码路径完成 review；
- 本文档为本地 review 文档，不迁移到 `docs/`。
