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
    "同一批完整自然年度对数收益的样本标准差",
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
    "总资产与已启用标的当前金额合计的差额。大于 0 时需计入现金/其他或补充持仓",
  holdings_sum:
    "当前计划中已启用标的的持仓金额合计，应与券商账户大致一致（不含未录入资产）",
  actionable_rebalance:
    "当前权重与目标权重之差超过「调仓阈值」的标的数量；可在计划设置中调整阈值",
  asset_class_allocation:
    "权益、债券、现金三大类在全组合中的目标占比与当前占比对比",
  region_allocation:
    "各大类下国内与国外资产的目标占比与当前占比对比；标的地区在录入资产时手动选择",
  deviation_amount:
    "目标持仓金额减去当前持仓金额；正数表示还需买入，负数表示持仓超标",
  rebalance_mode_full:
    "按全组合目标重新对齐，可能同时包含买入和卖出建议",
  rebalance_mode_new_cash:
    "仅用本次新增资金买入欠配标的，不主动卖出超配部分",
  current_amount_vs_target:
    "当前金额为您的真实持仓；目标金额由场景与权重自动计算。交易后请在此更新当前金额",
  fire_simulation_optional:
    "基于当前配置与历史数据估算长期退休路径成功率；不运行模拟也可正常管理持仓与调仓",
  fire_params_for_simulation:
    "年龄、支出、储蓄等仅用于 FIRE 模拟与提取规则，不改变调仓目标金额的计算方式",
  portfolio_gap_row:
    "汇总表中「还差」= 该维度下所有标的的目标金额之和减去当前金额之和；大类/地区行的目标与当前金额见下级标的，悬停「—」可查看合计",
  target_weight_portfolio:
    "占全组合的目标占比：该维度目标金额 ÷ 组合总资产",
  target_weight_within_asset_class:
    "占同一大类内的目标配比：国内与国外在该大类中的划分比例，合计 100%",
  current_weight_portfolio:
    "占全组合的当前占比：该维度当前金额 ÷ 组合总资产",
  current_weight_within_asset_class:
    "占同一大类内的当前配比：根据实际持仓折算，国内与国外在该大类中合计 100%",
  result_stale:
    "当前配置已变化，历史模拟结果可能不再反映最新输入",
  simulation_runs: "Monte Carlo 路径数量，默认 10000，范围 1000 至 100000",
  student_t_df: "Student-t 分布自由度，控制尾部厚度，默认 7",
};

export function helpForTerm(key?: string): string | undefined {
  if (!key) return undefined;
  return TERMS[key];
}
