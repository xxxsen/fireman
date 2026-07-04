"use client";

import { useMemo } from "react";
import { Button } from "@/components/ui/Button";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";
import type { AssumptionProfile } from "@/types/api";
import { buildCorrelationMatrix, factorLabel, scenarioLabel } from "./shared";

export interface ProfileDetailProps {
  profile: AssumptionProfile;
  onCopy: () => void;
  onEdit: () => void;
}

export function ProfileDetail({ profile, onCopy, onEdit }: ProfileDetailProps) {
  const correlation = useMemo(() => buildCorrelationMatrix(profile), [profile]);
  const isSystem = profile.owner_scope === "system";

  return (
    <section className="space-y-4 rounded-lg border border-line bg-surface p-4">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <div>
          <h2 className="font-medium text-ink">
            {profile.name} <span className="font-mono text-xs text-ink-muted">{profile.id}@{profile.version}</span>
          </h2>
          <p className="mt-1 text-xs text-ink-muted">
            {isSystem ? "系统只读 profile" : "自定义 profile"} · 厚尾自由度 ν={profile.student_t_df} · 收益截断{" "}
            {formatPercent(profile.return_floor)} ~ {formatPercent(profile.return_ceil)} · 先验等效年数{" "}
            {profile.prior_strength_years} · 相关性收缩等效月数 {profile.correlation_strength_months}
          </p>
        </div>
        <div className="flex gap-2">
          <Button variant="secondary" onClick={onCopy}>
            复制为自定义
          </Button>
          {!isSystem && (
            <Button variant="secondary" onClick={onEdit}>
              编辑为新版本
            </Button>
          )}
        </div>
      </div>

      <div className="overflow-x-auto">
        <h3 className="text-sm font-medium text-ink-muted">假设情景</h3>
        <table className="mt-1 min-w-full text-left text-xs">
          <caption className="sr-only">假设情景参数</caption>
          <thead>
            <tr className="text-ink-muted">
              <th scope="col" className="pr-4 py-1">假设情景</th>
              <th scope="col" className="pr-4 py-1">收益对数位移</th>
              <th scope="col" className="pr-4 py-1">FX 收益位移</th>
              <th scope="col" className="pr-4 py-1">波动率乘子</th>
            </tr>
          </thead>
          <tbody>
            {Object.entries(profile.scenarios).map(([name, s]) => (
              <tr key={name} className="border-t">
                <td className="py-1 pr-4">{scenarioLabel(name)}</td>
                <td className="py-1 pr-4">{s.return_shift_log.toFixed(4)}</td>
                <td className="py-1 pr-4">{s.return_shift_log_fx.toFixed(4)}</td>
                <td className="py-1 pr-4">{s.volatility_multiplier.toFixed(2)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      <div className="overflow-x-auto">
        <h3 className="text-sm font-medium text-ink-muted">收益先验（费用后·基准币种·名义几何）</h3>
        <table className="mt-1 min-w-full text-left text-xs">
          <caption className="sr-only">收益先验列表</caption>
          <thead>
            <tr className="text-ink-muted">
              <th scope="col" className="pr-4 py-1">资产类别</th>
              <th scope="col" className="pr-4 py-1">地区</th>
              <th scope="col" className="pr-4 py-1">计价币种</th>
              <th scope="col" className="pr-4 py-1">年化几何收益</th>
              <th scope="col" className="pr-4 py-1">波动率下限/上限</th>
              <th scope="col" className="pr-4 py-1">来源</th>
            </tr>
          </thead>
          <tbody>
            {profile.return_priors.map((p) => (
              <tr key={`${p.asset_class}/${p.region}/${p.valuation_currency}`} className="border-t">
                <td className="py-1 pr-4">{assetClassLabel(p.asset_class)}</td>
                <td className="py-1 pr-4">{regionLabel(p.region)}</td>
                <td className="py-1 pr-4">{p.valuation_currency}</td>
                <td className="py-1 pr-4">{formatPercent(p.annual_geometric_return)}</td>
                <td className="py-1 pr-4">
                  {formatPercent(p.annual_volatility_floor)} / {formatPercent(p.annual_volatility_ceiling)}
                </td>
                <td className="py-1 pr-4 max-w-xs truncate" title={p.source_url}>
                  {p.source_url || "—"}
                </td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>

      {(profile.fx_priors?.length ?? 0) > 0 && (
        <div className="overflow-x-auto">
          <h3 className="text-sm font-medium text-ink-muted">FX 先验</h3>
          <table className="mt-1 min-w-full text-left text-xs">
            <caption className="sr-only">FX 先验列表</caption>
            <thead>
              <tr className="text-ink-muted">
                <th scope="col" className="pr-4 py-1">货币对</th>
                <th scope="col" className="pr-4 py-1">年化几何收益</th>
                <th scope="col" className="pr-4 py-1">波动率下限/上限</th>
              </tr>
            </thead>
            <tbody>
              {profile.fx_priors!.map((p) => (
                <tr key={`${p.from_currency}/${p.base_currency}`} className="border-t">
                  <td className="py-1 pr-4">
                    {p.from_currency}→{p.base_currency}
                  </td>
                  <td className="py-1 pr-4">{formatPercent(p.annual_geometric_return)}</td>
                  <td className="py-1 pr-4">
                    {formatPercent(p.annual_volatility_floor)} / {formatPercent(p.annual_volatility_ceiling)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}

      {correlation.keys.length > 0 && (
        <div className="overflow-x-auto">
          <h3 className="text-sm font-medium text-ink-muted">相关性先验矩阵</h3>
          <table className="mt-1 min-w-full text-left text-xs">
            <caption className="sr-only">相关性先验矩阵</caption>
            <thead>
              <tr className="text-ink-muted">
                <th scope="col" className="pr-3 py-1" />
                {correlation.keys.map((k) => (
                  <th scope="col" key={k} className="px-2 py-1 text-center">
                    {factorLabel(k)}
                  </th>
                ))}
              </tr>
            </thead>
            <tbody>
              {correlation.keys.map((rowKey, i) => (
                <tr key={rowKey} className="border-t">
                  <td className="py-1 pr-3 font-medium">{factorLabel(rowKey)}</td>
                  {correlation.keys.map((_, j) => (
                    <td key={j} className="px-2 py-1 text-center">
                      {correlation.matrix[i][j].toFixed(2)}
                    </td>
                  ))}
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      )}
    </section>
  );
}
