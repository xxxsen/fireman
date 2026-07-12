"use client";

import { useMemo, useState } from "react";
import { useMutation, useQuery } from "@tanstack/react-query";
import {
  listResearchAssets,
  updateCollection,
  type ResearchCollectionDetail,
  type ResearchCollectionUpdate,
  type ResearchRebalancePolicy,
  type ResearchStartPolicy,
} from "@/lib/api/research";
import { queryErrorMessage } from "@/lib/query-error";
import { Button } from "@/components/ui/Button";
import { MetricHelp } from "@/components/ui/MetricHelp";

export const REBALANCE_POLICY_LABELS: Record<ResearchRebalancePolicy, string> = {
  monthly: "月度再平衡",
  quarterly: "季度再平衡",
  yearly: "年度再平衡",
  buy_hold: "买入并持有",
  fixed: "固定权重（逐期）",
  threshold: "阈值再平衡",
};

function labelCls() {
  return "mb-1 block text-xs font-medium text-ink-muted";
}

function inputCls() {
  return "w-full rounded-md border border-line bg-surface px-2.5 py-1.5 text-sm text-ink focus:border-brand focus:outline-none";
}

/** Inline single-select asset search used for the benchmark field. */
function BenchmarkPicker({
  value,
  onChange,
}: {
  value: string;
  onChange: (assetKey: string) => void;
}) {
  const [query, setQuery] = useState("");
  const [open, setOpen] = useState(false);

  const searchQuery = useQuery({
    queryKey: ["research", "benchmark-search", query],
    queryFn: () => listResearchAssets({ q: query, limit: 8 }),
    enabled: open && query.trim().length > 0,
  });

  return (
    <div className="relative">
      {value ? (
        <span className="flex items-center gap-2">
          <code className="flex-1 truncate rounded bg-surface-muted px-2 py-1.5 text-xs">{value}</code>
          <button
            type="button"
            onClick={() => onChange("")}
            className="text-xs text-ink-muted hover:text-danger"
            data-testid="benchmark-clear"
          >
            清除
          </button>
        </span>
      ) : (
        <input
          type="search"
          value={query}
          onChange={(e) => {
            setQuery(e.target.value);
            setOpen(true);
          }}
          onFocus={() => setOpen(true)}
          onBlur={() => setTimeout(() => setOpen(false), 150)}
          placeholder="搜索基准资产（可选）…"
          className={inputCls()}
          data-testid="benchmark-search"
        />
      )}
      {open && query.trim() && (
        <div className="absolute z-10 mt-1 max-h-56 w-full overflow-y-auto rounded-md border border-line bg-surface shadow-md">
          {searchQuery.isLoading ? (
            <p className="px-3 py-2 text-xs text-ink-muted">搜索中…</p>
          ) : (searchQuery.data?.assets ?? []).length === 0 ? (
            <p className="px-3 py-2 text-xs text-ink-muted">无匹配资产</p>
          ) : (
            (searchQuery.data?.assets ?? []).map((a) => (
              <button
                key={a.asset_key}
                type="button"
                onMouseDown={(e) => e.preventDefault()}
                onClick={() => {
                  onChange(a.asset_key);
                  setQuery("");
                  setOpen(false);
                }}
                className="block w-full px-3 py-1.5 text-left text-sm hover:bg-surface-muted"
              >
                <span className="font-medium text-ink">{a.name}</span>
                <span className="ml-2 text-xs text-ink-muted">
                  {a.symbol} · {a.currency}
                </span>
              </button>
            ))
          )}
        </div>
      )}
    </div>
  );
}

export interface CollectionParamsFormProps {
  detail: ResearchCollectionDetail;
  onSaved: (detail: ResearchCollectionDetail) => void;
}

