"use client";

import Link from "next/link";
import { useQuery } from "@tanstack/react-query";
import { useState } from "react";
import { listPlans } from "@/lib/api/plans";
import {
  downloadBackup,
  exportPlanJsonUrl,
  exportRebalanceCsvUrl,
  exportTargetsCsvUrl,
  restoreBackup,
} from "@/lib/api/system";

export default function SettingsPage() {
  const { data: plans = [] } = useQuery({ queryKey: ["plans"], queryFn: listPlans });
  const [exportPlanId, setExportPlanId] = useState("");
  const [message, setMessage] = useState<string | null>(null);
  const [error, setError] = useState<string | null>(null);
  const [busy, setBusy] = useState(false);

  const handleDownload = async () => {
    setBusy(true);
    setError(null);
    try {
      const { blob, filename } = await downloadBackup();
      const url = URL.createObjectURL(blob);
      const a = document.createElement("a");
      a.href = url;
      a.download = filename;
      a.click();
      URL.revokeObjectURL(url);
      setMessage("备份已下载。");
    } catch (e) {
      setError(e instanceof Error ? e.message : "下载失败");
    } finally {
      setBusy(false);
    }
  };

  const handleRestore = async (file: File | null) => {
    if (!file) return;
    if (!window.confirm("恢复将替换当前数据库，并自动备份现有数据。是否继续？")) return;
    setBusy(true);
    setError(null);
    try {
      const res = await restoreBackup(file);
      setMessage(
        res.restart_required
          ? "备份已恢复。请重启 Fireman 后端服务使更改生效。"
          : "备份已恢复。",
      );
    } catch (e) {
      setError(e instanceof Error ? e.message : "恢复失败");
    } finally {
      setBusy(false);
    }
  };

  return (
    <div className="max-w-2xl">
      <h1 className="text-2xl font-semibold">设置</h1>
      <p className="mt-2 text-slate-600">数据备份、恢复与计划导出。</p>

      {message && <p className="mt-4 text-sm text-emerald-700">{message}</p>}
      {error && <p className="mt-4 text-sm text-red-600">{error}</p>}

      <section className="mt-8 space-y-4">
        <div className="rounded-lg border p-4">
          <h2 className="font-medium">数据库备份</h2>
          <p className="mt-1 text-sm text-slate-600">下载当前 SQLite 数据库完整备份。</p>
          <button
            type="button"
            className="mt-3 rounded-md bg-slate-900 px-3 py-2 text-sm text-white disabled:opacity-50"
            disabled={busy}
            onClick={() => void handleDownload()}
          >
            下载备份
          </button>
        </div>

        <div className="rounded-lg border p-4">
          <h2 className="font-medium">恢复备份</h2>
          <p className="mt-1 text-sm text-slate-600">
            上传备份文件；恢复前会自动备份当前数据库。恢复后需重启后端。
          </p>
          <input
            type="file"
            accept=".db,.bak,application/octet-stream"
            className="mt-3 block text-sm"
            disabled={busy}
            onChange={(e) => void handleRestore(e.target.files?.[0] ?? null)}
          />
        </div>

        <div className="rounded-lg border p-4">
          <h2 className="font-medium">计划导出</h2>
          <p className="mt-1 text-sm text-slate-600">导出计划 JSON 及目标/调仓 CSV。</p>
          <label className="mt-3 block text-sm">
            选择计划
            <select
              className="mt-1 w-full rounded-md border px-3 py-2"
              value={exportPlanId}
              onChange={(e) => setExportPlanId(e.target.value)}
            >
              <option value="">请选择</option>
              {plans.map((p) => (
                <option key={p.id} value={p.id}>
                  {p.name}
                </option>
              ))}
            </select>
          </label>
          {exportPlanId && (
            <div className="mt-3 flex flex-wrap gap-3 text-sm">
              <a className="underline" href={exportPlanJsonUrl(exportPlanId)} download>
                导出 JSON
              </a>
              <a className="underline" href={exportTargetsCsvUrl(exportPlanId)} download>
                导出目标 CSV
              </a>
              <a className="underline" href={exportRebalanceCsvUrl(exportPlanId)} download>
                导出调仓 CSV
              </a>
            </div>
          )}
        </div>

        <div className="rounded-lg border p-4">
          <h2 className="font-medium">系统信息</h2>
          <dl className="mt-2 text-sm text-slate-600">
            <dt>部署模式</dt>
            <dd>单用户本地优先</dd>
            <dt className="mt-2">基础货币</dt>
            <dd>CNY（可在各计划中调整）</dd>
          </dl>
          <Link href="/" className="mt-3 inline-block text-sm underline">
            返回计划列表
          </Link>
        </div>
      </section>
    </div>
  );
}
