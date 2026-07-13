"use client";

import { useMemo, useState } from "react";
import { useSearchParams } from "next/navigation";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { Dialog } from "@/components/ui/Dialog";
import { ErrorState } from "@/components/ui/ErrorState";
import { MoneyInput } from "@/components/ui/MoneyInput";
import { PageHeader } from "@/components/ui/PageHeader";
import { PageSkeleton } from "@/components/ui/Skeleton";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { Tooltip } from "@/components/ui/Tooltip";
import { useActiveTaskRestore } from "@/hooks/useActiveTaskRestore";
import { useTaskStatus } from "@/hooks/useTaskStatus";
import {
  applyFrontierPoint,
  createFrontierRun,
  getFrontierReadiness,
  getFrontierRun,
  listFrontierRuns,
  previewFrontierPoint,
  type FrontierRequest,
} from "@/lib/api/frontiers";
import { getParameters, getPlan } from "@/lib/api/plans";
import { listSimulations } from "@/lib/api/simulations";
import { isTaskActive } from "@/lib/api/tasks";
import { formatDateTimeFromMs, formatMoney, formatPercent } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type {
  FrontierPoint,
  FrontierPreview,
  FrontierReadiness,
  FrontierResult,
  FrontierRun,
  FrontierType,
  PlanParameters,
} from "@/types/api";

type FrontierQuestion = {
  type: FrontierType;
  title: string;
  description: string;
  purpose: string;
  changes: string;
  fixed: string;
  logic: string;
};

const TYPE_CARDS: FrontierQuestion[] = [
  {
    type: "retirement_age_max_spending",
    title: "不同退休年龄可承受多少支出",
    description: "逐个整数退休年龄寻找达标的最大年度退休支出。",
    purpose: "比较退休早晚对退休后可承受生活支出的影响。",
    changes: "对每个整数退休年龄，只改变退休年龄和首年年度支出。",
    fixed: "当前资产、退休前年度储蓄、退休稳定收入、持仓与权重、通胀/提款规则、收益风险假设、seed 和路径均保持源模拟口径。",
    logic: "在金额离散网格上寻找 Wilson 95% 下界达到目标的最大支出，并保存高一个 step 的未达标邻点；若端点已说明整个搜索域，则不向域外推算。",
  },
  {
    type: "retirement_age_min_savings",
    title: "不同退休年龄至少需要储蓄多少",
    description: "逐个整数退休年龄寻找达标的最低首年年度储蓄。",
    purpose: "比较退休早晚对从现在开始每年至少要储蓄多少的影响。",
    changes: "对每个整数退休年龄，只改变退休年龄和首年年度储蓄；后续储蓄仍使用源方案的增长率。",
    fixed: "当前资产、年度支出、退休稳定收入、持仓与权重、通胀/提款规则、收益风险假设、seed 和路径均保持源模拟口径。",
    logic: "在金额离散网格上寻找 Wilson 95% 下界达到目标的最低储蓄，并保存低一个 step 的未达标邻点；不会在两个档位之间插值。",
  },
  {
    type: "required_current_assets",
    title: "按当前方案需要多少资产",
    description: "退休年龄和现金流不变，寻找达标所需的最低当前资产。",
    purpose: "回答如果退休时间、储蓄和支出计划都照旧，今天至少需要多少起始资产。",
    changes: "只改变起始总资产，并按源模拟中各启用持仓的金额比例同比缩放；源总资产为 0 时才使用冻结目标权重。",
    fixed: "退休年龄、年度储蓄、年度支出、退休稳定收入、资产构成、通胀/提款规则、收益风险假设、seed 和路径全部不变。",
    logic: "每个候选资产金额都用同一批路径重新运行正式模拟，寻找 Wilson 95% 下界达到目标的最低金额，并保存低一个 step 的未达标邻点。",
  },
  {
    type: "coast_required_assets",
    title: "停止新增储蓄后需要多少资产",
    description: "从现在起年度储蓄为 0，仍按当前年龄退休。",
    purpose: "回答从现在开始不再投入新储蓄，已有资产至少要达到多少才能按原退休年龄和支出计划达标。",
    changes: "先把首年年度储蓄固定为 0，再改变起始总资产并按冻结持仓比例缩放。",
    fixed: "源方案退休年龄、年度支出、退休稳定收入、资产构成、通胀/提款规则、收益风险假设、seed 和路径全部不变。",
    logic: "在金额离散网格上寻找停止储蓄后仍达到目标 Wilson 下界的最低资产，并保存低一个 step 的未达标邻点。",
  },
];

const TYPE_LABEL = Object.fromEntries(TYPE_CARDS.map((card) => [card.type, card.title])) as Record<FrontierType, string>;

const STATUS_LABEL: Record<FrontierPoint["status"], string> = {
  boundary_found: "找到离散边界",
  entire_domain_feasible: "整个搜索域均达标",
  no_feasible_value: "搜索域内无达标值",
};

const PHASE_LABEL: Record<string, string> = {
  validating: "校验冻结输入",
  evaluating_baseline: "重算同口径基线",
  searching: "搜索离散边界",
  validating_result: "验证边界与单调性",
  complete: "已完成",
};

