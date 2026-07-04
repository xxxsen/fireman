"use client";

import { useState } from "react";
import { Button } from "@/components/ui/Button";
import type { AssumptionProfileSummary } from "@/types/api";
import { scenarioLabel } from "./shared";

export interface PreferencesCardProps {
  profiles: AssumptionProfileSummary[];
  defaultId: string;
  defaultVersion: number;
  defaultScenario: string;
  scenarios: string[];
  pending: boolean;
  onSave: (id: string, version: number, scenario: string) => void;
}

export function PreferencesCard({
  profiles,
  defaultId,
  defaultVersion,
  defaultScenario,
  scenarios,
  pending,
  onSave,
}: PreferencesCardProps) {
  const eligibleProfiles = profiles.filter((p) => p.eligible_for_global_default);
  const ineligibleActive = profiles.filter(
    (p) => p.status === "active" && !p.eligible_for_global_default,
  );
  const [sel, setSel] = useState(`${defaultId}@${defaultVersion}`);
  const [scenario, setScenario] = useState(defaultScenario);

  const parse = (v: string): { id: string; version: number } => {
    const [id, ver] = v.split("@");
    return { id, version: Number(ver) };
  };

  return (
    <section className="rounded-lg border border-line bg-surface p-4">
      <h2 className="font-medium text-ink">全局默认</h2>
      <p className="mt-1 text-xs text-ink-muted">
        新建计划默认使用此处选择的 profile 与假设情景；未配置时使用系统 system_cma_v3。
      </p>
      <div className="mt-3 flex flex-wrap items-end gap-4">
        <label className="text-sm text-ink">
          默认 profile
          <select
            className="ml-2 rounded border border-line px-2 py-1"
            value={sel}
            onChange={(e) => setSel(e.target.value)}
            data-testid="default-profile-select"
          >
            {eligibleProfiles.map((p) => (
              <option key={`${p.id}@${p.version}`} value={`${p.id}@${p.version}`}>
                {p.name}（{p.id}@{p.version}）
              </option>
            ))}
          </select>
        </label>
        <label className="text-sm text-ink">
          默认假设情景
          <select
            className="ml-2 rounded border border-line px-2 py-1"
            value={scenario}
            onChange={(e) => setScenario(e.target.value)}
            data-testid="default-scenario-select"
          >
            {scenarios.map((s) => (
              <option key={s} value={s}>
                {scenarioLabel(s)}
              </option>
            ))}
          </select>
        </label>
        <Button
          disabled={pending || !sel}
          onClick={() => {
            const { id, version } = parse(sel);
            onSave(id, version, scenario);
          }}
        >
          保存默认
        </Button>
      </div>
      {ineligibleActive.length > 0 && (
        <p className="mt-3 text-xs text-ink-muted" data-testid="ineligible-default-note">
          以下 profile 仅用于历史回放或显式 pin，不能作为全局默认（已被更新的系统默认取代，或缺少当前发布门槛要求的覆盖/厚尾/校验）：
          {ineligibleActive.map((p) => `${p.id}@${p.version}`).join("、")}
        </p>
      )}
    </section>
  );
}
