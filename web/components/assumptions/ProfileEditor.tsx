"use client";

import { useMutation } from "@tanstack/react-query";
import { useState } from "react";
import { Alert } from "@/components/ui/Alert";
import { Button } from "@/components/ui/Button";
import { validateAssumptionProfile } from "@/lib/api/assumptions";
import { assetClassLabel, regionLabel } from "@/lib/format";
import type {
  AssumptionCorrelationPrior,
  AssumptionFXPrior,
  AssumptionProfile,
  AssumptionReturnPrior,
  AssumptionValidation,
} from "@/types/api";
import {
  type EditorState,
  factorLabel,
  factorUniverse,
  isRequiredBaseReturnPrior,
  scenarioLabel,
  todayISO,
} from "./shared";

const ASSET_CLASS_OPTIONS = ["equity", "bond", "cash"] as const;
const REGION_OPTIONS = ["domestic", "foreign"] as const;

function NumberField({
  label,
  ariaLabel,
  value,
  step,
  onChange,
  testid,
}: {
  label: string;
  ariaLabel?: string;
  value: number;
  step?: number;
  onChange: (v: number) => void;
  testid?: string;
}) {
  return (
    <label className="flex flex-col text-xs text-ink-muted">
      {label}
      <input
        type="number"
        step={step ?? "any"}
        aria-label={label || ariaLabel}
        className="mt-0.5 w-28 rounded border border-line px-2 py-1 text-ink"
        value={Number.isFinite(value) ? value : 0}
        data-testid={testid}
        onChange={(e) => onChange(Number(e.target.value))}
      />
    </label>
  );
}

