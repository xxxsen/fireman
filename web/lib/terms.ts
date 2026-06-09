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
  result_stale:
    "当前配置已变化，历史模拟结果可能不再反映最新输入",
  simulation_runs: "Monte Carlo 路径数量，默认 10000，范围 1000 至 100000",
  student_t_df: "Student-t 分布自由度，控制尾部厚度，默认 7",
};

export function helpForTerm(key?: string): string | undefined {
  if (!key) return undefined;
  return TERMS[key];
}
