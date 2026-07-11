"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import { useRouter } from "next/navigation";
import { Button } from "@/components/ui/Button";
import { QuickFireForm, validateQuickFireInput } from "@/components/quick-fire/QuickFireForm";
import { QuickFireResults } from "@/components/quick-fire/QuickFireResults";
import { QuickFireSummary } from "@/components/quick-fire/QuickFireSummary";
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
  const [calculation, setCalculation] = useState<{ inputKey: string; result: QuickFireResult } | null>(null);
  const [requestError, setRequestError] = useState<{ inputKey: string; message: string } | null>(null);
  const [loadingInputKey, setLoadingInputKey] = useState<string | null>(null);
  const [retryToken, setRetryToken] = useState(0);
  const requestSequence = useRef(0);
  const localErrors = useMemo(() => validateQuickFireInput(input), [input]);
  const valid = Object.keys(localErrors).length === 0;
  const currentInputKey = useMemo(() => JSON.stringify(input), [input]);
  const resultIsCurrent = calculation?.inputKey === currentInputKey;
  const errorIsCurrent = requestError?.inputKey === currentInputKey;
  const loadingCurrent = loadingInputKey === currentInputKey;

  useEffect(() => {
    saveQuickFireDraft(typeof window === "undefined" ? null : window.localStorage, input);
  }, [input]);

  useEffect(() => {
    const sequence = ++requestSequence.current;
    if (!valid) return;
    const controller = new AbortController();
    const requestInput = input;
    const requestInputKey = currentInputKey;
    const timer = window.setTimeout(() => {
      setLoadingInputKey(requestInputKey);
      void calculateQuickFire(requestInput, { signal: controller.signal })
        .then((next) => {
          if (sequence === requestSequence.current) {
            setCalculation({ inputKey: requestInputKey, result: next });
            setRequestError(null);
          }
        })
        .catch((err: unknown) => {
          if (controller.signal.aborted || (err instanceof ApiError && err.code === "request_aborted")) return;
          if (sequence === requestSequence.current) {
            setRequestError({
              inputKey: requestInputKey,
              message: err instanceof ApiError ? err.message : "计算失败，请重试。",
            });
          }
        })
        .finally(() => {
          if (sequence === requestSequence.current) setLoadingInputKey(null);
        });
    }, 300);
    return () => {
      window.clearTimeout(timer);
      controller.abort();
    };
  }, [currentInputKey, input, retryToken, valid]);

  const update = <K extends keyof QuickFireInput>(key: K, value: QuickFireInput[K]) => {
    if (Object.is(input[key], value)) return;
    setLoadingInputKey(null);
    setInput((previous) => ({ ...previous, [key]: value }));
  };
  const reset = () => {
    clearQuickFireDraft(window.localStorage);
    setLoadingInputKey(null);
    setInput(QUICK_FIRE_DEFAULTS);
  };
  const createPlan = () => {
    saveQuickFireTransfer(window.sessionStorage, input);
    router.push("/plans/new?source=quick-fire");
  };

  const status = !valid
    ? "invalid"
    : errorIsCurrent
      ? "error"
      : resultIsCurrent
        ? "current"
        : loadingCurrent
          ? "loading"
          : "debounce";
  const result = calculation?.result ?? null;

  return (
    <div className="content-enter">
      {valid && result && (
        <div className="mb-4 min-h-6 md:hidden">
          <QuickFireSummary result={result} compact />
          {!resultIsCurrent && <p className="mt-1 text-xs text-ink-muted">参数更新中，以上为上次计算结果。</p>}
        </div>
      )}
      <div className="grid min-w-0 gap-8 xl:grid-cols-[minmax(280px,0.8fr)_minmax(0,1.7fr)]">
        <div className="min-w-0 space-y-5">
          <QuickFireForm input={input} errors={localErrors} onChange={update} />
          <div className="flex flex-wrap gap-2"><Button variant="secondary" onClick={reset}>重置</Button><Button onClick={createPlan} disabled={!valid}>创建完整计划</Button></div>
        </div>
        <div className="min-w-0 space-y-8">
          <div className="flex min-h-10 items-center text-sm" data-testid="quick-fire-request-status">
            {status === "invalid" && <p role="status" className="text-danger">请先修正输入参数，结果将在参数合法后更新。</p>}
            {status === "debounce" && <p role="status" className="text-ink-muted">参数已更新，等待计算…</p>}
            {status === "loading" && <p role="status" className="text-ink-muted">正在计算{result ? "，暂显示上次结果" : ""}…</p>}
            {status === "error" && (
              <div role="alert" className="flex flex-wrap items-center gap-2 text-danger">
                <span>{requestError?.message}{result ? "，当前显示上次计算结果。" : ""}</span>
                <Button variant="secondary" onClick={() => { setRequestError(null); setRetryToken((value) => value + 1); }}>重试</Button>
              </div>
            )}
          </div>
          {result ? (
            <QuickFireResults result={result} concealed={!valid} busy={!resultIsCurrent && valid} />
          ) : (
            <div className="min-h-[620px] border-y border-line" aria-hidden="true" data-testid="quick-fire-result-placeholder" />
          )}
        </div>
      </div>
    </div>
  );
}