const DEFAULT_TARGET_PERCENT = 95;
const DEFAULT_EVALUATION_RUNS = 10_000;

function isAgeType(type: FrontierType) {
  return type === "retirement_age_max_spending" || type === "retirement_age_min_savings";
}

function QuestionHelp({ question }: { question: FrontierQuestion }) {
  return (
    <span className="absolute right-3 top-3 z-10">
      <Tooltip
        content={(
          <span className="block space-y-2">
            <span className="block"><strong>作用：</strong>{question.purpose}</span>
            <span className="block"><strong>改变：</strong>{question.changes}</span>
            <span className="block"><strong>保持：</strong>{question.fixed}</span>
            <span className="block"><strong>计算：</strong>{question.logic}</span>
          </span>
        )}
        align="center"
        clickToggle
        followCursor
        contentClassName="w-96 max-w-[calc(100vw-2rem)]"
        contentTestId={`frontier-question-help-${question.type}`}
      >
        <button
          type="button"
          aria-label={`了解「${question.title}」的作用和计算逻辑`}
          className="inline-flex h-5 w-5 items-center justify-center rounded-full border border-line bg-surface text-xs font-semibold text-ink-muted transition-colors hover:bg-surface-muted hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus"
        >
          ?
        </button>
      </Tooltip>
    </span>
  );
}

function pointConclusion(point: FrontierPoint, run: FrontierRun) {
  const value = formatMoney(point.value_minor);
  if (point.status === "boundary_found") {
    const neighbor = point.worse_neighbor ? formatMoney(point.worse_neighbor.value_minor) : "相邻档位";
    switch (run.frontier_type) {
      case "retirement_age_max_spending":
        return `结论：在 ${point.retirement_age} 岁退休时，本次网格内最大达标年度支出为 ${value}；高一个 step 的 ${neighbor} 未达标。`;
      case "retirement_age_min_savings":
        return `结论：在 ${point.retirement_age} 岁退休时，本次网格内最低达标首年年度储蓄为 ${value}；低一个 step 的 ${neighbor} 未达标。`;
      case "required_current_assets":
        return `结论：保持冻结方案其余内容不变时，本次网格内最低达标当前资产为 ${value}；低一个 step 的 ${neighbor} 未达标。`;
      case "coast_required_assets":
        return `结论：把年度储蓄设为 0 后，本次网格内最低达标当前资产为 ${value}；低一个 step 的 ${neighbor} 未达标。`;
    }
  }
  const isMaximum = run.frontier_type === "retirement_age_max_spending";
  if (point.status === "entire_domain_feasible") {
    return isMaximum
      ? `结论：搜索上限 ${value} 仍然达标，只能确定最大可承受支出大于或等于该上限；没有计算域外金额。`
      : `结论：搜索下限 ${value} 已经达标，只能确定所需金额小于或等于该下限；没有计算域外金额。`;
  }
  return isMaximum
    ? `结论：搜索下限 ${value} 仍未达标，只能确定最大可承受支出低于该下限。`
    : `结论：搜索上限 ${value} 仍未达标，只能确定所需金额高于该上限。`;
}

function modelLabel(value: string) {
  return ({
    historical_cagr: "历史 CAGR 复演",
    blended_prior: "历史数据与前瞻先验混合",
    custom: "自定义收益假设",
    multivariate_student_t: "多变量相关 Student-t 因子模型",
    independent_student_t: "独立 Student-t 因子模型",
  } as Record<string, string>)[value] ?? (value || "源模拟冻结模型");
}

function strategyLabel(value: string) {
  return ({
    fixed_real: "固定实际金额",
    random_ar1: "随机 AR(1) 通胀",
    fixed_portfolio: "按资产比例提款",
    guardrail: "护栏提款",
    monthly: "每月再平衡",
    quarterly: "每季再平衡",
    annual: "每年再平衡",
    conservative: "保守情景",
    baseline: "基准情景",
    optimistic: "乐观情景",
    follow_global: "跟随全局情景",
  } as Record<string, string>)[value] ?? value;
}

