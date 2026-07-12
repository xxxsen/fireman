"use client";

import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { useState } from "react";
import { PageHeader } from "@/components/ui/PageHeader";
import { Alert } from "@/components/ui/Alert";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { ProfileList } from "@/components/assumptions/ProfileList";
import { PreferencesCard } from "@/components/assumptions/PreferencesCard";
import { ProfileDetail } from "@/components/assumptions/ProfileDetail";
import { ProfileEditor } from "@/components/assumptions/ProfileEditor";
import {
  type EditorState,
  maxVersionForId,
  nextUserProfileId,
  todayISO,
} from "@/components/assumptions/shared";
import {
  activateAssumptionProfile,
  getAssumptionProfile,
  listAssumptionProfiles,
  saveAssumptionProfile,
  setAssumptionPreferences,
} from "@/lib/api/assumptions";
import { queryErrorMessage } from "@/lib/query-error";
import type { AssumptionProfile } from "@/types/api";

export default function AssumptionsPage() {
  const qc = useQueryClient();
  const [selected, setSelected] = useState<{ id: string; version: number } | null>(null);
  const [actionError, setActionError] = useState<string | null>(null);
  const [actionNotice, setActionNotice] = useState<string | null>(null);
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
	void qc.invalidateQueries({ queryKey: ["parameters"] });
	void qc.invalidateQueries({ queryKey: ["simulations"] });
  };

  // Copy a (system or user) profile into a fresh editable user draft. This does not save immediately; it opens the editor so the user can edit,
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
    setEditing({
      profile: draft,
      sourceLabel: `基于「${profile.name}」（${profile.id}@${profile.version}）复制`,
      sourceNote: `copied from ${profile.id}@${profile.version}`,
      reviewedBy: "",
      reviewedAt: todayISO(),
    });
  };

  // Edit an existing user profile by opening a new-version draft (never in place).
  const startEditNewVersion = (profile: AssumptionProfile) => {
    const draft: AssumptionProfile = {
      ...structuredClone(profile),
      version: maxVersionForId(profiles, profile.id) + 1,
      status: "draft",
    };
    setActionError(null);
    setEditing({
      profile: draft,
      sourceLabel: `基于「${profile.name}」（${profile.id}@${profile.version}）编辑为新版本`,
      sourceNote: `edited from ${profile.id}@${profile.version}`,
      reviewedBy: "",
      reviewedAt: todayISO(),
    });
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
    onSuccess: (result) => {
      setActionError(null);
	  setActionNotice(result.default_migrated ? "已激活；全局默认已迁移到新版本。" : "已激活新版本。")
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
    <div className="content-enter space-y-6">
      <PageHeader
        title="模拟假设"
        description="资本市场先验、假设情景、相关性与厚尾参数的全局唯一编辑入口。系统默认 profile 只读；复制为自定义后可编辑并激活。"
      />

      {actionError && (
        <Alert variant="danger">{actionError}</Alert>
      )}
      {actionNotice && <Alert variant="info">{actionNotice}</Alert>}

      <ProfileList
        profiles={profiles}
        currentId={current?.id}
        currentVersion={current?.version}
        defaultId={preferences?.default_profile_id}
        defaultVersion={preferences?.default_profile_version}
        activatePending={activateMut.isPending}
        onSelect={(id, version) => setSelected({ id, version })}
        onActivate={(id, version) => activateMut.mutate({ id, version })}
      />

      <PreferencesCard
        key={`${preferences?.default_profile_id}@${preferences?.default_profile_version}/${preferences?.default_scenario}`}
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
          {detailQ.isError && !detailQ.data && (
            <ErrorState
              message="无法加载 profile 详情。请确认后端服务可用后重试。"
              onRetry={() => void detailQ.refetch()}
              technicalDetail={queryErrorMessage(detailQ.error)}
            />
          )}
          {profile && (
            <ProfileDetail
              profile={profile}
              summary={profiles.find((item) => item.id === profile.id && item.version === profile.version)}
              onCopy={() => startCopy(profile)}
              onEdit={() => startEditNewVersion(profile)}
            />
          )}
        </>
      )}
    </div>
  );
}
