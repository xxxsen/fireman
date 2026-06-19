/** User-facing glossary for financial and simulation terminology. */

export const TERMS: Record<string, string> = {
  fire_success_rate:
    "所有模拟路径中，整个规划期资金未耗尽且满足期末最低资产目标的比例",
  p_quantiles:
    "终值分布的分位数。P00 是本次模拟最小值，不代表理论最坏情况",
  max_drawdown:
    "仅在参与模拟的完整自然年度连续分段内，资产从此前最高点到随后最低点的最大跌幅",
  annual_return:
    "最近最多 20 个完整自然年度收益的几何年化，不使用不完整年度",
  annual_volatility:
    "参与模拟的完整自然年度内月度对数收益的样本标准差，按 √12 年化",
  rebalance_threshold:
    "当前权重与目标权重相差多少个百分点时触发调仓",
  weight_within_group:
    "标的在同一「大类 + 地区」内部的占比，不是全组合占比",
  simulation_snapshot_sync:
    "按今日市场数据重新生成该标的的模拟用历史快照（年化收益、波动等），供 FIRE 模拟使用；不会修改当前持仓金额",
  portfolio_weight:
    "大类、地区、标的三层权重相乘后的最终占比",
  fee_included:
    "历史净值或市场价格已经反映持有费率，模拟不再重复扣除",
  stress_test:
    "将指定冲击强制施加到路径，不等同于冲击发生概率",
  sensitivity_test:
    "单独改变一个参数，观察成功率变化，不代表参数预测",
  unallocated_gap:
    "计划基准规模高于持仓市值的差额（规模缺口）。可能是市场缩水或未录全持仓；持仓已如实录入时可在调仓页同步下调基准，否则请补录持仓或计入现金",
  holdings_sum:
    "当前计划中已启用标的的持仓金额合计，应与券商账户大致一致（不含未录入资产）",
  holdings_market_value:
    "已录入持仓的当前市值合计，反映真实账户规模",
  invested_minor:
    "当前计划中已启用标的的持仓金额合计，用于衡量已投资金占计划基准规模的比例",
  configured_total_assets:
    "创建或更新计划时填写的基准规模；用于 FIRE 模拟与录入校验，不等于实时市值。市值变化后可在调仓页一键同步",
  scale_gap_over:
    "持仓市值高于计划基准规模；不代表需要全体减仓，仅表示账面基准与市值不一致，可同步上调基准或忽略",
  scale_gap_under:
    "计划基准规模高于持仓市值；可能是市场缩水或未录全。持仓已如实录入时建议同步下调基准；否则补录持仓或将差额计入现金",
  structural_gap_row:
    "结构还差 = 按当前持仓合计计算的权重偏离；调仓建议仅依据此列",
  plan_scale_gap_row:
    "按计划基准规模计算的金额差，仅作高级对齐参考，不驱动主建议",
  structural_rebalance:
    "在不变动总市值的前提下，调整各大类/标的占比",
  actionable_rebalance:
    "结构权重与目标权重之差超过「调仓阈值」的标的数量；可在计划设置中调整阈值",
  asset_class_allocation:
    "权益、债券、现金三大类在全组合中的目标占比与当前占比对比",
  region_allocation:
    "各大类下国内与国外资产的目标占比与当前占比对比；标的地区在录入资产时手动选择",
  deviation_amount:
    "结构目标金额减去当前持仓金额；正数表示按当前市值还需买入，负数表示结构超配",
  rebalance_mode_full:
    "按全组合目标重新对齐，可能同时包含买入和卖出建议",
  rebalance_mode_new_cash:
    "仅用本次新增资金买入欠配标的，不主动卖出超配部分",
  asset_refresh:
    "更新账户真实资产金额；可选同步计划基准；不等同于新建计划",
  annual_savings_wizard:
    "指 FIRE 之前，你每年预计还能新增积攒多少资金。",
  asset_refresh_vs_rebalance_plan:
    "资产变更=刷新事实基准；调仓计划=按冻结建议调整持仓，编辑时不重算其他行目标",
  rebalance_plan_draft:
    "进行中的调仓计划，分阶段保存，完成后才写入正式持仓",
  rebalance_fund_pool:
    "减配释放的资金可供增配使用；未在标的间分配的部分可在提交时转入现金持仓",
  rebalance_reference_package:
    "完整结构对齐参考方案，含未达调仓阈值的微调；与行内「不动」建议不同，仅作参考",
  apply_recommended_one_line:
    "仅将本行计划金额更新为推荐值，不影响其他标的；可配合分阶段暂存",
  unallocated_sweep_to_cash:
    "提交时若资金池仍有未分配金额，将计入计划中的现金持仓",
  frozen_structural_gap:
    "调仓计划创建时确定的结构还差，编辑过程中不变",
  current_amount_vs_target:
    "当前金额为您的真实持仓；目标金额由场景与权重自动计算。交易后请在此更新当前金额",
  fire_simulation_optional:
    "基于当前配置与历史数据估算长期退休路径成功率；不运行模拟也可正常管理持仓与调仓",
  fire_params_for_simulation:
    "年龄、支出、储蓄等仅用于 FIRE 模拟与提取规则，不改变调仓目标金额的计算方式",
  portfolio_gap_row:
    "汇总表中「结构还差」= 该维度下所有标的的结构还差金额之和；大类/地区行的目标与当前金额见下级标的，悬停「—」可查看合计",
  target_weight_portfolio:
    "占全组合的目标占比：该维度目标金额 ÷ 组合总资产",
  target_weight_within_asset_class:
    "占同一大类内的目标配比：国内与国外在该大类中的划分比例，合计 100%",
  current_weight_portfolio:
    "占当前持仓合计的当前占比：该维度当前金额 ÷ 已启用持仓市值合计",
  current_weight_within_asset_class:
    "占同一大类内的结构配比：根据实际持仓折算，国内与国外在该大类中合计 100%",
  result_stale:
    "当前配置已变化，历史模拟结果可能不再反映最新输入",
  simulation_runs: "Monte Carlo 路径数量，默认 10000，范围 1000 至 100000",
  student_t_df: "Student-t 分布自由度，控制尾部厚度，默认 7",
  metric_cagr:
    "CAGR：所选完整自然年度收益的几何年化，(Π(1+r))^(1/n)-1。仅基于当前纳入模拟的完整年度与清洗后行情",
  metric_annual_volatility:
    "年化波动率：所选完整年度内月度对数收益的样本标准差乘 √12。仅基于当前纳入模拟的完整年度与清洗后行情",
  metric_max_drawdown:
    "最大回撤：所选完整年度窗口中，从历史峰值到其后低点的最大跌幅，以正数幅度展示。仅基于当前纳入模拟的完整年度与清洗后行情",
};

export function helpForTerm(key?: string): string | undefined {
  if (!key) return undefined;
  return TERMS[key];
}
