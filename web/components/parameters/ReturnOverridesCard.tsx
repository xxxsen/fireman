"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { Button } from "@/components/ui/Button";
import { getHoldings } from "@/lib/api/holdings";
import {
  deleteReturnOverride,
  getReturnOverrides,
  setReturnOverride,
} from "@/lib/api/simulations";
import { decimalToPercentString, formatPercent, percentToDecimal } from "@/lib/percent";
import { queryErrorMessage } from "@/lib/query-error";
import type { ReturnOverride } from "@/types/api";

interface Draft {
  instrumentId: string;
  overrideReturn: boolean;
  forwardReturnPct: string;
  overrideVol: boolean;
  volPct: string;
  reason: string;
  expiresAt: string;
}

const emptyDraft: Draft = {
  instrumentId: "",
  overrideReturn: true,
  forwardReturnPct: "",
  overrideVol: false,
  volPct: "",
  reason: "",
  expiresAt: "",
};

/**
 * ReturnOverridesCard manages asset-level return overrides: a rare,
 * plan-specific escape hatch that pins a held instrument's forward geometric
 * return and/or volatility. It saves through its own per-instrument API
 * (independent of the parameters form) and shows the required reason + expiry.
 */
export function ReturnOverridesCard({ planId }: { planId: string }) {
  const qc = useQueryClient();
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<Draft>(emptyDraft);
  const [formError, setFormError] = useState<string | null>(null);

  const holdingsQ = useQuery({
    queryKey: ["holdings", planId],
    queryFn: () => getHoldings(planId),
    enabled: open,
  });
  const overridesQ = useQuery({
    queryKey: ["return-overrides", planId],
    queryFn: () => getReturnOverrides(planId),
    enabled: open,
  });

  const riskHoldings = useMemo(
    () =>
      (holdingsQ.data?.holdings ?? []).filter(
        (h) => h.enabled && h.asset_class !== "cash",
      ),
    [holdingsQ.data],
  );
  const overrides = overridesQ.data?.overrides ?? [];
  const nameOf = (instrumentId: string) => {
    const h = riskHoldings.find((x) => x.instrument_id === instrumentId);
    return h?.instrument_name || h?.instrument_code || instrumentId;
  };

  const invalidate = () => {
    void qc.invalidateQueries({ queryKey: ["return-overrides", planId] });
    // The staleness banner derives from the plan's runs query, so refresh it too.
    void qc.invalidateQueries({ queryKey: ["simulations", planId] });
  };

  const saveMut = useMutation({
    mutationFn: () => {
      const body = {
        forward_return: draft.overrideReturn
          ? percentToDecimal(draft.forwardReturnPct)
          : null,
        annual_volatility: draft.overrideVol ? percentToDecimal(draft.volPct) : null,
        reason: draft.reason.trim(),
        expires_at: draft.expiresAt,
      };
      return setReturnOverride(planId, draft.instrumentId, body);
    },
    onSuccess: () => {
      setDraft(emptyDraft);
      setFormError(null);
      invalidate();
    },
    onError: (e) => setFormError(queryErrorMessage(e)),
  });

  const deleteMut = useMutation({
    mutationFn: (instrumentId: string) => deleteReturnOverride(planId, instrumentId),
    onSuccess: invalidate,
  });

  const submit = () => {
    setFormError(null);
    if (!draft.instrumentId) {
      setFormError("请选择要覆盖的标的。");
      return;
    }
    if (!draft.overrideReturn && !draft.overrideVol) {
      setFormError("至少覆盖前瞻收益率或波动率之一。");
      return;
    }
    if (draft.overrideReturn && percentToDecimal(draft.forwardReturnPct) === null) {
      setFormError("前瞻收益率格式不正确。");
      return;
    }
    if (draft.overrideVol && percentToDecimal(draft.volPct) === null) {
      setFormError("波动率格式不正确。");
      return;
    }
    if (!draft.reason.trim()) {
      setFormError("请填写覆盖原因。");
      return;
    }
    if (!draft.expiresAt) {
      setFormError("请填写到期日期。");
      return;
    }
    saveMut.mutate();
  };

  const editExisting = (o: ReturnOverride) => {
    setDraft({
      instrumentId: o.instrument_id,
      overrideReturn: o.forward_return !== null,
      forwardReturnPct:
        o.forward_return !== null ? decimalToPercentString(o.forward_return) : "",
      overrideVol: o.annual_volatility !== null,
      volPct:
        o.annual_volatility !== null ? decimalToPercentString(o.annual_volatility) : "",
      reason: o.reason,
      expiresAt: o.expires_at,
    });
    setFormError(null);
  };

  return (
    <section className="rounded-lg border border-line p-4">
      <button
        type="button"
        className="flex w-full items-center justify-between text-left"
        onClick={() => setOpen((v) => !v)}
      >
        <span>
          <span className="text-lg font-medium">资产级收益覆盖</span>
          <span className="ml-2 text-xs text-ink-muted">高级 · 计划特异事实</span>
        </span>
        <span className="text-sm text-ink-muted">{open ? "收起" : "展开"}</span>
      </button>

      {open && (
        <div className="mt-3 space-y-4">
          <p className="text-sm text-ink-muted">
            仅当确有计划特异事实（如锁定到期收益率的持有至到期债券）时使用。覆盖只作用于前瞻几何收益率与波动率，不影响历史、相关性或汇率因子；到期后自动回退到全局假设。修改将使既有模拟结果标记为过期。
          </p>

          {overrides.length > 0 && (
            <table className="w-full text-sm">
              <thead>
                <tr className="text-left text-ink-muted">
                  <th className="py-1">标的</th>
                  <th className="py-1">前瞻收益</th>
                  <th className="py-1">波动率</th>
                  <th className="py-1">原因</th>
                  <th className="py-1">到期</th>
                  <th className="py-1" />
                </tr>
              </thead>
              <tbody>
                {overrides.map((o) => (
                  <tr key={o.instrument_id} className="border-t border-line">
                    <td className="py-1">{nameOf(o.instrument_id)}</td>
                    <td className="py-1 font-mono-numeric">
                      {o.forward_return !== null ? formatPercent(o.forward_return) : "—"}
                    </td>
                    <td className="py-1 font-mono-numeric">
                      {o.annual_volatility !== null ? formatPercent(o.annual_volatility) : "—"}
                    </td>
                    <td className="py-1">{o.reason}</td>
                    <td className="py-1">
                      {o.expires_at}
                      {o.expired && <span className="ml-1 text-danger">（已过期）</span>}
                    </td>
                    <td className="py-1 text-right">
                      <button
                        type="button"
                        className="text-brand hover:underline"
                        onClick={() => editExisting(o)}
                      >
                        编辑
                      </button>
                      <button
                        type="button"
                        className="ml-3 text-danger hover:underline"
                        onClick={() => deleteMut.mutate(o.instrument_id)}
                      >
                        清除
                      </button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          )}

          <div className="rounded-md border border-line p-3">
            <div className="grid gap-3 sm:grid-cols-2">
              <label className="block text-sm">
                标的
                <select
                  className="input-base mt-1"
                  value={draft.instrumentId}
                  onChange={(e) => setDraft({ ...draft, instrumentId: e.target.value })}
                >
                  <option value="">选择标的…</option>
                  {riskHoldings.map((h) => (
                    <option key={h.instrument_id} value={h.instrument_id}>
                      {h.instrument_name || h.instrument_code || h.instrument_id}
                    </option>
                  ))}
                </select>
              </label>
              <label className="block text-sm">
                到期日期
                <input
                  type="date"
                  className="input-base mt-1"
                  value={draft.expiresAt}
                  onChange={(e) => setDraft({ ...draft, expiresAt: e.target.value })}
                />
              </label>
              <label className="block text-sm">
                <span className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={draft.overrideReturn}
                    onChange={(e) => setDraft({ ...draft, overrideReturn: e.target.checked })}
                  />
                  前瞻几何收益率（%）
                </span>
                <input
                  type="text"
                  inputMode="decimal"
                  disabled={!draft.overrideReturn}
                  className="input-base mt-1 font-mono-numeric"
                  value={draft.forwardReturnPct}
                  onChange={(e) => setDraft({ ...draft, forwardReturnPct: e.target.value })}
                />
              </label>
              <label className="block text-sm">
                <span className="flex items-center gap-2">
                  <input
                    type="checkbox"
                    checked={draft.overrideVol}
                    onChange={(e) => setDraft({ ...draft, overrideVol: e.target.checked })}
                  />
                  年化波动率（%）
                </span>
                <input
                  type="text"
                  inputMode="decimal"
                  disabled={!draft.overrideVol}
                  className="input-base mt-1 font-mono-numeric"
                  value={draft.volPct}
                  onChange={(e) => setDraft({ ...draft, volPct: e.target.value })}
                />
              </label>
            </div>
            <label className="mt-3 block text-sm">
              覆盖原因
              <input
                type="text"
                className="input-base mt-1"
                placeholder="例如：持有至到期，锁定到期收益率 3.2%"
                value={draft.reason}
                onChange={(e) => setDraft({ ...draft, reason: e.target.value })}
              />
            </label>
            {formError && <p className="mt-2 text-sm text-danger">{formError}</p>}
            <div className="mt-3 flex items-center gap-3">
              <Button pending={saveMut.isPending} onClick={submit}>
                保存覆盖
              </Button>
              {(draft.instrumentId || draft.reason) && (
                <Button
                  variant="ghost"
                  onClick={() => {
                    setDraft(emptyDraft);
                    setFormError(null);
                  }}
                >
                  取消
                </Button>
              )}
            </div>
          </div>
        </div>
      )}
    </section>
  );
}