function CalculationBasis({ run }: { run: FrontierRun }) {
  const question = TYPE_CARDS.find((item) => item.type === run.frontier_type)!;
  const basis = run.frozen_basis;
  return (
    <section className="rounded-lg border border-brand/25 bg-brand/5 p-4" aria-labelledby="frontier-calculation-basis">
      <h3 id="frontier-calculation-basis" className="font-semibold text-ink">这次到底计算了什么</h3>
      <p className="mt-1 text-sm text-ink">{question.purpose}</p>
      <div className="mt-3 grid gap-3 text-sm md:grid-cols-3">
        <div className="rounded-md bg-surface p-3"><strong className="block text-ink">候选中改变</strong><span className="mt-1 block text-ink-muted">{question.changes}</span></div>
        <div className="rounded-md bg-surface p-3"><strong className="block text-ink">始终保持冻结</strong><span className="mt-1 block text-ink-muted">{question.fixed}</span></div>
        <div className="rounded-md bg-surface p-3"><strong className="block text-ink">如何得出结果</strong><span className="mt-1 block text-ink-muted">{question.logic}</span></div>
      </div>
      <h4 className="mt-4 text-sm font-semibold text-ink">本次使用的冻结数据</h4>
      <dl className="mt-2 grid gap-x-6 gap-y-2 text-sm sm:grid-cols-2 lg:grid-cols-3">
        <div><dt className="text-ink-muted">来源正式模拟</dt><dd className="break-all">{run.source_simulation_run_id}</dd></div>
        <div><dt className="text-ink-muted">年龄与期限</dt><dd>当前 {basis.current_age} 岁 · 原退休 {basis.retirement_age} 岁 · 计算至 {basis.end_age} 岁</dd></div>
        <div><dt className="text-ink-muted">源当前资产</dt><dd>{formatMoney(basis.total_assets_minor)} {basis.base_currency}</dd></div>
        <div><dt className="text-ink-muted">源年度现金流</dt><dd>储蓄 {formatMoney(basis.annual_savings_minor)}（年增 {formatPercent(basis.annual_savings_growth_rate)}） · 支出 {formatMoney(basis.annual_spending_minor)} · 退休收入 {formatMoney(basis.annual_retirement_income_minor)}（年增 {formatPercent(basis.annual_retirement_income_growth_rate)}）</dd></div>
        <div><dt className="text-ink-muted">资产与缩放口径</dt><dd>{basis.asset_count} 个启用持仓 · {basis.asset_scaling_basis === "source_amount_proportions" ? "按源金额比例" : "按冻结目标权重"}</dd></div>
        <div><dt className="text-ink-muted">收益风险模型</dt><dd>{modelLabel(basis.return_assumption_mode)} · {modelLabel(basis.random_factor_model)}{basis.return_assumption_scenario ? ` · ${strategyLabel(basis.return_assumption_scenario)}` : ""}</dd></div>
        <div><dt className="text-ink-muted">模拟路径</dt><dd>源共 {basis.source_simulation_runs.toLocaleString()} 条，固定使用前 {run.evaluation_runs.toLocaleString()} 条 · seed {basis.seed}</dd></div>
        <div><dt className="text-ink-muted">搜索网格</dt><dd>{formatMoney(run.config.search.min_minor)} 至 {formatMoney(run.config.search.max_minor)} · step {formatMoney(run.config.search.step_minor)}{run.config.retirement_age_range ? ` · ${run.config.retirement_age_range.min}–${run.config.retirement_age_range.max} 岁` : ""}</dd></div>
        <div><dt className="text-ink-muted">通胀 / 提款 / 再平衡</dt><dd>{strategyLabel(basis.inflation_mode)} · {strategyLabel(basis.withdrawal_type)} · {strategyLabel(basis.rebalance_frequency)}</dd></div>
        <div><dt className="text-ink-muted">达标门槛</dt><dd>成功率 Wilson 95% 下界 ≥ {formatPercent(run.config.target_success_probability)}</dd></div>
      </dl>
      <p className="mt-3 text-xs text-ink-muted">所有候选与同口径基线都重新运行正式模拟引擎，并使用同一个 seed 和相同 path number；这里展示的是 run 创建时的冻结值，不会随当前计划变化。</p>
    </section>
  );
}

function defaultSearch(type: FrontierType, parameters: PlanParameters) {
  if (type === "retirement_age_max_spending") {
    const step = Math.max(1, Math.round(parameters.annual_spending_minor / 10));
    return { min: step, max: step * 20, step };
  }
  if (type === "retirement_age_min_savings") {
    const step = Math.max(1, Math.round(Math.max(parameters.annual_savings_minor, 12_000_00) / 10));
    return { min: 0, max: step * 20, step };
  }
  const step = Math.max(1, Math.round(Math.max(parameters.total_assets_minor, 10_000_00) / 10));
  return { min: step, max: step * 20, step };
}

function download(name: string, type: string, content: string) {
  const url = URL.createObjectURL(new Blob([content], { type }));
  const anchor = document.createElement("a");
  anchor.href = url;
  anchor.download = name;
  anchor.click();
  URL.revokeObjectURL(url);
}

function csv(result: FrontierResult) {
  const header = [
    "age", "value_minor", "status", "runs", "success_count", "success_probability",
    "success_wilson_low", "success_wilson_high", "neighbor_value_minor",
    "neighbor_success_count", "neighbor_wilson_low", "outcome_hash", "snapshot_hash",
  ];
  const rows = result.points.map((point) => [
    point.retirement_age || "", point.value_minor, point.status, point.evaluation.runs,
    point.evaluation.success_count, point.evaluation.success_probability,
    point.evaluation.success_wilson_low, point.evaluation.success_wilson_high,
    point.worse_neighbor?.value_minor ?? "", point.worse_neighbor?.success_count ?? "",
    point.worse_neighbor?.success_wilson_low ?? "", point.evaluation.outcome_hash,
    point.evaluation.snapshot_hash,
  ]);
  return [header, ...rows].map((row) => row.map((value) => `"${String(value).replaceAll('"', '""')}"`).join(",")).join("\n");
}

