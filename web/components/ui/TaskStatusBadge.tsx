import type { WorkerTaskStatus } from "@/lib/api/market-assets";
import { Badge, type BadgeVariant } from "./Badge";

const DEFAULT_LABELS: Record<WorkerTaskStatus, string> = {
  pending: "等待同步",
  running: "同步中",
  pre_complete: "处理中",
  complete: "同步成功",
  failed: "同步失败",
  canceled: "已取消",
};

const STATUS_VARIANTS: Record<WorkerTaskStatus, BadgeVariant> = {
  pending: "info",
  running: "info",
  pre_complete: "info",
  complete: "positive",
  failed: "danger",
  canceled: "neutral",
};

export interface TaskStatusBadgeProps {
  status: WorkerTaskStatus;
  /** Page-specific wording overrides, e.g. complete → “最近同步成功”. */
  labels?: Partial<Record<WorkerTaskStatus, string>>;
  className?: string;
}

export function TaskStatusBadge({ status, labels, className }: TaskStatusBadgeProps) {
  const label = labels?.[status] ?? DEFAULT_LABELS[status] ?? status;
  return (
    <Badge variant={STATUS_VARIANTS[status] ?? "neutral"} className={className}>
      <span data-testid="task-status-badge" data-status={status}>
        {label}
      </span>
    </Badge>
  );
}
