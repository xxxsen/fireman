"use client";

import { useMutation, useQuery } from "@tanstack/react-query";
import { useRef, useState } from "react";
import { PageHeader } from "@/components/ui/PageHeader";
import { Button } from "@/components/ui/Button";
import { Alert } from "@/components/ui/Alert";
import { ConfirmDialog } from "@/components/ui/ConfirmDialog";
import { LoadingState } from "@/components/ui/LoadingState";
import { listPlans } from "@/lib/api/plans";
import {
  downloadBackup,
  exportPlanJsonUrl,
  exportRebalanceCsvUrl,
  exportTargetsCsvUrl,
  restoreBackup,
} from "@/lib/api/system";
import { queryErrorMessage } from "@/lib/query-error";

export default function SettingsPage() {
  const {
    data: plans = [],
    isLoading: plansLoading,
    isError: plansError,
  } = useQuery({ queryKey: ["plans"], queryFn: listPlans });
  const [exportPlanId, setExportPlanId] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [pendingFile, setPendingFile] = useState<File | null>(null);
  const [restoreError, setRestoreError] = useState<string | null>(null);
  const fileInputRef = useRef<HTMLInputElement>(null);

  const downloadMut = useMutation({
    mutationFn: downloadBackup,
    onSuccess: ({ blob, filename }) => {
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
      setMessage("备份已下载。");
    },
  });

  const restoreMut = useMutation({
    mutationFn: (file: File) => restoreBackup(file),
    onSuccess: (res) => {
      setPendingFile(null);
      setRestoreError(null);
      resetFileInput();
      setMessage(
        res.restart_required
          ? "备份已恢复。请重启 Fireman 后端服务使更改生效。"
          : "备份已恢复。",
      );
    },
    onError: (e) => setRestoreError(queryErrorMessage(e, "恢复失败")),
  });

  const resetFileInput = () => {
    if (fileInputRef.current) fileInputRef.current.value = "";
  };

  return (
    <div className="content-enter max-w-2xl">
      <PageHeader
        backHref="/"
        backLabel="返回计划列表"
        title="设置"
        description="数据备份、恢复与计划导出。"
      />

      {message && (
        <Alert variant="success" className="mb-4">
          {message}
        </Alert>
      )}
      {downloadMut.isError && (
        <Alert variant="danger" className="mb-4">
          {queryErrorMessage(downloadMut.error, "下载失败")}
        </Alert>
      )}

      <section className="space-y-4">
        <div className="rounded-lg border border-line bg-surface p-4">
          <h2 className="font-medium text-ink">数据库备份</h2>
          <p className="mt-1 text-sm text-ink-muted">下载当前 SQLite 数据库完整备份。</p>
          <Button
            className="mt-3"
            pending={downloadMut.isPending}
            onClick={() => {
              setMessage(null);
              downloadMut.mutate();
            }}
          >
            下载备份
          </Button>
        </div>

        <div className="rounded-lg border border-line bg-surface p-4">
          <h2 className="font-medium text-ink">恢复备份</h2>
          <p className="mt-1 text-sm text-ink-muted">
            上传备份文件；恢复前会自动备份当前数据库。恢复后需重启后端。
          </p>
          <input
            ref={fileInputRef}
            type="file"
            accept=".db,.bak,application/octet-stream"
            className="mt-3 block text-sm text-ink"
            disabled={restoreMut.isPending}
            onChange={(e) => {
              const file = e.target.files?.[0] ?? null;
              if (!file) return;
              setMessage(null);
              setRestoreError(null);
              setPendingFile(file);
            }}
          />
          {restoreMut.isPending && (
            <LoadingState label="恢复中…" className="mt-2 text-xs" />
          )}
        </div>

        <div className="rounded-lg border border-line bg-surface p-4">
          <h2 className="font-medium text-ink">计划导出</h2>
          <p className="mt-1 text-sm text-ink-muted">导出计划 JSON 及目标/调仓 CSV。</p>
          {plansError ? (
            <Alert variant="danger" className="mt-3">
              无法加载计划列表，导出暂不可用。请确认后端服务可用后刷新页面。
            </Alert>
          ) : (
            <label className="mt-3 block text-sm text-ink">
              选择计划
              <select
                className="input-base mt-1"
                value={exportPlanId}
                disabled={plansLoading}
                onChange={(e) => setExportPlanId(e.target.value)}
              >
                <option value="">{plansLoading ? "加载中…" : "请选择"}</option>
                {plans.map((p) => (
                  <option key={p.id} value={p.id}>
                    {p.name}
                  </option>
                ))}
              </select>
            </label>
          )}
          {exportPlanId && (
            <div className="mt-3 flex flex-wrap gap-3 text-sm">
              <a
                className="text-brand underline-offset-2 hover:underline"
                href={exportPlanJsonUrl(exportPlanId)}
                download
              >
                导出 JSON
              </a>
              <a
                className="text-brand underline-offset-2 hover:underline"
                href={exportTargetsCsvUrl(exportPlanId)}
                download
              >
                导出目标 CSV
              </a>
              <a
                className="text-brand underline-offset-2 hover:underline"
                href={exportRebalanceCsvUrl(exportPlanId)}
                download
              >
                导出调仓 CSV
              </a>
            </div>
          )}
        </div>

        <div className="rounded-lg border border-line bg-surface p-4">
          <h2 className="font-medium text-ink">系统信息</h2>
          <dl className="mt-2 text-sm text-ink-muted">
            <dt>部署模式</dt>
            <dd>单用户本地优先</dd>
            <dt className="mt-2">基础货币</dt>
            <dd>CNY（系统假设 profile 仅覆盖 CNY，所有计划固定使用）</dd>
          </dl>
        </div>
      </section>

      <ConfirmDialog
        open={pendingFile !== null}
        title="恢复备份"
        description="恢复将替换当前数据库，并自动备份现有数据。恢复后需重启后端服务。是否继续？"
        confirmLabel="恢复备份"
        variant="danger"
        pending={restoreMut.isPending}
        error={restoreError}
        onConfirm={() => {
          if (pendingFile) restoreMut.mutate(pendingFile);
        }}
        onClose={() => {
          setPendingFile(null);
          setRestoreError(null);
          resetFileInput();
        }}
      />
    </div>
  );
}
