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
  validateAssumptionProfile,
} from "@/lib/api/assumptions";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type {
  AssumptionCorrelationPrior,
  AssumptionFXPrior,
  AssumptionProfile,
  AssumptionProfileSummary,
  AssumptionReturnPrior,
  AssumptionValidation,
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

// nextUserProfileId returns a fresh, collision-resistant id for a custom copy.
function nextUserProfileId(): string {
  return `user_cma_${Math.random().toString(36).slice(2, 7)}`;
}

// maxVersionForId returns the highest stored version for a profile id, so editing
// an existing user profile always saves as a new version (never in place; td/063 R3).
function maxVersionForId(profiles: AssumptionProfileSummary[], id: string): number {
  return profiles.reduce((m, p) => (p.id === id && p.version > m ? p.version : m), 0);
}

export default function AssumptionsPage() {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<{ id: string; version: number } | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [editing, setEditing] = useState<EditorState | null>(null);

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

  // Copy a (system or user) profile into a fresh editable user draft. Per td/063
  // R3 this does not save immediately; it opens the editor so the user can edit,
  // pre-validate and then save as a new draft version.
  const startCopy = (profile: AssumptionProfile) => {
    const draft: AssumptionProfile = {
      ...structuredClone(profile),
      id: nextUserProfileId(),
      version: 1,
      owner_scope: "user",
      status: "draft",
      name: `${profile.name}（自定义副本）`,
    };
    setActionError(null);
    setEditing({ profile: draft, sourceNote: `copied from ${profile.id}@${profile.version}`, reviewedBy: "", reviewedAt: todayISO() });
  };

  // Edit an existing user profile by opening a new-version draft (never in place).
  const startEditNewVersion = (profile: AssumptionProfile) => {
    const draft: AssumptionProfile = {
      ...structuredClone(profile),
      version: maxVersionForId(profiles, profile.id) + 1,
      status: "draft",
    };
    setActionError(null);
    setEditing({ profile: draft, sourceNote: `edited from ${profile.id}@${profile.version}`, reviewedBy: "", reviewedAt: todayISO() });
  };

  const saveMut = useMutation({
    mutationFn: (s: EditorState) =>
      saveAssumptionProfile({
        profile: s.profile,
        source_note: s.sourceNote,
        reviewed_by: s.reviewedBy,
        reviewed_at: s.reviewedAt,
      }),
    onSuccess: (res) => {
      setActionError(null);
      setEditing(null);
      setSelected({ id: res.profile.id, version: res.profile.version });
      refresh();
    },
    onError: (e) => setActionError(e instanceof Error ? e.message : "保存失败"),
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

      {editing ? (
        <ProfileEditor
          state={editing}
          onChange={setEditing}
          onSave={() => saveMut.mutate(editing)}
          onCancel={() => {
            setEditing(null);
            setActionError(null);
          }}
          savePending={saveMut.isPending}
        />
      ) : (
        <>
          {detailQ.isLoading && <LoadingState label="加载 profile 详情…" />}
          {profile && (
            <ProfileDetail
              profile={profile}
              onCopy={() => startCopy(profile)}
              onEdit={() => startEditNewVersion(profile)}
            />
          )}
        </>
      )}
    </div>
  );
}

function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
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
        新建计划默认使用此处选择的 profile 与情景；未配置时使用系统 system_cma_v2。
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
      {ineligibleActive.length > 0 && (
        <p className="mt-3 text-xs text-ink-muted" data-testid="ineligible-default-note">
          以下 profile 仅用于历史兼容，不能作为全局默认（缺少当前发布门槛要求的覆盖/厚尾/校验）：
          {ineligibleActive.map((p) => `${p.id}@${p.version}`).join("、")}
        </p>
      )}
    </section>
  );
}

