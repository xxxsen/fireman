"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useMemo, useState } from "react";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { Badge } from "@/components/ui/Badge";
import { Alert } from "@/components/ui/Alert";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import {
  activateAssumptionProfile,
  getAssumptionProfile,
  listAssumptionProfiles,
  saveAssumptionProfile,
  setAssumptionPreferences,
} from "@/lib/api/assumptions";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type {
  AssumptionProfile,
  AssumptionProfileSummary,
} from "@/types/api";

const SCENARIO_LABELS: Record<string, string> = {
  conservative: "保守",
  baseline: "基准",
  optimistic: "乐观",
};

function scenarioLabel(s: string): string {
  return SCENARIO_LABELS[s] ?? s;
}

function statusBadge(status: string) {
  switch (status) {
    case "active":
      return <Badge variant="positive">已激活</Badge>;
    case "draft":
      return <Badge variant="info">草稿</Badge>;
    default:
      return <Badge variant="neutral">已弃用</Badge>;
  }
}

function factorLabel(key: string): string {
  const parts = key.split(":");
  if (parts[0] === "asset" && parts.length === 3) {
    return `${assetClassLabel(parts[1])}·${regionLabel(parts[2])}`;
  }
  if (parts[0] === "fx" && parts.length === 3) {
    return `汇率 ${parts[1]}→${parts[2]}`;
  }
  return key;
}