function FrontierChart({ result }: { result: FrontierResult }) {
  const values = result.points.map((point) => point.value_minor);
  const min = Math.min(...values);
  const max = Math.max(...values);
  const span = Math.max(1, max - min);
  const x = (index: number) => result.points.length === 1 ? 300 : 36 + index * (528 / (result.points.length - 1));
  const y = (value: number) => 172 - ((value - min) / span) * 132;
  const feasibleSegments: string[] = [];
  let segment: string[] = [];
  result.points.forEach((point, index) => {
    if (point.status === "no_feasible_value") {
      if (segment.length > 1) feasibleSegments.push(segment.join(" "));
      segment = [];
    } else {
      segment.push(`${x(index)},${y(point.value_minor)}`);
    }
  });
  if (segment.length > 1) feasibleSegments.push(segment.join(" "));
  return (
    <figure className="rounded-lg border border-line bg-surface p-3" aria-labelledby="frontier-chart-caption">
      <svg viewBox="0 0 600 210" className="h-auto w-full" role="img" aria-label="FIRE 达标前沿离散点图">
        <line x1="36" y1="172" x2="564" y2="172" stroke="currentColor" className="text-line" />
        {feasibleSegments.map((points) => <polyline key={points} points={points} fill="none" stroke="currentColor" strokeWidth="2" className="text-brand" />)}
        {result.points.map((point, index) => (
          <g key={point.id}>
            <circle
              cx={x(index)} cy={y(point.value_minor)} r="6"
              fill={point.status === "no_feasible_value" ? "white" : "currentColor"}
              stroke="currentColor" strokeWidth="2"
              className={point.status === "no_feasible_value" ? "text-warning" : "text-brand"}
            >
              <title>{`${point.retirement_age ? `${point.retirement_age} 岁，` : ""}${formatMoney(point.value_minor)}；${STATUS_LABEL[point.status]}；Wilson 下界 ${formatPercent(point.evaluation.success_wilson_low)}`}</title>
            </circle>
            <text x={x(index)} y="195" textAnchor="middle" className="fill-ink-muted text-[10px]">
              {point.retirement_age || "当前"}
            </text>
          </g>
        ))}
      </svg>
      <figcaption id="frontier-chart-caption" className="text-xs text-ink-muted">
        {result.discrete_connection_note} 空心点表示搜索域内没有达标值，不参与连线。
      </figcaption>
    </figure>
  );
}

function PointEvidence({ point, run, onPreview }: {
  point: FrontierPoint;
  run: FrontierRun;
  onPreview: (point: FrontierPoint) => void;
}) {
  const assetType = !isAgeType(run.frontier_type);
  return (
    <article className="rounded-lg border border-line bg-surface p-4" data-testid={`frontier-point-${point.id}`}>
      <div className="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h3 className="font-semibold text-ink">
            {point.retirement_age ? `${point.retirement_age} 岁` : TYPE_LABEL[run.frontier_type]}
          </h3>
          <p className="mt-1 text-sm text-ink-muted">{STATUS_LABEL[point.status]}</p>
        </div>
        {point.status === "boundary_found" ? (
          <strong className="text-lg text-ink">{formatMoney(point.value_minor)}</strong>
        ) : (
          <strong className="max-w-xs text-right text-sm text-ink">
            {point.status === "entire_domain_feasible"
              ? run.frontier_type === "retirement_age_max_spending"
                ? "最大可承受支出高于或等于搜索上限"
                : "所需金额低于或等于搜索下限"
              : run.frontier_type === "retirement_age_max_spending"
                ? "最大可承受支出低于搜索下限"
                : "所需金额高于搜索上限"}
          </strong>
        )}
      </div>
      <p className="mt-3 rounded-md bg-brand/5 px-3 py-2 text-sm text-ink" data-testid="frontier-point-conclusion">{pointConclusion(point, run)}</p>
      <dl className="mt-3 grid grid-cols-2 gap-2 text-sm sm:grid-cols-4">
        <div><dt className="text-ink-muted">目标 Wilson 下界</dt><dd>{formatPercent(run.config.target_success_probability)}</dd></div>
        <div><dt className="text-ink-muted">点估计</dt><dd>{formatPercent(point.evaluation.success_probability)}</dd></div>
        <div><dt className="text-ink-muted">Wilson 95% 区间</dt><dd>{formatPercent(point.evaluation.success_wilson_low)}–{formatPercent(point.evaluation.success_wilson_high)}</dd></div>
        <div><dt className="text-ink-muted">路径数</dt><dd>{point.evaluation.runs.toLocaleString()}</dd></div>
        <div><dt className="text-ink-muted">配对变化</dt><dd>改善 {point.evaluation.improved_path_count} / 回退 {point.evaluation.regressed_path_count}</dd></div>
      </dl>
      {point.worse_neighbor && (
        <p className="mt-3 rounded bg-surface-muted px-3 py-2 text-xs text-ink-muted">
          相邻更不利点 {formatMoney(point.worse_neighbor.value_minor)}：成功 {point.worse_neighbor.success_count}/{point.worse_neighbor.runs}，Wilson 下界 {formatPercent(point.worse_neighbor.success_wilson_low)}；与边界正好相差一个 step。
        </p>
      )}
      {assetType && point.status === "boundary_found" && (
        <p className="mt-3 text-sm text-ink-muted">
          当前资产 {formatMoney(point.source_current_assets_minor ?? 0)}；{(point.gap_minor ?? 0) > 0 ? "缺口" : "富余"} {formatMoney(Math.abs(point.gap_minor ?? 0))}
          {run.frontier_type === "coast_required_assets" ? `；Coast ${point.coast_achieved ? "已达到" : "尚未达到"}` : ""}
        </p>
      )}
      <p className="mt-2 break-all text-[11px] text-ink-muted">outcome {point.evaluation.outcome_hash}</p>
      {point.applicable && (
        <div className="mt-3"><Button variant="secondary" onClick={() => onPreview(point)} disabled={run.current_plan_changed || Boolean(run.application)}>预览应用</Button></div>
      )}
    </article>
  );
}

