import { Badge, type BadgeVariant } from "@/components/ui/Badge";
import type { AdminJobStatus } from "@/lib/api/admin";

const LABELS: Record<AdminJobStatus, string> = {
  queued: "排队中",
  running: "运行中",
  succeeded: "已完成",
  failed: "失败",
  canceled: "已取消",
};

const VARIANTS: Record<AdminJobStatus, BadgeVariant> = {
  queued: "info",
  running: "info",
  succeeded: "positive",
  failed: "danger",
  canceled: "neutral",
};

export function JobStatusBadge({ status }: { status: AdminJobStatus }) {
  return (
    <Badge variant={VARIANTS[status] ?? "neutral"}>
      <span data-testid="job-status-badge" data-status={status}>
        {LABELS[status] ?? status}
      </span>
    </Badge>
  );
}
