# 新建计划向导：权益/债券国内外配比与分区选标

- 设计来源：`td/015-wizard-region-domestic-foreign-allocation.md`
- 更新：2026-06-13

## 1. Step 1：地区组内权重

选择场景后，若权益或债券的大类权重大于 0，则向导展示对应的地区组内权重输入：

- 权益：国内 / 国外
- 债券：国内 / 国外

默认值均为国内 `100%`、国外 `0%`。离开 step 1 前，每个大类都要求国内 + 国外 = `100%`。现金不展示地区配比编辑，固定为国内 `100%`。

## 2. Step 2：按地区拆分选标容器

当某个大类的国外配比大于 0 时，step 2 会将该大类拆成国内、国外两个子容器：

- 国内子容器只搜索 `region=domestic`
- 国外子容器只搜索 `region=foreign`
- 国外配比为 `0%` 时，只保留国内单容器

已选标的采用更紧凑的行式布局；预期资金公式改为：

```text
总资产 × 大类权重 × 地区组内权重 × 标的组内占比
```

若用户把某大类国外配比改回 `0%`，离开 step 1 进入 step 2 时，会自动移除该大类下已选的国外标的。

## 3. Step 3 与提交

step 3 的全组合目标权重校验已使用用户选择的 `region_targets`，不再写死为“国内 100%”。提交 `POST /api/v1/plans/wizard` 时会携带 6 条 `region_targets`：

- `equity` × `domestic/foreign`
- `bond` × `domestic/foreign`
- `cash` × `domestic/foreign`

后端将这些地区目标直接写入 `plan_region_targets`，创建完成后，计划设置页看到的地区组内权重与向导输入一致。

## 4. 关键实现

- 前端：`web/app/plans/new/page.tsx`
- 选标组件：`web/components/plans/AssetClassHoldingPicker.tsx`
- 向导权重逻辑：`web/lib/wizard-allocation.ts`
- 后端写入：`internal/service/plan_wizard.go`

## 5. 验证覆盖

- `web/lib/wizard-allocation.test.ts`
- `web/app/plans/new/page.test.tsx`
- `internal/api/plan_wizard_integration_test.go`