function PreviewDialog({ preview, open, pending, error, onClose, onApply }: {
  preview: FrontierPreview | null;
  open: boolean;
  pending: boolean;
  error?: string;
  onClose: () => void;
  onApply: () => void;
}) {
  return (
    <Dialog open={open} onClose={onClose} title="应用 FIRE 达标前沿点" footer={(
      <div className="flex justify-end gap-2"><Button variant="secondary" onClick={onClose}>取消</Button><Button pending={pending} onClick={onApply}>确认应用</Button></div>
    )}>
      {preview && <div className="space-y-3">
        <table className="w-full text-sm"><thead><tr className="text-left text-ink-muted"><th>参数</th><th>应用前</th><th>应用后</th></tr></thead><tbody>
          <tr><td className="py-2">退休年龄</td><td>{preview.before.retirement_age}</td><td>{preview.after.retirement_age}</td></tr>
          <tr><td className="py-2">年度储蓄</td><td>{formatMoney(preview.before.annual_savings_minor)}</td><td>{formatMoney(preview.after.annual_savings_minor)}</td></tr>
          <tr><td className="py-2">年度支出</td><td>{formatMoney(preview.before.annual_spending_minor)}</td><td>{formatMoney(preview.after.annual_spending_minor)}</td></tr>
        </tbody></table>
        <p className="text-sm text-ink-muted">目标 Wilson 下界 {formatPercent(preview.target_probability)}；候选区间 {formatPercent(preview.success_wilson_low)}–{formatPercent(preview.success_wilson_high)}；改善/回退路径 {preview.improved_path_count}/{preview.regressed_path_count}。</p>
        <p className="text-xs text-ink-muted">保持不变：{preview.unchanged.join("、")}。预览有效至 {formatDateTimeFromMs(preview.preview_expires_at)}。</p>
        <Alert variant="warning">应用只更新计划草稿，不会自动运行模拟。</Alert>
        {error && <Alert variant="danger">{error}</Alert>}
      </div>}
    </Dialog>
  );
}

