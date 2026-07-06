import { Badge, type BadgeVariant } from "@/components/ui/Badge";

const BADGE_META: Record<string, { label: string; variant: BadgeVariant }> = {
  normal: { label: "正常", variant: "positive" },
  missing_history: { label: "缺历史", variant: "danger" },
  short_history: { label: "短历史", variant: "warning" },
  stale: { label: "数据过期", variant: "warning" },
  fx_missing: { label: "缺 FX", variant: "danger" },
  abnormal_volatility: { label: "异常波动", variant: "warning" },
  sync_failed: { label: "同步失败", variant: "danger" },
};

export function qualityBadgeLabel(code: string): string {
  return BADGE_META[code]?.label ?? code;
}

export function QualityBadges({ badges }: { badges: string[] }) {
  if (!badges.length) return null;
  return (
    <span className="inline-flex flex-wrap gap-1">
      {badges.map((code) => {
        const meta = BADGE_META[code] ?? { label: code, variant: "neutral" as const };
        return (
          <Badge key={code} variant={meta.variant}>
            {meta.label}
          </Badge>
        );
      })}
    </span>
  );
}
