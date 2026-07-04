# 组合总览与计划设置 UI 改造

## 1. 总览 Tooltip 跟随鼠标
- `lib/tooltip-position.ts` 新增 `computeCursorTooltipPosition`：默认显示在鼠标右下方偏移 12px，越界时翻转到左侧 / 上方并夹紧到视口内。
- `components/ui/Tooltip.tsx` 新增 `followCursor` 属性：`onMouseEnter` / `onMouseMove` 记录最近 `clientX/clientY`，指针打开时按鼠标定位；`onFocus`（键盘）将 `openedByPointerRef` 置否，回退到 trigger rect 定位。
- `components/ui/MetricHelp.tsx` 启用 `followCursor`。

## 2. 地区配置图表重构
- 后端 `DashboardRegionBar` 增加 `current_amount_minor` / `target_amount_minor` / `holdings`；新增 `DashboardAssetClassRegionGroup`（`asset_class_region_groups`）。
  - 全组合地区暴露：`buildRegionBars` 按 `Region` 聚合全组合权重与金额，并带持仓明细。
  - 大类内地区配置：`buildAssetClassRegionGroups` 目标用 `RegionTarget.weight_within_class`，当前用该大类内当前金额占比；地区排序国内→国外，大类排序权益→债券→现金/其他。
- 前端：`RegionAllocationBarChart` tooltip 展示金额与具体基金名称 / 编号 / 当前 / 目标金额；新增 `AssetClassRegionGroups` 紧凑组件展示大类内国内/国外目标、当前与资产明细。
- 总览页 `国内 / 国外配置` 改名 `地区配置`（标注“按全组合权重折算”），并新增 `大类内地区配置` 区块。

## 3. 场景模板与计划地区配比数据模型收敛
- 后端 `ApplyScenario` 只复制场景 `weights`，保留计划自身 `RegionTargets`。
- 场景配置页（`app/scenarios/page.tsx`）移除地区组内权重编辑，仅编辑大类目标权重（保存时仍提交默认 region targets 以兼容现有接口）。
- 计划设置“当前计划目标配置”（`PlanTargetsContent`）：场景模板只读预览大类权重；新增“本计划国内/国外配比”可编辑区，读取/编辑 `allocation.region_targets`。
  - 切换场景仅预览新模板大类权重，地区配比保持计划现值。
  - 保存：场景变更调用 `applyScenario`（用返回的 `config_version`）；地区变更调用 `updateAllocation`。

## 4. 计划设置目标配置 UI
- 移除独立的“前往场景配置”按钮，改为内联文案 `要修改场景结构，请前往 场景配置`，`场景配置` 为可点击链接跳转 `/scenarios`。

## 5. FIRE 参数：资金与现金流 UI 收敛
- `资金与现金流` 改为紧凑网格（宽屏 3 列 / 平板 2 列 / 移动 1 列）：第一行 计划基准规模、当前年储蓄、退休后首年支出；第二行 年储蓄增长率、期末最低资产目标。
- `计划基准规模` 的 `MetricHelp` 移到 label 行内（`MoneyInput.label` 支持 `ReactNode`）。
- 规模缺口/超出收敛为一条 compact alert，保留“计入现金/其他”勾选与超出阻断保存逻辑。

## 6. 移除额外现金流事件
- 删除 `CashFlowEditor` 组件与 `额外现金流事件` section；移除 `PlanCashFlow` 类型、参数 API 的 `cash_flows` 出入参、`form-snapshot` 的 cash flows 字段、`normalize` 的 `cash_flows`。
- 后端同步删除 cash flow model / repository / service / 模拟输入 / 配置哈希引用，并从 `0001_init.sql` 移除 `plan_cash_flows` 表。

## 7. 新建计划向导有效宽度
- `app/plans/new/page.tsx` 根容器 `max-w-5xl` → `max-w-[96rem]`；表单步骤（计划基础/目标配置）保持 `max-w-2xl`，建立持仓/确认组合步骤 `w-full`。