export function CollectionParamsForm({ detail, onSaved }: CollectionParamsFormProps) {
  const [name, setName] = useState(detail.name);
  const [description, setDescription] = useState(detail.description);
  const [tags, setTags] = useState(detail.tags.join(", "));
  const [baseCurrency, setBaseCurrency] = useState(detail.base_currency);
  const [initialAmount, setInitialAmount] = useState(String(detail.initial_amount_minor / 100));
  const [rebalancePolicy, setRebalancePolicy] = useState<ResearchRebalancePolicy>(
    detail.rebalance_policy,
  );
  const [threshold, setThreshold] = useState(String(detail.rebalance_threshold * 100));
  const [startPolicy, setStartPolicy] = useState<ResearchStartPolicy>(detail.start_policy);
  const [windowStart, setWindowStart] = useState(detail.window_start);
  const [windowEnd, setWindowEnd] = useState(detail.window_end);
  const [benchmark, setBenchmark] = useState(detail.benchmark_asset_key ?? "");
  const [riskFree, setRiskFree] = useState(String(detail.risk_free_rate * 100));
  const [txCost, setTxCost] = useState(String(detail.transaction_cost_rate * 100));
  const [tailConfidence, setTailConfidence] = useState(detail.tail_risk_confidence);
  const [tailHorizon, setTailHorizon] = useState(detail.tail_risk_horizon_days);
  const [showAdvanced, setShowAdvanced] = useState(detail.transaction_cost_rate > 0);

  const dirty = useMemo(() => {
    return (
      name !== detail.name ||
      description !== detail.description ||
      tags !== detail.tags.join(", ") ||
      baseCurrency !== detail.base_currency ||
      Number(initialAmount) * 100 !== detail.initial_amount_minor ||
      rebalancePolicy !== detail.rebalance_policy ||
      Number(threshold) / 100 !== detail.rebalance_threshold ||
      startPolicy !== detail.start_policy ||
      windowStart !== detail.window_start ||
      windowEnd !== detail.window_end ||
      benchmark !== (detail.benchmark_asset_key ?? "") ||
      Number(riskFree) / 100 !== detail.risk_free_rate ||
      Number(txCost) / 100 !== detail.transaction_cost_rate ||
      tailConfidence !== detail.tail_risk_confidence ||
      tailHorizon !== detail.tail_risk_horizon_days
    );
  }, [
    detail, name, description, tags, baseCurrency, initialAmount, rebalancePolicy,
    threshold, startPolicy, windowStart, windowEnd, benchmark, riskFree, txCost,
    tailConfidence, tailHorizon,
  ]);

  const saveMutation = useMutation({
    mutationFn: () => {
      const body: ResearchCollectionUpdate = {
        name: name.trim(),
        description,
        base_currency: baseCurrency,
        rebalance_policy: rebalancePolicy,
        start_policy: startPolicy,
        window_start: startPolicy === "custom_range" ? windowStart : "",
        window_end: startPolicy === "custom_range" ? windowEnd : "",
        benchmark_asset_key: benchmark,
        tags: tags
          .split(/[,，]/)
          .map((t) => t.trim())
          .filter(Boolean),
        tail_risk_confidence: tailConfidence,
        tail_risk_horizon_days: tailHorizon,
      };
      const amount = Number(initialAmount);
      if (Number.isFinite(amount) && amount > 0) {
        body.initial_amount_minor = Math.round(amount * 100);
      }
      const thresholdVal = Number(threshold);
      if (Number.isFinite(thresholdVal)) {
        body.rebalance_threshold = thresholdVal / 100;
      }
      const riskFreeVal = Number(riskFree);
      if (Number.isFinite(riskFreeVal)) {
        body.risk_free_rate = riskFreeVal / 100;
      }
      const txCostVal = Number(txCost);
      if (Number.isFinite(txCostVal)) {
        body.transaction_cost_rate = txCostVal / 100;
      }
      return updateCollection(detail.id, body);
    },
    onSuccess: onSaved,
  });

  return (
    <section
      className="rounded-lg border border-line bg-surface p-4"
      data-testid="collection-params"
    >
      <div className="mb-3 flex items-center justify-between">
        <h2 className="text-base font-semibold text-ink">基础参数</h2>
        <Button
          disabled={!dirty || !name.trim()}
          pending={saveMutation.isPending}
          onClick={() => saveMutation.mutate()}
          data-testid="save-params"
        >
          保存参数
        </Button>
      </div>

      {saveMutation.isError && (
        <p className="mb-3 text-sm text-danger" role="alert">
          保存失败：{queryErrorMessage(saveMutation.error)}
        </p>
      )}

      <div className="grid gap-3 sm:grid-cols-2 lg:grid-cols-3">
        <label className="block sm:col-span-2 lg:col-span-1">
          <span className={labelCls()}>名称</span>
          <input
            type="text"
            value={name}
            onChange={(e) => setName(e.target.value)}
            className={inputCls()}
            data-testid="param-name"
          />
        </label>

        <label className="block">
          <span className={labelCls()}>基准币种</span>
          <select
            value={baseCurrency}
            onChange={(e) => setBaseCurrency(e.target.value)}
            className={inputCls()}
            data-testid="param-currency"
          >
            <option value="CNY">CNY</option>
            <option value="USD">USD</option>
            <option value="HKD">HKD</option>
          </select>
        </label>

        <div className="sm:col-span-2 lg:col-span-3">
          <p className="mb-2 text-xs font-medium text-ink-muted">尾部风险口径</p>
          <div className="grid gap-3 sm:grid-cols-2">
            <div>
              <span className="mb-1 flex items-center text-xs text-ink-muted">
                置信度
                <MetricHelp text="95% CVaR 表示在历史上最差 5% 的持有期场景中，平均损失是多少。" />
              </span>
              <div className="inline-flex overflow-hidden rounded-md border border-line" data-testid="tail-confidence-control">
                {([0.9, 0.95, 0.99] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={tailConfidence === value}
                    onClick={() => setTailConfidence(value)}
                    className={`min-h-9 px-3 text-sm ${tailConfidence === value ? "bg-brand text-white" : "bg-surface text-ink hover:bg-surface-muted"}`}
                  >
                    {value * 100}%
                  </button>
                ))}
              </div>
            </div>
            <div>
              <span className="mb-1 flex items-center text-xs text-ink-muted">
                持有期
                <MetricHelp text="20 日按回测有效收益日滚动复合，相邻场景会共享部分交易日。" />
              </span>
              <div className="inline-flex overflow-hidden rounded-md border border-line" data-testid="tail-horizon-control">
                {([1, 20] as const).map((value) => (
                  <button
                    key={value}
                    type="button"
                    aria-pressed={tailHorizon === value}
                    onClick={() => setTailHorizon(value)}
                    className={`min-h-9 px-3 text-sm ${tailHorizon === value ? "bg-brand text-white" : "bg-surface text-ink hover:bg-surface-muted"}`}
                  >
                    {value === 1 ? "1 日" : "20 个有效交易日"}
                  </button>
                ))}
              </div>
            </div>
          </div>
        </div>

        <label className="block">
          <span className={labelCls()}>初始资金（{baseCurrency}）</span>
          <input
            type="number"
            value={initialAmount}
            onChange={(e) => setInitialAmount(e.target.value)}
            className={inputCls()}
            data-testid="param-initial-amount"
          />
        </label>

        <label className="block">
          <span className={labelCls()}>再平衡规则</span>
          <select
            value={rebalancePolicy}
            onChange={(e) => setRebalancePolicy(e.target.value as ResearchRebalancePolicy)}
            className={inputCls()}
            data-testid="param-rebalance"
          >
            {Object.entries(REBALANCE_POLICY_LABELS).map(([value, label]) => (
              <option key={value} value={value}>
                {label}
              </option>
            ))}
          </select>
        </label>

        {rebalancePolicy === "threshold" && (
          <label className="block">
            <span className={labelCls()}>再平衡阈值（%）</span>
            <input
              type="number"
              value={threshold}
              onChange={(e) => setThreshold(e.target.value)}
              placeholder="如 5"
              className={inputCls()}
              data-testid="param-threshold"
            />
          </label>
        )}

        <label className="block">
          <span className={labelCls()}>历史区间策略</span>
          <select
            value={startPolicy}
            onChange={(e) => setStartPolicy(e.target.value as ResearchStartPolicy)}
            className={inputCls()}
            data-testid="param-start-policy"
          >
            <option value="common_intersection">共同可用区间（默认）</option>
            <option value="custom_range">指定起止日期</option>
          </select>
        </label>

        {startPolicy === "custom_range" && (
          <>
            <label className="block">
              <span className={labelCls()}>开始日期</span>
              <input
                type="date"
                value={windowStart}
                onChange={(e) => setWindowStart(e.target.value)}
                className={inputCls()}
                data-testid="param-window-start"
              />
            </label>
            <label className="block">
              <span className={labelCls()}>结束日期</span>
              <input
                type="date"
                value={windowEnd}
                onChange={(e) => setWindowEnd(e.target.value)}
                className={inputCls()}
                data-testid="param-window-end"
              />
            </label>
          </>
        )}

        <label className="block">
          <span className={labelCls()}>无风险利率（%/年）</span>
          <input
            type="number"
            value={riskFree}
            onChange={(e) => setRiskFree(e.target.value)}
            className={inputCls()}
            data-testid="param-risk-free"
          />
        </label>

        <div className="block">
          <span className={labelCls()}>基准资产（可选）</span>
          <BenchmarkPicker value={benchmark} onChange={setBenchmark} />
        </div>

        <label className="block sm:col-span-2 lg:col-span-1">
          <span className={labelCls()}>标签（逗号分隔）</span>
          <input
            type="text"
            value={tags}
            onChange={(e) => setTags(e.target.value)}
            placeholder="如 宽基, 美元资产"
            className={inputCls()}
          />
        </label>

        <label className="block sm:col-span-2 lg:col-span-3">
          <span className={labelCls()}>描述</span>
          <textarea
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            rows={2}
            className={inputCls()}
          />
        </label>
      </div>

      <div className="mt-3 border-t border-line pt-3">
        <button
          type="button"
          onClick={() => setShowAdvanced(!showAdvanced)}
          className="text-xs text-ink-muted underline-offset-2 hover:text-ink hover:underline"
          data-testid="toggle-advanced"
        >
          {showAdvanced ? "收起高级设置" : "高级设置"}
        </button>
        {showAdvanced && (
          <div className="mt-2 grid gap-3 sm:grid-cols-3">
            <label className="block">
              <span className={labelCls()}>交易费率（%，预留暂不参与计算）</span>
              <input
                type="number"
                value={txCost}
                onChange={(e) => setTxCost(e.target.value)}
                className={inputCls()}
                data-testid="param-tx-cost"
              />
            </label>
          </div>
        )}
      </div>
    </section>
  );
}