function ProfileDetail({
  profile,
  onCopy,
  onEdit,
}: {
  profile: AssumptionProfile;
  onCopy: () => void;
  onEdit: () => void;
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

interface EditorState {
  profile: AssumptionProfile;
  sourceNote: string;
  reviewedBy: string;
  reviewedAt: string;
}

// factorUniverse mirrors the backend canonical factor set: one factor per
// distinct non-cash (asset_class, region) return prior and one per FX prior
// (td/063 R4). The correlation editor uses it so every pair is a real factor.
function factorUniverse(p: AssumptionProfile): string[] {
  const set = new Set<string>();
  for (const rp of p.return_priors) {
    if (rp.asset_class !== "cash") set.add(`asset:${rp.asset_class}:${rp.region}`);
  }
  for (const fx of p.fx_priors ?? []) set.add(`fx:${fx.from_currency}:${fx.base_currency}`);
  return [...set].sort();
}

function NumberField({
  label,
  value,
  step,
  onChange,
  testid,
}: {
  label: string;
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
  value,
  onChange,
  testid,
  width = "w-44",
}: {
  label: string;
  value: string;
  onChange: (v: string) => void;
  testid?: string;
  width?: string;
}) {
  return (
    <label className="flex flex-col text-xs text-ink-muted">
      {label}
      <input
        type="text"
        className={`mt-0.5 ${width} rounded border border-line px-2 py-1 text-ink`}
        value={value}
        data-testid={testid}
        onChange={(e) => onChange(e.target.value)}
      />
    </label>
  );
}

function ProfileEditor({
  state,
  onChange,
  onSave,
  onCancel,
  savePending,
}: {
  state: EditorState;
  onChange: (s: EditorState) => void;
  onSave: () => void;
  onCancel: () => void;
  savePending: boolean;
}) {
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

  return (
    <section className="space-y-5 rounded-lg border border-brand/40 bg-surface p-4" data-testid="profile-editor">
      <div className="flex flex-wrap items-center justify-between gap-2">
        <h2 className="font-medium text-ink">
          编辑 profile <span className="font-mono text-xs text-ink-muted">{profile.id}@{profile.version}（草稿）</span>
        </h2>
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
        <TextField label="审核日期 (YYYY-MM-DD)" value={state.reviewedAt} onChange={(v) => patchMeta({ reviewedAt: v })} testid="editor-reviewed-at" />
      </div>

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
          {names.map((name) => {
            const s = profile.scenarios[name] ?? { return_shift_log: 0, return_shift_log_fx: 0, volatility_multiplier: 1 };
            return (
              <tr key={name} className="border-t">
                <td className="py-1 pr-4">{scenarioLabel(name)}</td>
                <td className="py-1 pr-4">
                  <NumberField label="" value={s.return_shift_log} onChange={(v) => set(name, "return_shift_log", v)} />
                </td>
                <td className="py-1 pr-4">
                  <NumberField label="" value={s.return_shift_log_fx} onChange={(v) => set(name, "return_shift_log_fx", v)} />
                </td>
                <td className="py-1 pr-4">
                  <NumberField label="" value={s.volatility_multiplier} onChange={(v) => set(name, "volatility_multiplier", v)} />
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
        <thead>
          <tr className="text-ink-muted">
            <th className="pr-2 py-1">资产</th>
            <th className="pr-2 py-1">地区</th>
            <th className="pr-2 py-1">币种</th>
            <th className="pr-2 py-1">年化几何</th>
            <th className="pr-2 py-1">波动下限</th>
            <th className="pr-2 py-1">波动上限</th>
            <th className="pr-2 py-1">来源 URL</th>
            <th className="pr-2 py-1">发布</th>
            <th className="pr-2 py-1">审核</th>
            <th className="pr-2 py-1" />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-t align-top">
              <td className="py-1 pr-2">
                <TextField label="" value={r.asset_class} width="w-20" onChange={(v) => update(i, { asset_class: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.region} width="w-20" onChange={(v) => update(i, { region: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.valuation_currency} width="w-16" onChange={(v) => update(i, { valuation_currency: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" value={r.annual_geometric_return} onChange={(v) => update(i, { annual_geometric_return: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" value={r.annual_volatility_floor} onChange={(v) => update(i, { annual_volatility_floor: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" value={r.annual_volatility_ceiling} onChange={(v) => update(i, { annual_volatility_ceiling: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.source_url} width="w-40" onChange={(v) => update(i, { source_url: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.published_at} width="w-24" onChange={(v) => update(i, { published_at: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.reviewed_at} width="w-24" onChange={(v) => update(i, { reviewed_at: v })} />
              </td>
              <td className="py-1 pr-2">
                {isRequiredBaseReturnPrior(r) ? (
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
          ))}
        </tbody>
      </table>
    </div>
  );
}

// REQUIRED_BASE_RETURN_CELLS mirrors the backend RequiredGlobalCoverage (td/064
// R7): these CNY base-currency cells must exist in every active/global profile,
// so the editor forbids deleting them.
const REQUIRED_BASE_RETURN_CELLS: ReadonlyArray<readonly [string, string]> = [
  ["equity", "domestic"],
  ["equity", "foreign"],
  ["bond", "domestic"],
  ["bond", "foreign"],
  ["cash", "domestic"],
];

function isRequiredBaseReturnPrior(r: AssumptionReturnPrior): boolean {
  return (
    r.valuation_currency === "CNY" &&
    REQUIRED_BASE_RETURN_CELLS.some(([c, region]) => c === r.asset_class && region === r.region)
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
        <thead>
          <tr className="text-ink-muted">
            <th className="pr-2 py-1">从</th>
            <th className="pr-2 py-1">到</th>
            <th className="pr-2 py-1">年化几何</th>
            <th className="pr-2 py-1">波动下限</th>
            <th className="pr-2 py-1">波动上限</th>
            <th className="pr-2 py-1">来源 URL</th>
            <th className="pr-2 py-1">发布</th>
            <th className="pr-2 py-1">审核</th>
            <th className="pr-2 py-1" />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-t align-top">
              <td className="py-1 pr-2">
                <TextField label="" value={r.from_currency} width="w-16" onChange={(v) => update(i, { from_currency: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.base_currency} width="w-16" onChange={(v) => update(i, { base_currency: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" value={r.annual_geometric_return} onChange={(v) => update(i, { annual_geometric_return: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" value={r.annual_volatility_floor} onChange={(v) => update(i, { annual_volatility_floor: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" value={r.annual_volatility_ceiling} onChange={(v) => update(i, { annual_volatility_ceiling: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.source_url} width="w-40" onChange={(v) => update(i, { source_url: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.published_at} width="w-24" onChange={(v) => update(i, { published_at: v })} />
              </td>
              <td className="py-1 pr-2">
                <TextField label="" value={r.reviewed_at} width="w-24" onChange={(v) => update(i, { reviewed_at: v })} />
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
  // (td/063 R4) can be satisfied in one click; existing pairs are preserved.
  const fillMissing = () => {
    const have = new Set(
      rows.map((r) => [r.factor_a, r.factor_b].sort().join("|")),
    );
    const next = [...rows];
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
        因子宇宙需两两配对：每个不同因子对必须恰有一个先验（{universe.map(factorLabel).join("、") || "暂无非现金因子"}）。
      </p>
      <table className="mt-1 min-w-full text-left text-xs">
        <thead>
          <tr className="text-ink-muted">
            <th className="pr-2 py-1">因子 A</th>
            <th className="pr-2 py-1">因子 B</th>
            <th className="pr-2 py-1">ρ</th>
            <th className="pr-2 py-1" />
          </tr>
        </thead>
        <tbody>
          {rows.map((r, i) => (
            <tr key={i} className="border-t">
              <td className="py-1 pr-2">
                <FactorSelect value={r.factor_a} options={universe} onChange={(v) => update(i, { factor_a: v })} />
              </td>
              <td className="py-1 pr-2">
                <FactorSelect value={r.factor_b} options={universe} onChange={(v) => update(i, { factor_b: v })} />
              </td>
              <td className="py-1 pr-2">
                <NumberField label="" value={r.rho} onChange={(v) => update(i, { rho: v })} />
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

function FactorSelect({
  value,
  options,
  onChange,
}: {
  value: string;
  options: string[];
  onChange: (v: string) => void;
}) {
  const all = options.includes(value) || value === "" ? options : [value, ...options];
  return (
    <select
      className="rounded border border-line px-2 py-1 text-ink"
      value={value}
      onChange={(e) => onChange(e.target.value)}
    >
      {all.map((o) => (
        <option key={o} value={o}>
          {factorLabel(o)}
        </option>
      ))}
    </select>
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