export default function AssumptionsPage() {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<{ id: string; version: number } | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);

  const listQ = useQuery({
    queryKey: ["assumption-profiles"],
    queryFn: listAssumptionProfiles,
  });

  const profiles = listQ.data?.profiles ?? [];
  const preferences = listQ.data?.preferences;
  const scenarios = listQ.data?.scenarios ?? ["conservative", "baseline", "optimistic"];

  const current =
    selected ??
    (preferences
      ? { id: preferences.default_profile_id, version: preferences.default_profile_version }
      : profiles[0]
        ? { id: profiles[0].id, version: profiles[0].version }
        : null);

  const detailQ = useQuery({
    queryKey: ["assumption-profile", current?.id, current?.version],
    queryFn: () => getAssumptionProfile(current!.id, current!.version),
    enabled: !!current,
  });

  const refresh = () => {
    void qc.invalidateQueries({ queryKey: ["assumption-profiles"] });
    void qc.invalidateQueries({ queryKey: ["assumption-profile"] });
  };

  const copyMut = useMutation({
    mutationFn: (profile: AssumptionProfile) => {
      const suffix = Math.random().toString(36).slice(2, 7);
      const draft: AssumptionProfile = {
        ...profile,
        id: `user_cma_${suffix}`,
        version: 1,
        owner_scope: "user",
        status: "draft",
        name: `${profile.name}（自定义副本）`,
      };
      return saveAssumptionProfile({
        profile: draft,
        source_note: `copied from ${profile.id}@${profile.version}`,
      });
    },
    onSuccess: (res) => {
      setActionError(null);
      setSelected({ id: res.profile.id, version: res.profile.version });
      refresh();
    },
    onError: (e) => setActionError(e instanceof Error ? e.message : "复制失败"),
  });

  const activateMut = useMutation({
    mutationFn: (p: { id: string; version: number }) => activateAssumptionProfile(p.id, p.version),
    onSuccess: () => {
      setActionError(null);
      refresh();
    },
    onError: (e) => setActionError(e instanceof Error ? e.message : "激活失败"),
  });

  const prefMut = useMutation({
    mutationFn: (p: { id: string; version: number; scenario: string }) =>
      setAssumptionPreferences({
        default_profile_id: p.id,
        default_profile_version: p.version,
        default_scenario: p.scenario,
      }),
    onSuccess: () => {
      setActionError(null);
      refresh();
    },
    onError: (e) => setActionError(e instanceof Error ? e.message : "保存默认失败"),
  });

  if (listQ.isError && !listQ.data) {
    return (
      <ErrorState
        message="无法加载模拟假设。请确认后端服务可用后重试。"
        onRetry={() => void listQ.refetch()}
        technicalDetail={queryErrorMessage(listQ.error)}
      />
    );
  }
  if (listQ.isLoading || !listQ.data) {
    return <LoadingState label="加载模拟假设…" />;
  }

  const profile = detailQ.data?.profile;

  return (
    <div className="space-y-6">
      <PageHeader
        title="模拟假设"
        description="资本市场先验、情景、相关性与厚尾参数的全局唯一编辑入口。系统默认 profile 只读；复制为自定义后可编辑并激活。"
      />

      {actionError && (
        <Alert variant="danger">{actionError}</Alert>
      )}

      <section className="rounded-lg border border-line bg-surface p-4">
        <h2 className="font-medium text-ink">假设 Profile</h2>
        <div className="mt-3 overflow-x-auto">
          <table className="min-w-full text-left text-sm">
            <thead>
              <tr className="text-ink-muted">
                <th className="pr-4 py-1">名称</th>
                <th className="pr-4 py-1">ID@版本</th>
                <th className="pr-4 py-1">归属</th>
                <th className="pr-4 py-1">状态</th>
                <th className="pr-4 py-1">默认</th>
                <th className="pr-4 py-1" />
              </tr>
            </thead>
            <tbody>
              {profiles.map((p: AssumptionProfileSummary) => {
                const isDefault =
                  preferences?.default_profile_id === p.id &&
                  preferences?.default_profile_version === p.version;
                const isCurrent = current?.id === p.id && current?.version === p.version;
                return (
                  <tr key={`${p.id}@${p.version}`} className={`border-t ${isCurrent ? "bg-brand/5" : ""}`}>
                    <td className="py-1 pr-4">{p.name}</td>
                    <td className="py-1 pr-4 font-mono text-xs">
                      {p.id}@{p.version}
                    </td>
                    <td className="py-1 pr-4">{p.owner_scope === "system" ? "系统" : "自定义"}</td>
                    <td className="py-1 pr-4">{statusBadge(p.status)}</td>
                    <td className="py-1 pr-4">{isDefault ? <Badge variant="info">全局默认</Badge> : ""}</td>
                    <td className="py-1 pr-4">
                      <div className="flex gap-2">
                        <Button
                          variant="ghost"
                          className="px-2 py-1"
                          onClick={() => setSelected({ id: p.id, version: p.version })}
                        >
                          查看
                        </Button>
                        {p.status === "draft" && (
                          <Button
                            variant="secondary"
                            className="px-2 py-1"
                            disabled={activateMut.isPending}
                            onClick={() => activateMut.mutate({ id: p.id, version: p.version })}
                          >
                            激活
                          </Button>
                        )}
                      </div>
                    </td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <PreferencesCard
        profiles={profiles}
        defaultId={preferences?.default_profile_id ?? ""}
        defaultVersion={preferences?.default_profile_version ?? 0}
        defaultScenario={preferences?.default_scenario ?? "baseline"}
        scenarios={scenarios}
        pending={prefMut.isPending}
        onSave={(id, version, scenario) => prefMut.mutate({ id, version, scenario })}
      />

      {detailQ.isLoading && <LoadingState label="加载 profile 详情…" />}
      {profile && (
        <ProfileDetail
          profile={profile}
          onCopy={() => copyMut.mutate(profile)}
          copyPending={copyMut.isPending}
        />
      )}
    </div>
  );
}

function PreferencesCard({
  profiles,
  defaultId,
  defaultVersion,
  defaultScenario,
  scenarios,
  pending,
  onSave,
}: {
  profiles: AssumptionProfileSummary[];
  defaultId: string;
  defaultVersion: number;
  defaultScenario: string;
  scenarios: string[];
  pending: boolean;
  onSave: (id: string, version: number, scenario: string) => void;
}) {
  const activeProfiles = profiles.filter((p) => p.status === "active");
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
        新建计划默认使用此处选择的 profile 与情景；未配置时使用系统 system_cma_v1。
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
            {activeProfiles.map((p) => (
              <option key={`${p.id}@${p.version}`} value={`${p.id}@${p.version}`}>
                {p.name}（{p.id}@{p.version}）
              </option>
            ))}
          </select>
        </label>
        <label className="text-sm text-ink">
          默认情景
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
    </section>
  );
}

function ProfileDetail({
  profile,
  onCopy,
  copyPending,
}: {
  profile: AssumptionProfile;
  onCopy: () => void;
  copyPending: boolean;
}) {
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
            {isSystem ? "系统只读 profile" : "自定义 profile"} · 厚尾自由度 ν={profile.student_t_df} · 先验等效年数{" "}
            {profile.prior_strength_years} · 相关性收缩等效月数 {profile.correlation_strength_months}
          </p>
        </div>
        <Button variant="secondary" disabled={copyPending} onClick={onCopy}>
          复制为自定义
        </Button>
      </div>

      <div className="overflow-x-auto">
        <h3 className="text-sm font-medium text-ink-muted">情景</h3>
        <table className="mt-1 min-w-full text-left text-xs">
          <thead>
            <tr className="text-ink-muted">
              <th className="pr-4 py-1">情景</th>
              <th className="pr-4 py-1">收益对数位移</th>
              <th className="pr-4 py-1">FX 收益位移</th>
              <th className="pr-4 py-1">波动率乘子</th>
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
          <thead>
            <tr className="text-ink-muted">
              <th className="pr-4 py-1">资产类别</th>
              <th className="pr-4 py-1">地区</th>
              <th className="pr-4 py-1">计价币种</th>
              <th className="pr-4 py-1">年化几何收益</th>
              <th className="pr-4 py-1">波动率下限/上限</th>
              <th className="pr-4 py-1">来源</th>
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
            <thead>
              <tr className="text-ink-muted">
                <th className="pr-4 py-1">货币对</th>
                <th className="pr-4 py-1">年化几何收益</th>
                <th className="pr-4 py-1">波动率下限/上限</th>
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
            <thead>
              <tr className="text-ink-muted">
                <th className="pr-3 py-1" />
                {correlation.keys.map((k) => (
                  <th key={k} className="px-2 py-1 text-center">
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

function buildCorrelationMatrix(profile: AssumptionProfile): {
  keys: string[];
  matrix: number[][];
} {
  const idx = new Map<string, number>();
  const keys: string[] = [];
  const add = (k: string) => {
    if (!idx.has(k)) {
      idx.set(k, keys.length);
      keys.push(k);
    }
  };
  for (const c of profile.correlation_priors ?? []) {
    add(c.factor_a);
    add(c.factor_b);
  }
  const n = keys.length;
  const matrix: number[][] = Array.from({ length: n }, (_, i) =>
    Array.from({ length: n }, (_, j): number => (i === j ? 1 : 0)),
  );
  for (const c of profile.correlation_priors ?? []) {
    const i = idx.get(c.factor_a)!;
    const j = idx.get(c.factor_b)!;
    matrix[i][j] = c.rho;
    matrix[j][i] = c.rho;
  }
  return { keys, matrix };
}
