/** Central registry for user-facing financial, simulation and operations help. */

export interface HelpTopic {
  label: string;
  summary: string;
  purpose?: string;
  calculation?: string;
  inputs?: string;
  interpretation?: string;
  caveat?: string;
}

function topic(
  label: string,
  summary: string,
  details: Omit<HelpTopic, "label" | "summary"> = {},
): HelpTopic {
  return { label, summary, ...details };
}

function calculatedTopic(
  label: string,
  summary: string,
  calculation: string,
  inputs: string,
  interpretation: string,
  details: Pick<HelpTopic, "purpose" | "caveat"> = {},
): HelpTopic {
  return { label, summary, calculation, inputs, interpretation, ...details };
}

export const HELP_TOPICS = {
  fire_success_rate: calculatedTopic(
    "FIRE 成功率",
    "本次模拟中，整个规划期资金未耗尽且满足期末最低资产目标的路径占比。",
    "成功路径数除以本次实际完成的模拟路径总数。",
    "使用当前计划参数、冻结的资产配置与市场快照，以及本次 seed 生成的路径。",
    "越高表示在这些假设和样本下满足目标的路径更多，但不代表真实未来的保证。",
  ),
  p_quantiles: calculatedTopic(
    "终值分位数",
    "把所有模拟路径在规划期末的资产从低到高排列后得到的位置值。",
    "Pn 表示约有 n% 的样本终值不高于该值；P00 是样本最小值。",
    "使用本次已完成模拟路径的期末资产。",
    "较低分位数用于观察不利样本，P50 表示样本中位数；它们都不是单条预测路径。",
    { caveat: "P00 只是有限样本中的最小值，不是理论最坏情况。" },
  ),
  p95_drawdown: calculatedTopic(
    "P95 最大回撤", "把每条模拟路径的最大回撤排序后得到的第 95 分位幅度。", "先计算每条路径从峰值到后续低点的最大跌幅，再取样本第 95 分位。", "使用本次模拟全部已完成路径。", "约 95% 的样本路径回撤不高于该值；不是‘有 95% 概率会亏这么多’。",
  ),
  failure_age: topic("首次资金不足年龄", "模拟路径第一次无法完整支付当期计划支出的年龄。", {
    interpretation: "P50 表示发生资金不足的路径中位年龄；没有失败的路径不应被当作 0。",
  }),
  recovery_period: topic("恢复期", "受到压力冲击后，资产或目标指标恢复到指定基准所需的月份。", {
    caveat: "‘规划期内未恢复’表示在当前规划终点前没有观察到恢复。",
  }),
  representative_path: topic("代表路径", "期末资产最接近某个分位数的一条真实模拟样本路径。", {
    caveat: "它便于查看过程，但不代表该分位数在每个月都沿着这条路径。",
  }),
  max_drawdown: calculatedTopic(
    "模拟最大回撤",
    "资产从此前高点跌到随后低点的最大跌幅。",
    "在参与模拟的连续完整自然年度窗口内，计算每个时点相对历史峰值的跌幅并取最大幅度。",
    "使用清洗后的历史月度收益与所选完整年度区间。",
    "数值越大代表历史窗口内出现过更深的峰谷损失；它不同于波动率和尾部损失。",
  ),
  annual_return: calculatedTopic(
    "年化收益",
    "把多期累计收益折算成每年的几何平均增长率。",
    "最近最多 20 个完整自然年度收益按几何方式年化，不使用不完整年度。",
    "使用当前资产纳入模拟的清洗后历史行情。",
    "用于描述历史复合增长速度，不是下一年的预测收益。",
  ),
  annual_volatility: calculatedTopic(
    "历史年化波动率",
    "衡量历史月度收益围绕平均值波动的幅度。",
    "月度对数收益的样本标准差乘以 √12。",
    "使用参与模拟的连续完整自然年度月度收益。",
    "越高表示历史收益起伏越大，不直接等于亏损概率。",
  ),
  rebalance_threshold: topic(
    "调仓阈值",
    "当前权重与目标权重相差达到多少个百分点时，才把标的列为需要调仓。",
    { interpretation: "阈值越小越敏感，可能产生更频繁的交易和更高成本。" },
  ),
  guardrail_withdrawal_rate: calculatedTopic(
    "护栏提取率",
    "以退休财富为基准动态调整生活支出的提取策略。",
    "初始提取率由退休首年生活支出减税后稳定收入后除以退休财富得到；上下护栏约束后续总生活支出。",
    "使用退休时财富、生活支出、稳定收入、通胀与护栏上下限。",
    "触及护栏会调整后续支出；它不是固定的安全提取率承诺。",
  ),
  weight_within_group: calculatedTopic(
    "组内占比",
    "标的在同一资产大类和地区组内部的目标比例。",
    "标的目标金额除以该大类地区组的目标金额。",
    "使用配置模板中的大类、地区和标的三层目标权重。",
    "同组标的合计应为 100%；它不是标的占全组合的比例。",
  ),
  simulation_snapshot_sync: topic(
    "模拟快照同步",
    "按当前市场数据重新生成该标的供 FIRE 模拟使用的历史统计快照。",
    { purpose: "会更新年化收益、波动率和数据区间等模拟输入。", caveat: "不会修改当前持仓金额，也不会自动执行交易。" },
  ),
  portfolio_weight: calculatedTopic(
    "全组合权重",
    "标的最终占整个组合的目标比例。",
    "资产大类权重 × 地区组内权重 × 标的组内权重。",
    "使用计划当前选择的配置模板和三层目标权重。",
    "全部启用标的的全组合权重应合计为 100%。",
  ),
  fee_included: topic(
    "费率口径",
    "历史净值或市场价格通常已经反映产品持有费率，因此模拟不重复扣除。",
    { caveat: "调仓交易成本仍按计划参数另行计入。" },
  ),
  stress_test: topic(
    "压力测试",
    "把指定冲击强制施加到模拟路径，用来观察计划在极端假设下的表现。",
    { interpretation: "结果描述该冲击条件下的影响，不代表冲击发生概率。" },
  ),
  sensitivity_test: topic(
    "敏感性测试",
    "每次只改变一个或两个指定参数，观察结果对输入变化的敏感程度。",
    { interpretation: "曲线展示离散评估点的关系，不是参数预测或连续保证。" },
  ),
  unallocated_gap: topic(
    "未分配规模缺口",
    "计划基准规模高于已录入持仓市值的差额。",
    { interpretation: "可能来自市场缩水或持仓未录全；持仓已如实录入时可同步下调基准，否则应补录持仓或现金。" },
  ),
  holdings_sum: calculatedTopic(
    "持仓合计",
    "当前计划中已启用标的的持仓金额合计。",
    "将所有启用持仓的当前金额相加。",
    "使用当前计划已录入的持仓，不包含未录入账户或资产。",
    "应与对应券商账户规模大致一致。",
  ),
  holdings_market_value: calculatedTopic(
    "持仓市值",
    "已录入且启用持仓的当前市值合计。",
    "将当前计划启用标的的当前金额相加。",
    "使用用户最近保存的持仓金额，不自动读取券商实时余额。",
    "反映已录入账户规模；与计划基准规模可以暂时不同。",
  ),
  invested_minor: topic("已投资金", "当前计划中已启用标的的持仓金额合计，用于衡量资金录入进度。"),
  configured_total_assets: topic(
    "计划基准规模",
    "创建或更新计划时填写的组合基准金额，用于目标金额、模拟和录入校验。",
    { caveat: "它不是实时市值；市值变化后可在调仓页同步。" },
  ),
  scale_gap_over: topic("规模超出", "持仓市值高于计划基准规模的差额。", {
    interpretation: "不代表需要全体减仓；可同步上调基准或保留当前基准。",
  }),
  structural_gap_row: calculatedTopic(
    "结构还差",
    "按当前持仓合计规模计算的目标结构与当前金额之差。",
    "当前持仓合计 × 目标权重 − 当前持仓金额。",
    "使用启用持仓的当前金额和计划目标权重。",
    "正数表示按目标结构还需买入，负数表示超配；调仓主建议依据此列。",
  ),
  structural_rebalance: topic("结构调仓", "保持持仓总市值不变，只调整各大类、地区和标的之间的比例。"),
  actionable_rebalance: calculatedTopic(
    "需调仓标的",
    "结构权重偏离目标且超过调仓阈值的标的数量。",
    "逐个比较当前结构权重与目标权重的绝对差，超过阈值则计入。",
    "使用当前持仓、目标权重和计划调仓阈值。",
    "数量越多表示当前结构偏离目标的标的越多，不代表必须立即交易。",
  ),
  asset_class_allocation: topic("大类配置", "权益、债券、现金三大类在全组合中的目标占比与当前占比对比。"),
  region_allocation: topic("地区配置", "各资产大类内部，国内与国外资产的目标和当前结构对比。"),
  deviation_amount: calculatedTopic(
    "偏差金额",
    "结构目标金额与当前持仓金额之间的差额。",
    "按当前持仓合计计算的目标金额减去当前持仓金额。",
    "使用当前持仓总额、标的目标权重和标的当前金额。",
    "正数表示待买入，负数表示超配待减仓。",
  ),
  asset_refresh: topic("持仓校正", "更新账户真实持仓金额，并可选择同步计划基准。", {
    caveat: "不会切换配置模板，也不会代表交易已经执行。",
  }),
  annual_savings_wizard: topic("当前年储蓄", "FIRE 之前每年预计新增投入组合的净储蓄。", {
    interpretation: "它影响退休前资产积累；不包含已有可投资资产。",
  }),
  config_template: topic("配置模板", "跨计划复用的权益、债券和现金目标权重模板。", {
    caveat: "模板决定目标配置，不直接决定模拟收益。",
  }),
  assumption_scenario: topic("假设情景", "保守、基准、乐观三档收益先验选择。", {
    caveat: "只影响 FIRE 模拟收益假设，不改变配置模板或压力测试冲击。",
  }),
  gap_color_semantics: topic("还差与超出的颜色", "绿色表示按目标结构仍需投入，红色表示当前超配。", {
    caveat: "颜色不代表盈利或亏损。",
  }),
  current_amount_vs_target: topic("当前金额与目标金额", "当前金额来自真实持仓；目标金额由当前规模和目标权重计算。"),
  fire_simulation_optional: topic("FIRE 模拟", "基于当前配置、现金流和市场统计生成长期退休路径。", {
    caveat: "不运行模拟也可管理持仓和调仓；模拟不是未来保证。",
  }),
  fire_params_for_simulation: topic("FIRE 模拟参数", "年龄、支出、储蓄、通胀和提取规则只进入 FIRE 模拟。", {
    caveat: "这些参数不改变调仓目标金额的计算方式。",
  }),
  portfolio_gap_row: topic("汇总结构还差", "大类或地区下所有标的结构差额之和。"),
  target_weight_portfolio: calculatedTopic(
    "全组合目标占比", "该维度占整个组合的目标比例。", "该维度目标金额除以组合目标总额。", "使用计划目标权重。", "所有同层维度合计应为 100%。",
  ),
  target_weight_within_asset_class: calculatedTopic(
    "大类内目标配比", "国内与国外在同一资产大类中的目标划分。", "地区目标金额除以该大类目标金额。", "使用地区组内目标权重。", "同一大类内地区配比合计应为 100%。",
  ),
  current_weight_portfolio: calculatedTopic(
    "全组合当前占比", "该维度占当前已录入持仓的比例。", "该维度当前金额除以启用持仓市值合计。", "使用当前保存的启用持仓。", "用于比较当前结构，不以计划基准规模为分母。",
  ),
  current_weight_within_asset_class: calculatedTopic(
    "大类内当前配比", "国内与国外在同一资产大类当前持仓中的划分。", "地区当前金额除以该大类当前持仓金额。", "使用当前保存的启用持仓。", "同一大类有持仓时，各地区合计为 100%。",
  ),
  result_stale: topic("结果过期", "结果使用的计划版本或市场快照已不是当前版本。", {
    interpretation: "历史结果仍可查看，但不应作为当前计划结论；请重新运行。",
  }),
  simulation_runs: topic("模拟路径数", "Monte Carlo 生成的独立样本路径数量。", {
    interpretation: "路径更多通常能降低抽样波动，但运行时间和资源消耗也更高。",
  }),
  student_t_df: topic("Student-t 自由度", "控制模拟收益分布尾部厚度的参数。", {
    interpretation: "自由度越低，极端收益样本相对更常见；它不是历史样本年数。",
  }),
  metric_cagr: calculatedTopic(
    "复合年增长率（CAGR）", "把整个回测窗口的累计增长折算成每年的复合增长率。", "(期末净值 ÷ 期初净值)^(1/年数) − 1。", "使用回测共同区间内扣除设定交易成本后的组合净值。", "越高表示该历史窗口复合增长越快，不代表未来回报。",
  ),
  metric_annual_volatility: calculatedTopic(
    "回测年化波动率", "回测收益起伏的年化统计幅度。", "周期收益的样本标准差按周期数年化。", "使用回测共同区间的组合收益。", "越低表示历史路径更平稳，但不等于回撤一定更小。",
  ),
  metric_max_drawdown: calculatedTopic(
    "回测最大回撤", "回测净值从历史峰值到后续低点的最大跌幅。", "逐点计算相对此前峰值的跌幅并取最大幅度。", "使用回测共同区间的组合净值。", "越低通常表示历史最深回撤较小。",
  ),
  metric_var_loss: calculatedTopic(
    "风险价值损失（VaR loss）", "给定置信度与持有期下，历史尾部场景的分位损失。", "对滚动持有期收益排序，在设定置信度位置取负收益作为损失。", "使用回测共同区间内的滚动持有期收益。", "正数表示损失；越低通常代表该分位尾部风险越小。", { caveat: "不是最大可能亏损，也不是未来保证。" },
  ),
  metric_cvar_loss: calculatedTopic(
    "条件风险价值损失（CVaR loss）", "比 VaR 边界更差的尾部场景平均损失。", "对落在 VaR 尾部之外的滚动持有期收益取平均并以损失展示。", "使用回测共同区间内的滚动持有期收益。", "正数表示损失；越低通常代表极端历史场景的平均损失较小。", { caveat: "不是预计最大亏损，也不是未来保证。" },
  ),

  monte_carlo: topic("Monte Carlo 模拟", "用同一组计划输入生成大量可能的收益、通胀和现金流路径，以观察结果分布。", {
    caveat: "结果只反映模型、输入和有限样本，不是单一预测。",
  }),
  wilson_interval: calculatedTopic(
    "Wilson 置信区间", "根据有限模拟路径估计成功率可能波动的统计区间。", "由成功路径数和总路径数按 Wilson score interval 计算。", "使用本次实际完成的成功路径数和路径总数。", "区间越窄表示该样本量下抽样不确定性越小；它不覆盖模型假设错误。",
  ),
  wilson_lower_bound: calculatedTopic(
    "Wilson 95% 下界", "本次有限模拟样本对成功率给出的保守统计边界。", "由成功路径数和总路径数计算 Wilson 95% 置信区间并取下界。", "使用来源模拟冻结的 seed 和前 N 条路径。", "下界达到目标表示样本对达标有更强支持，不代表真实未来有 95% 保证。",
  ),
  evaluation_paths: topic("评估路径数", "前沿或改善搜索在每个候选点实际评估的模拟路径数量。", {
    interpretation: "路径越多，抽样边界通常更稳定，但计算成本更高。",
  }),
  common_random_numbers: topic("同路径比较", "不同候选使用相同 seed 和相同 path number 的随机样本进行比较。", {
    purpose: "减少候选之间由随机抽样不同造成的噪声。",
  }),
  random_seed: topic("随机种子", "用于可复现地生成同一组随机路径的起始值。", {
    interpretation: "相同输入和 seed 可复现样本；seed 不代表更好或更差的未来。",
  }),
  discrete_search_step: topic("搜索步长", "相邻候选参数之间的离散间隔。", {
    interpretation: "结果是评估过的档位边界，不代表连续参数上的精确极值。",
  }),
  search_domain: topic("搜索范围", "允许前沿或改善器评估的参数下限、上限和约束区域。", {
    caveat: "域内无解不等于所有可能参数都无解。",
  }),
  path_month_budget: calculatedTopic(
    "评估预算", "用路径数与每条路径月数衡量一次搜索允许消耗的计算规模。", "每个评估点的路径数 × 规划月数，再对所有评估点累计。", "使用来源模拟规划期、评估路径数和实际候选点数量。", "预算越大可评估更多候选或路径，但运行时间也会上升。",
  ),
  paired_path_changes: calculatedTopic(
    "路径改善与回退", "比较同编号随机路径在原方案和候选方案下是否从失败变成功或从成功变失败。", "按相同 seed、path number 配对，分别统计失败→成功和成功→失败。", "使用来源方案与候选方案的逐路径结果。", "净改善为改善路径数减回退路径数；它解释样本变化，不是未来人数或概率保证。",
  ),
  nominal_vs_real: topic("名义金额与当前购买力", "名义金额包含未来价格上涨；当前购买力金额用累计通胀折回起点币值。", {
    interpretation: "比较跨年份生活水平时优先看当前购买力；查看届时账户数额时看名义金额。",
  }),
  terminal_wealth_floor: topic("期末最低资产目标", "规划结束时希望账户至少保留的名义资产金额。", {
    purpose: "模拟成功不仅要求中途不耗尽，还必须满足这个期末目标。",
  }),
  required_fire_capital: calculatedTopic(
    "所需 FIRE 资本", "在计划 FIRE 月，为支付后续退休现金流并满足期末目标所需的起始资产。", "从规划终点向前，按同一月度收益、通胀、稳定收入、支出和期末目标逐月反推。", "完全使用当前快算输入，不读取持仓或市场历史。", "低于该资本表示固定假设下存在缺口；它不是年支出除以收益率，也不是成功概率。",
  ),
  current_investable_assets: topic("当前可投资资产", "今天已经可用于这项 FIRE 计划投资和提款的资产金额。", {
    caveat: "不应包含自住房等不准备用于退休现金流的资产。",
  }),
  geometric_annual_return: topic("名义几何年化收益", "快算假设资产每年按这个固定复合速度增长，金额包含通胀影响。", {
    caveat: "它不是历史平均数的自动读取，也不包含逐年波动。",
  }),
  fixed_inflation: topic("固定通胀率", "快算中每年固定采用的价格上涨率，用于放大未来支出并折算当前购买力。", {
    caveat: "现实通胀会波动；完整模拟可使用随机通胀路径。",
  }),
  retirement_income_growth: topic("稳定收入增长率", "退休后稳定收入预计每年的名义增长率。", {
    interpretation: "低于通胀时，这项收入的当前购买力会逐年下降。",
  }),
  projected_fire_assets: calculatedTopic(
    "计划 FIRE 时资产", "从今天积累到计划 FIRE 月时预计拥有的名义资产。", "按月化固定收益增长当前资产，并在退休前加入按增长率变化的净储蓄。", "使用当前资产、FIRE 年龄、年储蓄、储蓄增长率和固定年化收益。", "用于与同一时点的所需 FIRE 资本比较。",
  ),
  fire_funding_gap: calculatedTopic(
    "FIRE 资金富余或缺口", "计划 FIRE 时资产与该时点所需资本的差额。", "计划 FIRE 时资产 − 所需 FIRE 资本。", "使用同一次快算的前向积累结果和反推资本结果。", "正数为固定假设下富余，负数为缺口；不是概率安全边际。",
  ),
  earliest_fire_age: calculatedTopic(
    "最早可 FIRE", "在当前快算固定假设下，前向资产首次达到对应所需资本的年龄。", "逐月比较预计资产与从该月反推的所需资本，取首次不低于的月份。", "使用当前所有快算输入。", "只在当前固定模型内成立，输入变化后需重新计算。",
  ),
  support_duration: calculatedTopic(
    "可完整支付时长", "从计划 FIRE 后开始，资产能够完整覆盖每月净支出的持续时间。", "按月执行收入、支出和固定投资收益结算，统计未发生支付不足的完整月份。", "使用退休现金流、固定收益、通胀和起始资产。", "时间不足意味着某个月无法完整支付，不表示此前账户余额一直宽裕。",
  ),
  depletion_age: topic("资产耗尽年龄", "快算月度路径中资产首次无法继续覆盖当期现金流的年龄。", {
    caveat: "固定模型没有市场路径波动，该年龄不是未来预测。",
  }),
  terminal_nominal_wealth: topic("期末名义资产", "目标年龄时账户显示的未来币值金额，尚未扣除累计通胀影响。"),
  terminal_real_wealth: calculatedTopic(
    "期末真实资产", "把目标年龄的名义资产折算为今天购买力后的金额。", "期末名义资产除以从今天到目标月的累计通胀因子。", "使用期末名义资产、固定通胀率和经过月数。", "适合比较购买力，不是届时账户显示金额。",
  ),
  real_annual_return: calculatedTopic(
    "实际年化收益", "扣除固定通胀影响后的复合年化收益率。", "(1 + 名义年化收益) ÷ (1 + 通胀率) − 1。", "使用快算输入的固定名义收益率和固定通胀率。", "正数表示固定假设下购买力增长；不包含波动或税费。",
  ),
  return_sequence_risk: topic("收益顺序风险", "平均收益相同但涨跌发生顺序不同，会让持续提款组合得到不同结局。", {
    caveat: "快算使用固定收益率，不包含这类风险；Monte Carlo 模拟才会生成不同路径顺序。",
  }),
  stable_retirement_income: topic("退休稳定年收入", "退休后预计每年获得、可抵扣生活支出的税后稳定收入。", {
    interpretation: "开始年龄和增长率会改变每年需要从组合提取的净额。",
  }),
  retirement_spending: topic("退休首年支出", "以当前购买力填写的退休后首年生活支出。", {
    interpretation: "模拟会按选定通胀和支出增长规则换算未来每期需要。",
  }),
  savings_growth: topic("储蓄增长率", "FIRE 前年净储蓄预计每年的名义增长率。", {
    interpretation: "只影响退休前后续年度新增投入，不改变当前已有资产。",
  }),
  fire_delay: topic("推迟 FIRE", "把退休年龄向后移动，同时延长储蓄期并缩短退休提款期。", {
    caveat: "若稳定收入从退休开始，开始时间也会随 FIRE 年龄一起推迟。",
  }),
  improvement_search: topic("FIRE 计划改善器", "只搜索退休年龄、年储蓄、退休支出和稳定收入四个现金流杠杆。", {
    caveat: "不会优化资产收益、市场假设或配置权重；应用后仍需运行正式模拟验证。",
  }),
  improvement_recipe: topic("改善方案类型", "单杠杆方案只调整一项；平衡方案按设定约束组合调整多项。", {
    interpretation: "候选先满足 Wilson 下界目标，再按调整成本和稳定规则排序。",
  }),
  search_boundary: topic("搜索边界", "列出改善器实际评估过的离散候选点及其判定。", {
    caveat: "它不是连续函数曲线；达到约束上限仍未达标只说明当前搜索域内无解。",
  }),
  random_inflation_ar1: topic("随机 AR(1) 通胀", "让每条模拟路径的通胀围绕长期均值随机波动，并保留一部分上月偏离。", {
    purpose: "用于观察持续高通胀或低通胀路径对支出的影响。",
  }),
  inflation_mu: topic("通胀长期均值 μ", "随机通胀过程长期回归的中心水平。", {
    interpretation: "它是长期中心，不是每个月固定采用的通胀率。",
  }),
  inflation_sigma: topic("通胀波动 σ", "随机通胀每月围绕条件均值变化的幅度。", {
    interpretation: "越高会生成更分散的通胀路径。",
  }),
  inflation_phi: calculatedTopic(
    "通胀持续性 φ", "本月通胀偏离长期均值后延续到下个月的比例。", "下一期偏离包含 φ × 本期偏离，再叠加随机波动。", "与长期均值 μ、波动 σ 共同生成每条通胀路径。", "越接近 1，冲击持续越久；越接近 0，越快回到均值。",
  ),
  withdrawal_fixed_real: topic("固定实际支出", "退休后按通胀调整生活支出，使当前购买力大致保持不变。"),
  withdrawal_fixed_portfolio: topic("组合百分比提款", "每期按组合资产的一定比例决定可提取金额。", {
    interpretation: "支出会随市场资产变化，较少出现固定支出下的完全耗尽，但生活水平也更波动。",
  }),
  withdrawal_guardrail: topic("护栏提款", "以初始提款计划为中心，在提取率越过上下护栏时调整支出。"),
  withdrawal_tax_rate: topic("有效提取税率", "从组合提取的应税部分预计实际承担的综合税率。", {
    purpose: "用于把税后生活支出换算为需要从组合提取的税前金额。",
  }),
  taxable_withdrawal_ratio: topic("应税提取比例", "每次从组合提取的金额中，预计需要按有效税率计税的比例。", {
    purpose: "与有效提取税率一起把税后支出换算为税前提取额。",
  }),
  transaction_cost_rate: topic("交易成本率", "模拟调仓时按成交金额扣除的成本比例。", {
    interpretation: "成本越高，频繁调仓对长期资产的拖累通常越大。",
  }),
  rebalance_frequency: topic("调仓检查频率", "模拟按月、季度或年度检查当前权重是否超过调仓阈值。", {
    interpretation: "检查更频繁可能更快回到目标结构，也可能增加交易成本。",
  }),
  return_assumption_mode: topic("收益假设来源", "决定模拟未来收益中心取自历史 CAGR、向长期先验收缩，还是用户自定义。", {
    caveat: "切换后旧模拟会过期；历史收益不代表未来收益。",
  }),
  assumption_profile_selection: topic("假设 Profile 选择", "跟随全局会在运行时使用当时的默认 Profile；固定版本会一直使用指定已发布版本。", {
    interpretation: "固定版本便于复现，跟随全局便于统一升级，但全局默认改变后结果需重新运行。",
  }),
  forward_return: topic("前瞻收益假设", "用于未来模拟的资产预期名义收益，不等于历史 CAGR。", {
    caveat: "它是模型输入，不是收益承诺。",
  }),
  fx_forward_return: topic("FX 前瞻收益", "外币相对计划基准币种的预期汇率变化率。", {
    purpose: "与资产本币收益共同合成为基准币种收益。",
  }),
  base_currency_return: calculatedTopic(
    "基准币种合成收益", "把资产本币收益和汇率变化合并后的计划币种收益。", "(1 + 资产本币收益) × (1 + FX 收益) − 1。", "使用资产前瞻收益、FX 前瞻收益和计划基准币种。", "外币资产即使本币上涨，也可能被不利汇率变化抵消。",
  ),
  historical_weight: topic("历史权重", "前瞻收益收缩时分配给历史统计的相对权重。", {
    interpretation: "样本更长时通常更依赖历史；仍不表示历史一定会重复。",
  }),
  sample_years: topic("样本年数", "该资产实际用于估计收益和风险的完整历史长度。", {
    interpretation: "年数较少会提高估计不确定性，并可能让结果更多依赖先验。",
  }),
  scenario_comparison: topic("假设情景比较", "在冻结计划输入、市场快照和随机路径的前提下，只切换保守、基准、乐观收益假设。", {
    interpretation: "行间差异主要来自情景假设，不是三次不同随机抽样。",
  }),
  prior_strength: topic("先验等效样本强度", "控制预设收益或相关性与历史数据合并时相当于多少年或多少月的证据。", {
    interpretation: "越高表示结果更靠近先验，越低表示更依赖当前历史样本。",
  }),
  correlation_shrinkage: topic("相关性收缩", "把样本相关性向较稳定的先验矩阵拉近，减少短历史带来的极端估计。", {
    interpretation: "收缩更强不代表资产真实相关性一定更低。",
  }),
  psd_repair: topic("相关矩阵 PSD 修复", "把不可用于联合随机采样的相关矩阵调整为半正定矩阵。", {
    interpretation: "修复较大表示原始设定内部不够一致，应复核相关系数，而不是只接受修复结果。",
  }),
  return_log_shift: topic("收益对数位移", "在对数收益空间给情景中的资产年化收益增加或减少的偏移。", {
    interpretation: "负值形成更保守的收益中心，正值形成更乐观的收益中心；不是简单百分点相加。",
  }),
  fx_return_shift: topic("FX 收益位移", "在对数收益空间调整外币相对基准币种的情景收益中心。", {
    purpose: "只影响带独立 FX 因子的外币资产基准币种收益。",
  }),
  volatility_multiplier: topic("波动率乘子", "情景对基础波动率统一放大或缩小的倍数。", {
    interpretation: "1 表示不变，大于 1 表示情景波动更高。",
  }),
  return_prior: topic("收益先验", "在历史样本之外为不同资产类别、地区和计价币种提供长期名义几何收益与波动边界。", {
    caveat: "先验会与历史按等效样本强度合并，不是收益承诺。",
  }),
  fx_prior: topic("FX 先验", "为外币相对基准币种提供长期汇率收益中心和波动边界。", {
    caveat: "只有使用独立 FX 因子的资产才会应用对应先验。",
  }),
  correlation_rho: topic("相关系数 ρ", "描述两个模拟因子共同涨跌方向和强度，范围为 −1 到 1。", {
    interpretation: "1 为完全同向，−1 为完全反向，0 表示线性相关较弱；同类型不同资产项描述两个不同资产，不是变量与自身。",
  }),
  minimum_eigenvalue: topic("相关矩阵最小特征值", "用于判断相关矩阵能否稳定地用于联合随机采样。", {
    interpretation: "明显为负表示矩阵不半正定，需要复核或进行 PSD 修复。",
  }),
  assumption_evidence: topic("假设来源与审核", "记录先验的来源类型、出处、发布日期、审核人和审核日期，便于追溯版本。", {
    caveat: "发布日描述原始材料，审核日描述本系统确认时间，两者含义不同。",
  }),
  sharpe_ratio: calculatedTopic(
    "Sharpe 比率", "每承担一单位波动获得的超额年化收益。", "(年化收益 − 无风险利率) ÷ 年化波动率。", "使用当前回测共同区间的收益、波动率和页面采用的无风险利率口径。", "越高表示该历史窗口的波动调整后收益较高；负收益时需谨慎比较。",
  ),
  calmar_ratio: calculatedTopic(
    "Calmar 比率", "年化收益相对最大回撤幅度的比值。", "CAGR ÷ 最大回撤幅度。", "使用当前回测共同区间的 CAGR 和最大回撤。", "越高表示该历史窗口每单位最深回撤对应的复合收益较高。",
  ),
  rolling_metric: topic("滚动指标", "用固定长度窗口沿时间移动，在每个终点重新计算收益或风险。", {
    interpretation: "开头样本不足时没有数值；相邻点会共享大部分历史数据。",
  }),
  data_as_of: topic("数据截至日期", "当前统计或曲线实际使用的最后一个市场数据日期。", {
    caveat: "它不一定等于今天，也不代表实时行情。",
  }),
  adjustment_policy: topic("复权口径", "决定价格序列如何处理分红、拆分等公司行为。", {
    interpretation: "前复权、后复权和不复权的价格绝对值不可直接混比，收益统计必须保持同一口径。",
  }),
  point_type: topic("行情点类型", "历史序列使用净值、收盘价或其他数据点的口径。", {
    caveat: "不同点类型可能包含不同费用与分红信息。",
  }),
  canonical_fund_symbol: topic("净值主代码", "系统用于定位基金净值历史的标准化市场代码。", {
    caveat: "它是数据映射标识，不是交易建议或持仓代码校验。",
  }),
  complete_year: topic("完整年度", "从该自然年首个有效周期覆盖到末个有效周期、可用于年度统计的年份。"),
  partial_year: topic("部分年度", "只覆盖自然年一部分的数据区间。", {
    caveat: "通常不纳入 CAGR、年度波动或年度回撤等完整年度统计。",
  }),
  quick_fire_fixed_model: topic("快算固定模型", "按固定月度收益和确定性通胀逐月推进现金流，并从终点反推所需资本。", {
    caveat: "不包含波动、相关性、FX、交易费、税务和收益顺序风险，也不输出成功概率。",
  }),
  common_backtest_interval: topic("回测共同区间", "所有组合资产都有可用数据的重叠时间范围。", {
    purpose: "确保同一期组合收益不会混用不同资产的缺失历史。",
  }),
  tail_risk_confidence: topic("尾部风险置信度", "决定 VaR/CVaR 关注收益分布中多靠后的不利场景。", {
    interpretation: "95% CVaR 对应最差约 5% 场景的平均损失；置信度越高，尾部样本更少。",
  }),
  tail_risk_horizon: topic("尾部风险持有期", "把连续若干有效收益日复合成一个风险场景的长度。", {
    caveat: "相邻滚动场景会共享部分交易日，因此不是完全独立样本。",
  }),
  backtest_rebalance: topic("回测再平衡规则", "决定回测何时把漂移后的资产权重恢复到目标权重。", {
    interpretation: "频率或阈值不同会改变换手、交易成本和组合路径。",
  }),
  risk_free_rate: topic("无风险利率", "计算 Sharpe 比率时从组合年化收益中扣除的年化基准利率。", {
    caveat: "它只影响风险调整指标，不直接改变回测净值。",
  }),
  benchmark_asset: topic("基准资产", "用于把组合收益和回撤与一个参考资产放在同一历史区间比较。", {
    caveat: "基准不参与组合权重或调仓。",
  }),
  normalized_nav: topic("归一化净值", "把序列起点设为 1，展示此后累计增长的相对路径。", {
    interpretation: "便于比较不同价格尺度的组合和资产，不是实际单位净值或账户金额。",
  }),
  cumulative_return: calculatedTopic(
    "累计收益", "从回测起点到当前或终点的总收益比例。", "当前归一化净值 ÷ 起点净值 − 1。", "使用回测共同区间内扣除设定交易成本后的净值。", "正数表示相对起点增长，负数表示低于起点；不做年化。",
  ),
  weight_deviation: calculatedTopic(
    "权重偏离", "实际权重相对目标权重的最大绝对差。", "对各资产计算 |实际权重 − 目标权重| 并取最大值。", "使用回测每个时点的漂移权重和集合目标权重。", "越高表示组合离目标结构越远；是否调仓还取决于规则。",
  ),
  positive_month_ratio: calculatedTopic(
    "正收益月份占比", "回测中月收益大于 0 的月份比例。", "正收益月份数除以有完整月收益的月份总数。", "使用回测共同区间的月度组合收益。", "越高表示上涨月份更多，但不说明上涨幅度大于下跌幅度。",
  ),
  drawdown_duration: topic("回撤持续期", "净值从峰值跌落后，到重新达到该峰值所经历的天数。", {
    interpretation: "当前值表示尚未收复的本轮回撤；最长值表示历史最长一次峰值到收复。",
  }),
  portfolio_turnover: calculatedTopic(
    "累计单边换手", "所有再平衡时点买卖权重变动规模的累计值。", "每次取 0.5 × Σ|漂移权重 − 目标权重|，再跨时点相加。", "使用扣费前漂移权重、目标权重和实际调仓时点。", "越高通常意味着交易更频繁；首次建仓不计入。",
  ),
  transaction_cost_drag: calculatedTopic(
    "交易成本拖累", "相同调仓路径下，交易成本让期末结果减少的比例。", "不计费用终值与计费后终值之差除以初始净值。", "使用回测初始资金、每次单边换手和交易费率。", "越高表示费用对该历史路径的影响越大。",
  ),
  worst_period_loss: topic("最差持有期损失", "当前回测冻结区间内观察到的最差滚动持有期收益，以损失展示。", {
    caveat: "只是历史样本最差值，不是未来最大可能损失。",
  }),
  linear_log_scale: topic("线性与对数坐标", "线性轴展示绝对净值差；对数轴让相同比例涨跌占据相近视觉距离。", {
    interpretation: "切换坐标只改变显示，不改变回测数据或指标。",
  }),
  optimization_constraints: topic("自动调优约束", "候选必须满足权重步长、锁定权重、候选上限和最低 CAGR 等条件。", {
    caveat: "无结果通常表示当前离散候选中没有同时满足约束的组合。",
  }),
  optimization_tie_break: topic("候选并列规则", "主排序指标相同时，系统按预设次级指标和稳定顺序决定排名。", {
    purpose: "确保相同输入可复现同一候选顺序。",
  }),
  forward_fill: topic("前值填充", "缺少某个日期价格时暂用最近一次有效值补齐。", {
    caveat: "会把缺失期收益视为零，可能压低短期波动；页面应同时展示数据质量提示。",
  }),
  return_contribution: calculatedTopic(
    "收益贡献", "每个资产对组合区间收益的贡献拆分。", "按该期资产权重与资产收益计算并跨期汇总。", "使用回测权重、资产收益和调仓时点。", "贡献之和用于解释组合收益；高贡献可能来自高权重或高收益。",
  ),
  risk_contribution: calculatedTopic(
    "风险贡献", "每个资产对组合波动的边际贡献拆分。", "由资产权重和协方差矩阵计算，并归一化到组合总风险。", "使用回测共同区间收益和组合权重。", "贡献高表示该资产对组合波动影响大，不等于它单独波动最高。",
  ),
  drawdown_contribution: topic("回撤贡献", "在组合最大回撤窗口内，各资产对组合损失的拆分。", {
    caveat: "只解释该特定历史回撤窗口，不代表所有压力情景。",
  }),
  correlation_matrix: topic("相关性矩阵", "描述资产收益共同涨跌方向和强度的成对统计表。", {
    interpretation: "接近 1 表示同向，接近 −1 表示反向，接近 0 表示线性关系较弱。",
  }),
  optimization_ranking: topic("候选排序", "自动调优按当前目标指标、约束和并列规则排列已评估候选。", {
    caveat: "排名只在已生成候选和该段历史中成立，不代表全局最优或未来最优。",
  }),
  optimization_weight_step: topic("权重步长", "自动调优生成相邻权重候选时采用的离散百分点间隔。", {
    interpretation: "步长越小候选更细，但组合数量和计算成本会快速增加。",
  }),
  instrument_kind: topic("资产类型与数据类别", "资产类型描述投资工具，数据类别描述系统如何获取和解释其历史序列。"),
  readiness_status: topic("模拟数据就绪状态", "汇总资产历史、FX、完整年度和快照是否满足模拟要求。", {
    interpretation: "未就绪项会阻止或降低模拟可信度；展开原因可看到可执行的修复动作。",
  }),
  admin_worker_task: topic("Worker 任务", "由指定 worker 类型领取并执行的后台异步任务。", {
    interpretation: "pending 等待领取，running 正在执行，终态表示成功、失败或取消。",
  }),
  admin_worker_type: topic("Worker 类型", "决定哪一类执行器有权领取任务。", {
    interpretation: "Go Worker 执行服务端计算，Sidecar Worker 执行对应外部数据或专用计算任务；类型不匹配时不会领取。",
  }),
  admin_task_scope: topic("任务范围", "任务所归属的计划、研究组合、市场资产或系统级对象。", {
    interpretation: "scope type 表示对象种类，scope id 用于追踪具体业务对象。",
  }),
  admin_task_attempt: topic("执行尝试", "任务每次被领取并开始执行都会形成一次 attempt。", {
    interpretation: "当前次数达到最大次数后仍失败，任务将不再自动重试。",
  }),
  admin_claim: topic("任务领取", "空闲 worker 从等待队列中原子领取与自身类型匹配的任务。", {
    caveat: "领取成功只代表获得执行权，不代表任务已经完成。",
  }),
  admin_heartbeat: topic("任务心跳", "执行中的 worker 定期上报进度并续期租约。", {
    interpretation: "心跳滞留表示服务端已长时间未收到上报，任务会按租约和重试策略被回收。",
  }),
  admin_pre_complete: topic("等待终结", "worker 已上传结果，等待 Go finalizer 校验并写入业务表。", {
    caveat: "此阶段尚不能视为业务完成；最终落库失败时会重试或进入失败状态。",
  }),
  admin_retry_exhausted: topic("重试耗尽", "任务已使用完允许的执行或收尾重试次数。", {
    interpretation: "需要结合 attempt、错误码和 request/task ID 排查根因后重新发起。",
  }),
  admin_lease: topic("任务租约", "worker 领取任务后获得的限时执行权，并通过心跳续期。", {
    interpretation: "租约过期可让服务端识别失联执行者并按策略恢复任务。",
  }),
  admin_finalization: topic("任务收尾", "worker 完成计算后，服务端校验并持久化最终业务结果的阶段。", {
    caveat: "pre-complete 只表示 worker 已上报，不代表业务结果已经成功落库。",
  }),
  admin_data_version: topic("数据版本", "一次可审计的市场数据或目录同步版本，用于关联任务、结果和错误。"),
  admin_sync_health: topic("同步健康", "汇总目录、资产历史和汇率数据最近一次成功时间与活跃任务。", {
    interpretation: "过期或从未同步会影响资产可用性与模拟快照；应沿任务链接查看失败原因。",
  }),
  admin_payload_result: topic("Payload 与结果", "任务输入、结果元数据和错误信息的原始排障上下文。", {
    caveat: "其中可能包含内部字段；排障时应将 task ID、request ID、错误码和时间一起关联。",
  }),
} satisfies Record<string, HelpTopic>;

export type HelpTopicKey = keyof typeof HELP_TOPICS;

/** Compatibility maps retained for callers that only need a short string. */
export const TERMS: Record<string, string> = Object.fromEntries(
  Object.entries(HELP_TOPICS).map(([key, value]) => [key, value.summary]),
);

export const TERM_LABELS: Record<string, string> = Object.fromEntries(
  Object.entries(HELP_TOPICS).map(([key, value]) => [key, value.label]),
);

export function helpTopicForTerm(key?: string): HelpTopic | undefined {
  if (!key) return undefined;
  return HELP_TOPICS[key as HelpTopicKey];
}

export function helpForTerm(key?: string): string | undefined {
  return helpTopicForTerm(key)?.summary;
}

export function labelForTerm(key?: string): string | undefined {
  return helpTopicForTerm(key)?.label;
}