export function FrontierPage({ planId }: { planId: string }) {
  const searchParams = useSearchParams();
  const qc = useQueryClient();
  const requestedRunID = searchParams.get("run_id");
  const requestedSourceID = searchParams.get("simulation_run_id");
  const [frontierType, setFrontierType] = useState<FrontierType>("retirement_age_max_spending");
  const [sourceID, setSourceID] = useState(requestedSourceID ?? "");
  const [targetDraft, setTargetDraft] = useState(String(DEFAULT_TARGET_PERCENT));
  const [evaluationRuns, setEvaluationRuns] = useState(DEFAULT_EVALUATION_RUNS);
  const [ageMin, setAgeMin] = useState<number | null>(null);
  const [ageMax, setAgeMax] = useState<number | null>(null);
  const [search, setSearch] = useState<{ min: number; max: number; step: number } | null>(null);
  const [readiness, setReadiness] = useState<FrontierReadiness | null>(null);
  const [selectedRunID, setSelectedRunID] = useState<string | null>(requestedRunID);
  const [trackedTaskID, setTrackedTaskID] = useState<string | null>(null);
  const [preview, setPreview] = useState<FrontierPreview | null>(null);
  const [previewError, setPreviewError] = useState<string>();
  const [applied, setApplied] = useState(false);

  const planQ = useQuery({ queryKey: ["plan", planId], queryFn: () => getPlan(planId) });
  const paramsQ = useQuery({ queryKey: ["parameters", planId], queryFn: () => getParameters(planId) });
  const simulationsQ = useQuery({ queryKey: ["simulations", planId], queryFn: () => listSimulations(planId) });
  const runsQ = useQuery({ queryKey: ["frontier-runs", planId], queryFn: () => listFrontierRuns(planId) });
  const qualifiedSources = useMemo(() => (simulationsQ.data?.simulations ?? []).filter(
    (run) => run.task_status === "complete" && run.runs >= 1000,
  ), [simulationsQ.data]);

  const activeSummary = runsQ.data?.runs.find((run) => isTaskActive(run.status));
  const effectiveRunID = selectedRunID ?? activeSummary?.id ?? runsQ.data?.runs[0]?.id ?? null;
  const detailQ = useQuery({
    queryKey: ["frontier-run", effectiveRunID], queryFn: () => getFrontierRun(effectiveRunID!),
    enabled: Boolean(effectiveRunID), refetchInterval: (query) => isTaskActive(query.state.data?.status) ? 2000 : false,
  });
  const detailTaskID = detailQ.data && isTaskActive(detailQ.data.status) ? detailQ.data.task_id : null;
  const restore = useActiveTaskRestore({
    workerType: "go_worker", taskType: "fire_frontier", scopeType: "plan", scopeId: planId,
    businessTaskId: activeSummary?.task_id ?? detailTaskID, preferredTaskId: trackedTaskID,
  });
  const taskID = restore.taskId ?? detailTaskID ?? (restore.restoring ? trackedTaskID : null);
  const invalidate = () => {
    setTrackedTaskID(null);
    void qc.invalidateQueries({ queryKey: ["frontier-run"] });
    void qc.invalidateQueries({ queryKey: ["frontier-runs", planId] });
  };
  const taskState = useTaskStatus(taskID, {
    initialTask: restore.task, onComplete: invalidate, onFailed: invalidate, onCanceled: invalidate,
  });

  const effectiveSourceID = sourceID || qualifiedSources[0]?.id || "";
  const selectedSource = qualifiedSources.find((run) => run.id === effectiveSourceID);
  const effectiveEvaluationRuns = Math.min(
    evaluationRuns,
    selectedSource?.runs ?? evaluationRuns,
    20_000,
  );
  const effectiveAgeMin = ageMin ?? paramsQ.data?.parameters.retirement_age ?? 0;
  const effectiveAgeMax = ageMax ?? (paramsQ.data
    ? Math.min(paramsQ.data.parameters.end_age - 1, paramsQ.data.parameters.retirement_age + 10)
    : 0);
  const effectiveSearch = search ?? (paramsQ.data
    ? defaultSearch(frontierType, paramsQ.data.parameters)
    : { min: 1, max: 10_000_00, step: 100_00 });
  const target = Number(targetDraft) / 100;
  const request = (): FrontierRequest => ({
    source_simulation_run_id: effectiveSourceID,
    frontier_type: frontierType,
    target_success_probability: target,
    evaluation_runs: effectiveEvaluationRuns,
    retirement_age_range: isAgeType(frontierType) ? { min: effectiveAgeMin, max: effectiveAgeMax } : null,
    search: { min_minor: effectiveSearch.min, max_minor: effectiveSearch.max, step_minor: effectiveSearch.step },
  });
  const localError = !effectiveSourceID ? "请选择一条已完成且不少于 1000 路径的正式模拟"
    : !Number.isFinite(target) || target < 0.5 || target > 0.99 ? "目标成功率须在 50% 至 99% 之间"
      : effectiveEvaluationRuns < 1000 ? "评估路径数至少为 1000"
        : effectiveSearch.step <= 0 || effectiveSearch.max < effectiveSearch.min ? "金额搜索域或 step 无效"
          : isAgeType(frontierType) && effectiveAgeMax < effectiveAgeMin ? "退休年龄范围无效" : null;

  const readinessM = useMutation({
    mutationFn: () => getFrontierReadiness(planId, request()),
    onSuccess: setReadiness,
  });
  const createM = useMutation({
    mutationFn: async () => {
      const checked = await getFrontierReadiness(planId, request());
      setReadiness(checked);
      if (!checked.ready) throw new Error(checked.issues[0]?.message ?? "当前输入不可运行");
      return createFrontierRun(planId, request());
    },
    onSuccess: (created) => {
      setSelectedRunID(created.run_id); setTrackedTaskID(created.task_id); setApplied(false);
      void qc.invalidateQueries({ queryKey: ["frontier-runs", planId] });
    },
  });
  const previewM = useMutation({
    mutationFn: (point: FrontierPoint) => {
      if (!detailQ.data || !planQ.data) throw new Error("计划版本不可用");
      return previewFrontierPoint(detailQ.data.id, point.id, planQ.data.config_version);
    },
    onSuccess: (value) => { setPreview(value); setPreviewError(undefined); },
  });
  const applyM = useMutation({
    mutationFn: () => preview ? applyFrontierPoint(preview) : Promise.reject(new Error("预览不可用")),
    onSuccess: () => {
      setPreview(null); setApplied(true);
      for (const key of ["plan", "parameters", "dashboard", "simulations", "frontier-run", "frontier-runs"]) {
        void qc.invalidateQueries({ queryKey: [key] });
      }
    },
    onError: (error) => setPreviewError(queryErrorMessage(error)),
  });

  const baseError = planQ.error ?? paramsQ.error ?? simulationsQ.error ?? runsQ.error;
  if ((planQ.isPending || paramsQ.isPending || simulationsQ.isPending) && !paramsQ.data) {
    return <PageSkeleton label="加载 FIRE 达标前沿…" />;
  }
  if (baseError && (!planQ.data || !paramsQ.data || !simulationsQ.data)) {
    return <ErrorState message="无法加载 FIRE 达标前沿。" technicalDetail={queryErrorMessage(baseError)} onRetry={() => { void planQ.refetch(); void paramsQ.refetch(); void simulationsQ.refetch(); }} backHref={`/plans/${planId}/settings`} />;
  }
  if (!planQ.data || !paramsQ.data || !simulationsQ.data) return null;

  const activeRun = detailQ.data;
  const result = activeRun?.result;
  const progress = taskState.task?.progress_total
    ? taskState.task.progress_current / taskState.task.progress_total
    : activeRun?.progress_total ? activeRun.progress_current / activeRun.progress_total : 0;
  const mutationError = readinessM.error ?? createM.error;

  return (
    <div className="space-y-6 pb-10">
      <PageHeader
        eyebrow="FIRE · 离散压力边界"
        title="FIRE 达标前沿"
        description="在同一套冻结模型、seed 和路径下，寻找刚好达到目标 Wilson 95% 下界的离散边界。"
        backHref={`/plans/${planId}/settings?section=simulation`}
        backLabel="返回模拟设置"
      />

      <section aria-labelledby="frontier-type-heading">
        <h2 id="frontier-type-heading" className="mb-3 text-lg font-semibold text-ink">1. 选择要回答的问题</h2>
        <div className="grid gap-3 md:grid-cols-2">
          {TYPE_CARDS.map((card) => (
            <div key={card.type} className={`relative rounded-lg border ${frontierType === card.type ? "border-brand bg-brand/5" : "border-line bg-surface hover:border-brand/50"}`}>
              <button type="button" aria-label={`选择「${card.title}」`} onClick={() => { setFrontierType(card.type); setSearch(defaultSearch(card.type, paramsQ.data!.parameters)); setReadiness(null); }} aria-pressed={frontierType === card.type}
                className="block w-full rounded-lg p-4 pr-12 text-left focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus">
                <span className="font-semibold text-ink">{card.title}</span>
                <span className="mt-1 block text-sm text-ink-muted">{card.description}</span>
              </button>
              <QuestionHelp question={card} />
            </div>
          ))}
        </div>
      </section>

      <section className="space-y-4 rounded-lg border border-line bg-surface p-5" aria-labelledby="frontier-input-heading">
        <h2 id="frontier-input-heading" className="text-lg font-semibold text-ink">2. 冻结来源与离散搜索域</h2>
        <div className="grid gap-4 md:grid-cols-3">
          <label className="text-sm font-medium text-ink">来源正式模拟
            <select value={effectiveSourceID} onChange={(event) => { setSourceID(event.target.value); setReadiness(null); }} className="input-base mt-1 w-full">
              <option value="">请选择</option>
              {qualifiedSources.map((run) => <option key={run.id} value={run.id}>{formatDateTimeFromMs(run.created_at)} · {run.runs.toLocaleString()} 路径</option>)}
            </select>
          </label>
          <label className="text-sm font-medium text-ink">目标成功率（%）
            <input className="input-base mt-1 w-full" inputMode="decimal" value={targetDraft} onChange={(event) => { setTargetDraft(event.target.value); setReadiness(null); }} />
          </label>
          <label className="text-sm font-medium text-ink">评估路径数
            <input className="input-base mt-1 w-full" type="number" min={1000} max={Math.min(20000, selectedSource?.runs ?? 20000)} value={effectiveEvaluationRuns}
              onChange={(event) => { setEvaluationRuns(Number(event.target.value)); setReadiness(null); }} />
          </label>
        </div>
        {isAgeType(frontierType) && <div className="grid gap-4 sm:grid-cols-2">
          <label className="text-sm font-medium text-ink">最小退休年龄<input className="input-base mt-1 w-full" type="number" value={effectiveAgeMin} onChange={(event) => { setAgeMin(Number(event.target.value)); setReadiness(null); }} /></label>
          <label className="text-sm font-medium text-ink">最大退休年龄<input className="input-base mt-1 w-full" type="number" value={effectiveAgeMax} onChange={(event) => { setAgeMax(Number(event.target.value)); setReadiness(null); }} /></label>
        </div>}
        <div className="grid gap-4 md:grid-cols-3">
          <MoneyInput plain label="搜索下限" valueMinor={effectiveSearch.min} onChange={(min) => { setSearch({ ...effectiveSearch, min }); setReadiness(null); }} />
          <MoneyInput plain label="搜索上限" valueMinor={effectiveSearch.max} onChange={(max) => { setSearch({ ...effectiveSearch, max }); setReadiness(null); }} />
          <MoneyInput plain label="搜索精度（step）" valueMinor={effectiveSearch.step} onChange={(step) => { setSearch({ ...effectiveSearch, step }); setReadiness(null); }} />
        </div>
        {readiness && <div className={`rounded-md border p-3 text-sm ${readiness.ready ? "border-success/30 bg-success/5" : "border-danger/30 bg-danger/5"}`}>
          {readiness.ready && readiness.config ? (
            <p>路径 {readiness.config.evaluation_runs.toLocaleString()} · step {formatMoney(readiness.config.search.step_minor)} · 金额档位 {readiness.money_levels} · 年龄点 {readiness.age_points} · 最多评估 {readiness.evaluation_budget} 次 · 最坏成本 {readiness.path_month_budget.toLocaleString()} path-months</p>
          ) : readiness.issues.map((issue) => <p key={issue.code}>{issue.message}</p>)}
          {!readiness.ready && <p className="mt-1 text-xs">可缩小年龄范围或增大 step 后重试。</p>}
        </div>}
        {localError && <Alert variant="warning">{localError}</Alert>}
        {mutationError && <Alert variant="danger">{queryErrorMessage(mutationError)}</Alert>}
        <div className="flex flex-wrap gap-2">
          <Button variant="secondary" pending={readinessM.isPending} disabled={Boolean(localError)} onClick={() => readinessM.mutate()}>检查可运行性</Button>
          <Button pending={createM.isPending} disabled={Boolean(localError) || readiness?.ready === false || Boolean(taskID)} onClick={() => createM.mutate()}>开始计算前沿</Button>
        </div>
      </section>

      {(taskID || isTaskActive(activeRun?.status)) && <section className="rounded-lg border border-line bg-surface p-5" aria-live="polite">
        <div className="flex flex-wrap items-center justify-between gap-3"><div><h2 className="font-semibold text-ink">正在计算</h2><p className="text-sm text-ink-muted">{PHASE_LABEL[taskState.task?.phase ?? activeRun?.phase ?? ""] ?? "等待 Worker"} · {Math.round(progress * 100)}%</p></div><TaskCancelButton task={taskState.task} onCanceled={() => taskState.refetch().then(() => undefined)} /></div>
        <div className="mt-3 h-2 overflow-hidden rounded bg-surface-muted"><div className="h-full bg-brand transition-[width]" style={{ width: `${Math.min(100, progress * 100)}%` }} /></div>
      </section>}

      {activeRun?.status === "canceled" && <Alert variant="warning">任务已取消，没有保存或显示部分前沿。</Alert>}
      {activeRun?.status === "failed" && <Alert variant="danger">计算失败：{activeRun.error_message || activeRun.error_code}</Alert>}
      {applied && <Alert variant="success">前沿点已应用到计划草稿；系统没有自动运行模拟。</Alert>}

      {activeRun?.status === "complete" && result && <section className="space-y-4" aria-labelledby="frontier-result-heading">
        <div className="flex flex-wrap items-start justify-between gap-3"><div><h2 id="frontier-result-heading" className="text-lg font-semibold text-ink">3. 冻结结果</h2><p className="mt-1 text-sm text-ink-muted">来源 {activeRun.source_simulation_run_id} · engine {activeRun.source_engine_version} · algorithm {activeRun.algorithm_version} · 目标 {formatPercent(result.target_probability)} · {result.evaluation_runs.toLocaleString()} 路径</p></div><div className="flex gap-2"><Button variant="secondary" onClick={() => download(`${activeRun.id}.json`, "application/json", JSON.stringify(result, null, 2))}>导出 JSON</Button><Button variant="secondary" onClick={() => download(`${activeRun.id}.csv`, "text/csv;charset=utf-8", csv(result))}>导出 CSV</Button></div></div>
        <div className="flex flex-wrap gap-2 text-xs"><span className={`rounded-full px-2 py-1 ${activeRun.source_available ? "bg-success/10 text-success" : "bg-surface-muted text-ink-muted"}`}>{activeRun.source_available ? "源模拟仍存在" : "源模拟已清理"}</span><span className={`rounded-full px-2 py-1 ${activeRun.current_plan_changed ? "bg-warning/10 text-warning" : "bg-success/10 text-success"}`}>{activeRun.current_plan_changed ? "当前计划已变化" : "当前计划未变化"}</span>{activeRun.application && <span className="rounded-full bg-brand/10 px-2 py-1 text-brand">已应用</span>}</div>
        <CalculationBasis run={activeRun} />
        <div className="rounded-lg border border-line bg-surface p-4"><h3 className="font-semibold text-ink">同口径重算基线</h3><p className="mt-1 text-sm text-ink-muted">成功 {result.baseline.success_count}/{result.baseline.runs}，点估计 {formatPercent(result.baseline.success_probability)}，Wilson 95% 区间 {formatPercent(result.baseline.success_wilson_low)}–{formatPercent(result.baseline.success_wilson_high)}。实际执行 {result.distinct_evaluations}/{result.evaluation_budget} 次评估，{result.actual_path_months.toLocaleString()} path-months。</p></div>
        {isAgeType(activeRun.frontier_type) && <FrontierChart result={result} />}
        <div className="grid gap-3">{result.points.map((point) => <PointEvidence key={point.id} point={point} run={activeRun} onPreview={(value) => previewM.mutate(value)} />)}</div>
        <Alert variant="info">达标前沿是给定模型与输入下的压力边界；Wilson 区间只反映有限模拟路径的抽样误差，不代表真实未来有同等置信保证。</Alert>
      </section>}

      {runsQ.data && runsQ.data.runs.length > 0 && <section className="space-y-2 border-t border-line pt-5"><h2 className="text-lg font-semibold text-ink">历史前沿</h2><div className="grid gap-2">{runsQ.data.runs.map((run) => <button key={run.id} type="button" onClick={() => setSelectedRunID(run.id)} className="flex flex-wrap justify-between gap-2 rounded border border-line bg-surface px-3 py-2 text-left text-sm hover:border-brand/50"><span>{TYPE_LABEL[run.frontier_type]} · {formatPercent(run.target_probability)}</span><span className="text-ink-muted">{run.status} · {formatDateTimeFromMs(run.created_at)}</span></button>)}</div></section>}

      <PreviewDialog preview={preview} open={Boolean(preview)} pending={applyM.isPending} error={previewError} onClose={() => setPreview(null)} onApply={() => applyM.mutate()} />
      {previewM.isError && <Alert variant="danger">{queryErrorMessage(previewM.error)}</Alert>}
    </div>
  );
}
