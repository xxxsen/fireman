import { Badge, type BadgeVariant } from "@/components/ui/Badge";
import type { ResearchRunStatus } from "@/lib/api/research";

const RUN_STATUS_LABEL: Record<ResearchRunStatus, string> = {
  queued: "排队中",
  running: "计算中",
  succeeded: "已完成",
  failed: "失败",
  canceled: "已取消",
};

const RUN_STATUS_VARIANT: Record<ResearchRunStatus, BadgeVariant> = {
  queued: "info",
  running: "info",
  succeeded: "positive",
  failed: "danger",
  canceled: "neutral",
};

export function runStatusLabel(status: string): string {
  return RUN_STATUS_LABEL[status as ResearchRunStatus] ?? status;
}

export function runStatusBadge(status: string) {
  const variant = RUN_STATUS_VARIANT[status as ResearchRunStatus] ?? "neutral";
  return <Badge variant={variant}>{runStatusLabel(status)}</Badge>;
}
