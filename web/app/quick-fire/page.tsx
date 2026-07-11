"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/Button";
import { ErrorState } from "@/components/ui/ErrorState";
import { QuickFireChart } from "@/components/quick-fire/QuickFireChart";
import { QuickFireForm, validateQuickFireInput } from "@/components/quick-fire/QuickFireForm";
import { QuickFireSummary } from "@/components/quick-fire/QuickFireSummary";
import { QuickFireYearTable } from "@/components/quick-fire/QuickFireYearTable";
import { ApiError } from "@/lib/api/client";
import { calculateQuickFire, type QuickFireInput, type QuickFireResult } from "@/lib/api/quick-fire";
import {
  QUICK_FIRE_DEFAULTS,
  clearQuickFireDraft,
  loadQuickFireDraft,
  saveQuickFireDraft,
  saveQuickFireTransfer,
} from "@/lib/quick-fire-draft";

export default function QuickFirePage() {
  const router = useRouter();
  const [input, setInput] = useState<QuickFireInput>(() => loadQuickFireDraft(typeof window === "undefined" ? null : window.localStorage));
  const [result, setResult] = useState<QuickFireResult | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [loading, setLoading] = useState(false);
  const requestSequence = useRef(0);
  const localErrors = useMemo(() => validateQuickFireInput(input), [input]);
  const valid = Object.keys(localErrors).length === 0;

  useEffect(() => {
    saveQuickFireDraft(typeof window === "undefined" ? null : window.localStorage, input);
  }, [input]);

  useEffect(() => {
    if (!valid) {
      return;
    }
    const controller = new AbortController();
    const sequence = ++requestSequence.current;
    const timer = window.setTimeout(() => {
      setLoading(true);
      void calculateQuickFire(input, { signal: controller.signal })
        .then((next) => {
          if (sequence === requestSequence.current) setResult(next);
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted || (err instanceof ApiError && err.code === "request_aborted")) return;
          if (sequence === requestSequence.current) {
            setResult(null);
            setError(err instanceof ApiError ? err.message : "计算失败，请重试。");
          }
        })
        .finally(() => {
          if (sequence === requestSequence.current) setLoading(false);
        });
    }, 300);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [input, valid]);

  const update = <K extends keyof QuickFireInput>(key: K, value: QuickFireInput[K]) => {
    setResult(null);
    setError(null);
    setInput((previous) => ({ ...previous, [key]: value }));
  };
  const reset = () => {
    clearQuickFireDraft(window.localStorage);
    setResult(null);
    setError(null);
    setInput(QUICK_FIRE_DEFAULTS);
  };
  const createPlan = () => {
    saveQuickFireTransfer(window.sessionStorage, input);
    router.push("/plans/new?source=quick-fire");
  };

  return (
    <div className="content-enter">
      {valid && result && <div className="mb-4 md:hidden"><QuickFireSummary result={result} compact /></div>}
      <div className="grid gap-8 xl:grid-cols-[minmax(280px,0.8fr)_minmax(0,1.7fr)]">
        <div className="space-y-5">
          <QuickFireForm input={input} errors={localErrors} onChange={update} />
          <div className="flex flex-wrap gap-2"><Button variant="secondary" onClick={reset}>重置</Button><Button onClick={createPlan} disabled={!valid}>创建完整计划</Button></div>
        </div>
        <div className="space-y-8">
          {valid && loading && <p role="status" className="text-sm text-ink-muted">正在计算…</p>}
          {!valid && <p role="status" className="text-sm text-danger">请先修正输入参数。</p>}
          {valid && error && <ErrorState message={error} onRetry={() => { setError(null); setInput((previous) => ({ ...previous })); }} />}
          {valid && result && !loading && !error && <>
            <QuickFireSummary result={result} />
            <section aria-labelledby="quick-fire-chart-title"><h2 id="quick-fire-chart-title" className="text-lg font-medium text-ink">资产与所需资本</h2><div className="mt-3 border-y border-line py-3"><QuickFireChart years={result.years} /></div></section>
            <QuickFireYearTable years={result.years} />
            <details className="border-t border-line pt-4">
              <summary className="cursor-pointer text-sm font-medium text-ink">了解完整模拟差异</summary>
              <p className="mt-2 text-sm leading-6 text-ink-muted">完整计划会根据持仓、收益波动、资产相关性和通胀路径运行 Monte Carlo，用概率结果评估收益顺序风险。</p>
            </details>
          </>}
        </div>
      </div>
    </div>
  );
}
