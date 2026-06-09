const API_BASE = process.env.NEXT_PUBLIC_API_BASE_URL ?? "";

export async function downloadBackup(): Promise<{ blob: Blob; filename: string }> {
  const res = await fetch(`${API_BASE}/api/v1/system/backup`);
  if (!res.ok) {
    throw new Error("下载备份失败");
  }
  const disposition = res.headers.get("Content-Disposition") ?? "";
  const match = disposition.match(/filename=([^;]+)/);
  const filename = match?.[1]?.trim() ?? "fireman-backup.db";
  const blob = await res.blob();
  return { blob, filename };
}

export async function restoreBackup(file: File): Promise<{ restored: boolean; restart_required: boolean }> {
  const res = await fetch(`${API_BASE}/api/v1/system/restore`, {
    method: "POST",
    headers: { "Content-Type": "application/octet-stream" },
    body: file,
  });
  const body = await res.json().catch(() => null);
  if (!res.ok) {
    throw new Error((body as { message?: string })?.message ?? "恢复失败");
  }
  const data = (body as { data?: { restored: boolean; restart_required: boolean } }).data;
  return data ?? { restored: true, restart_required: true };
}

export function exportPlanJsonUrl(planId: string) {
  return `${API_BASE}/api/v1/plans/${planId}/export/json`;
}

export function exportTargetsCsvUrl(planId: string) {
  return `${API_BASE}/api/v1/plans/${planId}/export/targets.csv`;
}

export function exportRebalanceCsvUrl(planId: string) {
  return `${API_BASE}/api/v1/plans/${planId}/export/rebalance.csv`;
}