function TextField({
  label,
  ariaLabel,
  value,
  onChange,
  testid,
  width = "w-44",
  invalid,
}: {
  label: string;
  ariaLabel?: string;
  value: string;
  onChange: (v: string) => void;
  testid?: string;
  width?: string;
  invalid?: boolean;
}) {
  return (
    <label className="flex flex-col text-xs text-ink-muted">
      {label}
      <input
        type="text"
        aria-label={label || ariaLabel}
        aria-invalid={invalid || undefined}
        className={`mt-0.5 ${width} rounded border px-2 py-1 text-ink ${
          invalid ? "border-danger" : "border-line"
        }`}
        value={value}
        data-testid={testid}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}

function EnumSelect({
  ariaLabel,
  value,
  options,
  labelFor,
  onChange,
}: {
  ariaLabel: string;
  value: string;
  options: readonly string[];
  labelFor: (v: string) => string;
  onChange: (v: string) => void;
}) {
  const all = options.includes(value) || value === "" ? [...options] : [value, ...options];
  return (
    <select
      aria-label={ariaLabel}
      className="rounded border border-line px-2 py-1 text-ink"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {all.map((o) => (
        <option key={o} value={o}>
          {labelFor(o)}
        </option>
      ))}
    </select>
  );
}

export interface ProfileEditorProps {
  state: EditorState;
  onChange: (s: EditorState) => void;
  onSave: () => void;
  onCancel: () => void;
  savePending: boolean;
}

export function ProfileEditor({
  state,
  onChange,
  onSave,
  onCancel,
  savePending,
}: ProfileEditorProps) {
  const { profile } = state;
  const [validation, setValidation] = useState<AssumptionValidation | null>(null);

  const validateMut = useMutation({
    mutationFn: () => validateAssumptionProfile(profile.id, profile.version, profile),
    onSuccess: (res) => setValidation(res),
    onError: () =>
      setValidation({ valid: false, error: "校验请求失败", min_eigenvalue: 0, max_repair_delta: 0, psd_repair_heavy: false }),
  });

  const patch = (p: Partial<AssumptionProfile>) => {
    setValidation(null);
    onChange({ ...state, profile: { ...profile, ...p } });
  };
  const patchMeta = (m: Partial<Pick<EditorState, "sourceNote" | "reviewedBy" | "reviewedAt">>) =>
    onChange({ ...state, ...m });

  const universe = factorUniverse(profile);
  const reviewedAtInvalid =
    state.reviewedAt !== "" && !/^\d{4}-\d{2}-\d{2}$/.test(state.reviewedAt);

  return (
    <section className="space-y-5 rounded-lg border border-brand/40 bg-surface p-4" data-testid="profile-editor">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="font-medium text-ink">
            编辑 profile <span className="font-mono text-xs text-ink-muted">{profile.id}@{profile.version}（草稿）</span>
          </h2>
          {state.sourceLabel && (
            <p className="mt-1 text-xs text-ink-muted" data-testid="editor-source-label">
              {state.sourceLabel}
            </p>
          )}
        </div>
        <div className="flex gap-2">
          <Button variant="ghost" onClick={onCancel}>
            取消
          </Button>
          <Button variant="secondary" disabled={validateMut.isPending} onClick={() => validateMut.mutate()}>
            校验
          </Button>
          <Button disabled={savePending} onClick={onSave} data-testid="editor-save">
            保存为草稿
          </Button>
        </div>
      </div>

      {validation && (
        <Alert variant={validation.valid ? (validation.psd_repair_heavy ? "warning" : "info") : "danger"}>
          {validation.valid
            ? `结构校验通过。相关性矩阵最小特征值 ${validation.min_eigenvalue.toFixed(4)}，最大 PSD 修复 ${validation.max_repair_delta.toFixed(4)}${
                validation.psd_repair_heavy ? "（修复幅度较大，建议复核相关性后再激活）" : ""
              }`
            : `校验失败：${validation.error ?? "未知错误"}`}
        </Alert>
      )}

      <div className="flex flex-wrap gap-3">
        <TextField label="名称" value={profile.name} width="w-56" onChange={(v) => patch({ name: v })} testid="editor-name" />
        <NumberField label="厚尾自由度 ν" value={profile.student_t_df} step={1} onChange={(v) => patch({ student_t_df: v })} />
        <NumberField label="先验等效年数" value={profile.prior_strength_years} step={1} onChange={(v) => patch({ prior_strength_years: v })} />
        <NumberField label="相关性收缩月数" value={profile.correlation_strength_months} step={1} onChange={(v) => patch({ correlation_strength_months: v })} />
        <NumberField label="收益截断下限" value={profile.return_floor} onChange={(v) => patch({ return_floor: v })} testid="editor-return-floor" />
        <NumberField label="收益截断上限" value={profile.return_ceil} onChange={(v) => patch({ return_ceil: v })} testid="editor-return-ceil" />
      </div>

      <div className="flex flex-wrap gap-3">
        <TextField label="来源说明（CMA 出处）" value={state.sourceNote} width="w-72" onChange={(v) => patchMeta({ sourceNote: v })} testid="editor-source-note" />
        <TextField label="审核人" value={state.reviewedBy} onChange={(v) => patchMeta({ reviewedBy: v })} testid="editor-reviewed-by" />
        <TextField
          label="审核日期 (YYYY-MM-DD)"
          value={state.reviewedAt}
          invalid={reviewedAtInvalid}
          onChange={(v) => patchMeta({ reviewedAt: v })}
          testid="editor-reviewed-at"
        />
      </div>
      {reviewedAtInvalid && (
        <p className="text-xs text-danger" role="alert">
          审核日期格式应为 YYYY-MM-DD。
        </p>
      )}

      <ScenarioEditor profile={profile} onChange={patch} />
      <ReturnPriorEditor profile={profile} onChange={patch} />
      <FXPriorEditor profile={profile} onChange={patch} />
      <CorrelationEditor profile={profile} universe={universe} onChange={patch} />
    </section>
  );
}

function ScenarioEditor({
  profile,
  onChange,
}: {
  profile: AssumptionProfile;
  onChange: (p: Partial<AssumptionProfile>) => void;
}) {
  const names = ["conservative", "baseline", "optimistic"];
  const set = (name: string, key: "return_shift_log" | "return_shift_log_fx" | "volatility_multiplier", v: number) => {
    const cur = profile.scenarios[name] ?? { return_shift_log: 0, return_shift_log_fx: 0, volatility_multiplier: 1 };
    onChange({ scenarios: { ...profile.scenarios, [name]: { ...cur, [key]: v } } });
  };
  return (
    <div>
      <h3 className="text-sm font-medium text-ink-muted">假设情景</h3>
      <table className="mt-1 min-w-full text-left text-xs">
        <caption className="sr-only">假设情景编辑</caption>
        <thead>
          <tr className="text-ink-muted">
            <th scope="col" className="pr-4 py-1">假设情景</th>
            <th scope="col" className="pr-4 py-1">收益对数位移</th>
            <th scope="col" className="pr-4 py-1">FX 收益位移</th>
            <th scope="col" className="pr-4 py-1">波动率乘子</th>
          </tr>
        </thead>
        <tbody>
          {names.map((name) => {
            const s = profile.scenarios[name] ?? { return_shift_log: 0, return_shift_log_fx: 0, volatility_multiplier: 1 };
            return (
              <tr key={name} className="border-t">
                <td className="py-1 pr-4">{scenarioLabel(name)}</td>
                <td className="py-1 pr-4">
                  <NumberField label="" ariaLabel={`${scenarioLabel(name)}收益对数位移`} value={s.return_shift_log} onChange={(v) => set(name, "return_shift_log", v)} />
                </td>
                <td className="py-1 pr-4">
                  <NumberField label="" ariaLabel={`${scenarioLabel(name)}FX 收益位移`} value={s.return_shift_log_fx} onChange={(v) => set(name, "return_shift_log_fx", v)} />
                </td>
                <td className="py-1 pr-4">
                  <NumberField label="" ariaLabel={`${scenarioLabel(name)}波动率乘子`} value={s.volatility_multiplier} onChange={(v) => set(name, "volatility_multiplier", v)} />
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function ReturnPriorEditor({
  profile,
  onChange,
}: {
  profile: AssumptionProfile;
  onChange: (p: Partial<AssumptionProfile>) => void;
}) {
  const rows = profile.return_priors;
  // Valid valuation currencies: base CNY plus any currency present in FX priors.
  const currencyOptions = [
    ...new Set(["CNY", ...(profile.fx_priors ?? []).map((fx) => fx.from_currency)]),
  ];
  const update = (i: number, patch: Partial<AssumptionReturnPrior>) => {
    const next = rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r));
    onChange({ return_priors: next });
  };
  const add = () =>
    onChange({
      return_priors: [
        ...rows,
        {
          asset_class: "equity",
          region: "domestic",
          valuation_currency: "CNY",
          annual_geometric_return: 0.05,
          annual_volatility_floor: 0.1,
          annual_volatility_ceiling: 0.3,
          source_url: "https://",
          published_at: todayISO(),
          reviewed_at: todayISO(),
        },
      ],
    });
  const remove = (i: number) => onChange({ return_priors: rows.filter((_, idx) => idx !== i) });

  return (
    <div className="overflow-x-auto">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-ink-muted">收益先验（CNY 基础条目必填，不可删除）</h3>
        <Button variant="ghost" className="px-2 py-1" onClick={add}>
          + 新增
        </Button>
      </div>
      <table className="mt-1 min-w-full text-left text-xs">
        <caption className="sr-only">收益先验编辑</caption>
        <thead>
          <tr className="text-ink-muted">
            <th scope="col" className="pr-2 py-1">资产类别</th>
            <th scope="col" className="pr-2 py-1">地区</th>
            <th scope="col" className="pr-2 py-1">计价币种</th>
            <th scope="col" className="pr-2 py-1">年化几何</th>
            <th scope="col" className="pr-2 py-1">波动下限</th>
            <th scope="col" className="pr-2 py-1">波动上限</th>
            <th scope="col" className="pr-2 py-1">来源 URL</th>
            <th scope="col" className="pr-2 py-1">发布</th>
            <th scope="col" className="pr-2 py-1">审核</th>
            <th scope="col" className="pr-2 py-1" />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => {
            const required = isRequiredBaseReturnPrior(r);
            return (
              <tr key={i} className="border-t align-top">
                <td className="py-1 pr-2">
                  <EnumSelect
                    ariaLabel={`第 ${i + 1} 行资产类别`}
                    value={r.asset_class}
                    options={ASSET_CLASS_OPTIONS}
                    labelFor={assetClassLabel}
                    onChange={(v) => update(i, { asset_class: v })}
                  />
                </td>
                <td className="py-1 pr-2">
                  <EnumSelect
                    ariaLabel={`第 ${i + 1} 行地区`}
                    value={r.region}
                    options={REGION_OPTIONS}
                    labelFor={regionLabel}
                    onChange={(v) => update(i, { region: v })}
                  />
                </td>
                <td className="py-1 pr-2">
                  <EnumSelect
                    ariaLabel={`第 ${i + 1} 行计价币种`}
                    value={r.valuation_currency}
                    options={currencyOptions}
                    labelFor={(v) => v}
                    onChange={(v) => update(i, { valuation_currency: v })}
                  />
                </td>
                <td className="py-1 pr-2">
                  <NumberField label="" ariaLabel={`第 ${i + 1} 行年化几何收益`} value={r.annual_geometric_return} onChange={(v) => update(i, { annual_geometric_return: v })} />
                </td>
                <td className="py-1 pr-2">
                  <NumberField label="" ariaLabel={`第 ${i + 1} 行波动率下限`} value={r.annual_volatility_floor} onChange={(v) => update(i, { annual_volatility_floor: v })} />
                </td>
                <td className="py-1 pr-2">
                  <NumberField label="" ariaLabel={`第 ${i + 1} 行波动率上限`} value={r.annual_volatility_ceiling} onChange={(v) => update(i, { annual_volatility_ceiling: v })} />
                </td>
                <td className="py-1 pr-2">
                  <TextField label="" ariaLabel={`第 ${i + 1} 行来源 URL`} value={r.source_url} width="w-40" onChange={(v) => update(i, { source_url: v })} />
                </td>
                <td className="py-1 pr-2">
                  <TextField label="" ariaLabel={`第 ${i + 1} 行发布日期`} value={r.published_at} width="w-24" onChange={(v) => update(i, { published_at: v })} />
                </td>
                <td className="py-1 pr-2">
                  <TextField label="" ariaLabel={`第 ${i + 1} 行审核日期`} value={r.reviewed_at} width="w-24" onChange={(v) => update(i, { reviewed_at: v })} />
                </td>
                <td className="py-1 pr-2">
                  {required ? (
                    <span className="text-ink-muted" title="产品支持的 CNY 基础资产条目，不可删除">
                      必填
                    </span>
                  ) : (
                    <Button variant="ghost" className="px-2 py-1" onClick={() => remove(i)}>
                      删除
                    </Button>
                  )}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}

function FXPriorEditor({
  profile,
  onChange,
}: {
  profile: AssumptionProfile;
  onChange: (p: Partial<AssumptionProfile>) => void;
}) {
  const rows = profile.fx_priors ?? [];
  const update = (i: number, patch: Partial<AssumptionFXPrior>) => {
    const next = rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r));
    onChange({ fx_priors: next });
  };
  const add = () =>
    onChange({
      fx_priors: [
        ...rows,
        {
          from_currency: "USD",
          base_currency: "CNY",
          annual_geometric_return: 0,
          annual_volatility_floor: 0.03,
          annual_volatility_ceiling: 0.12,
          source_url: "https://",
          published_at: todayISO(),
          reviewed_at: todayISO(),
        },
      ],
    });
  const remove = (i: number) => onChange({ fx_priors: rows.filter((_, idx) => idx !== i) });

  return (
    <div className="overflow-x-auto">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-ink-muted">FX 先验</h3>
        <Button variant="ghost" className="px-2 py-1" onClick={add}>
          + 新增
        </Button>
      </div>
      <table className="mt-1 min-w-full text-left text-xs">
        <caption className="sr-only">FX 先验编辑</caption>
        <thead>
          <tr className="text-ink-muted">
            <th scope="col" className="pr-2 py-1">从</th>
            <th scope="col" className="pr-2 py-1">到</th>
            <th scope="col" className="pr-2 py-1">年化几何</th>
            <th scope="col" className="pr-2 py-1">波动下限</th>
            <th scope="col" className="pr-2 py-1">波动上限</th>
            <th scope="col" className="pr-2 py-1">来源 URL</th>
            <th scope="col" className="pr-2 py-1">发布</th>
            <th scope="col" className="pr-2 py-1">审核</th>
            <th scope="col" className="pr-2 py-1" />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-t align-top">
              <td className="py-1 pr-2">
                <TextField label="" ariaLabel={`第 ${i + 1} 行起始币种`} value={r.from_currency} width="w-16" onChange={(v) => update(i, { from_currency: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" ariaLabel={`第 ${i + 1} 行目标币种`} value={r.base_currency} width="w-16" onChange={(v) => update(i, { base_currency: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" ariaLabel={`第 ${i + 1} 行年化几何收益`} value={r.annual_geometric_return} onChange={(v) => update(i, { annual_geometric_return: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" ariaLabel={`第 ${i + 1} 行波动率下限`} value={r.annual_volatility_floor} onChange={(v) => update(i, { annual_volatility_floor: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" ariaLabel={`第 ${i + 1} 行波动率上限`} value={r.annual_volatility_ceiling} onChange={(v) => update(i, { annual_volatility_ceiling: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" ariaLabel={`第 ${i + 1} 行来源 URL`} value={r.source_url} width="w-40" onChange={(v) => update(i, { source_url: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" ariaLabel={`第 ${i + 1} 行发布日期`} value={r.published_at} width="w-24" onChange={(v) => update(i, { published_at: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" ariaLabel={`第 ${i + 1} 行审核日期`} value={r.reviewed_at} width="w-24" onChange={(v) => update(i, { reviewed_at: v })} />
              </td>
              <td className="py-1 pr-2">
                <Button variant="ghost" className="px-2 py-1" onClick={() => remove(i)}>
                  删除
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}

function CorrelationEditor({
  profile,
  universe,
  onChange,
}: {
  profile: AssumptionProfile;
  universe: string[];
  onChange: (p: Partial<AssumptionProfile>) => void;
}) {
  const rows = profile.correlation_priors ?? [];
  const update = (i: number, patch: Partial<AssumptionCorrelationPrior>) => {
    const next = rows.map((r, idx) => (idx === i ? { ...r, ...patch } : r));
    onChange({ correlation_priors: next });
  };
  const add = () =>
    onChange({
      correlation_priors: [...rows, { factor_a: universe[0] ?? "", factor_b: universe[1] ?? "", rho: 0 }],
    });
  const remove = (i: number) => onChange({ correlation_priors: rows.filter((_, idx) => idx !== i) });

  // fillMissing adds every uncovered universe pair with rho=0 so completeness
  // can be satisfied in one click; existing pairs are preserved.
  const fillMissing = () => {
    const have = new Set(
      rows.map((r) => [r.factor_a, r.factor_b].sort().join("|")),
    );
    const next = [...rows];
	for (const factor of universe.filter((item) => item.startsWith("asset:"))) {
	  const key = `${factor}|${factor}`;
	  if (!have.has(key)) {
		next.push({ factor_a: factor, factor_b: factor, rho: 1 });
		have.add(key);
	  }
	}
    for (let i = 0; i < universe.length; i++) {
      for (let j = i + 1; j < universe.length; j++) {
        const key = [universe[i], universe[j]].sort().join("|");
        if (!have.has(key)) {
          next.push({ factor_a: universe[i], factor_b: universe[j], rho: 0 });
          have.add(key);
        }
      }
    }
    onChange({ correlation_priors: next });
  };

  return (
    <div className="overflow-x-auto">
      <div className="flex items-center justify-between">
        <h3 className="text-sm font-medium text-ink-muted">相关性先验</h3>
        <div className="flex gap-2">
          <Button variant="ghost" className="px-2 py-1" onClick={fillMissing} data-testid="fill-missing-corr">
            补全缺失对（ρ=0）
          </Button>
          <Button variant="ghost" className="px-2 py-1" onClick={add}>
            + 新增
          </Button>
        </div>
      </div>
      <p className="mt-1 text-xs text-ink-muted">
        因子宇宙需两两配对，并为每个资产因子提供“同类型不同资产”先验（{universe.map(factorLabel).join("、") || "暂无非现金因子"}）。
      </p>
      <table className="mt-1 min-w-full text-left text-xs">
        <caption className="sr-only">相关性先验编辑</caption>
        <thead>
          <tr className="text-ink-muted">
            <th scope="col" className="pr-2 py-1">因子 A</th>
            <th scope="col" className="pr-2 py-1">因子 B</th>
            <th scope="col" className="pr-2 py-1">ρ</th>
            <th scope="col" className="pr-2 py-1" />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-t">
              <td className="py-1 pr-2">
                <EnumSelect
                  ariaLabel={`第 ${i + 1} 行因子 A`}
                  value={r.factor_a}
                  options={universe}
                  labelFor={factorLabel}
                  onChange={(v) => update(i, { factor_a: v })}
                />
              </td>
              <td className="py-1 pr-2">
                <EnumSelect
                  ariaLabel={`第 ${i + 1} 行因子 B`}
                  value={r.factor_b}
                  options={universe}
                  labelFor={factorLabel}
                  onChange={(v) => update(i, { factor_b: v })}
                />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" ariaLabel={`第 ${i + 1} 行相关系数`} value={r.rho} onChange={(v) => update(i, { rho: v })} />
              </td>
              <td className="py-1 pr-2">
                <Button variant="ghost" className="px-2 py-1" onClick={() => remove(i)}>
                  删除
                </Button>
              </td>
            </tr>
          ))}
        </tbody>
      </table>
    </div>
  );
}
