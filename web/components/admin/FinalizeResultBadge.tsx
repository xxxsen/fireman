import { Badge, type BadgeVariant } from "@/components/ui/Badge";
import { FINALIZE_RESULT_LABELS, type FinalizeResult } from "@/lib/api/admin";

const VARIANTS: Record<FinalizeResult, BadgeVariant> = {
  success: "positive",
  retryable_error: "warning",
  permanent_error: "danger",
};

export function FinalizeResultBadge({ result }: { result: FinalizeResult }) {
  return (
    <Badge variant={VARIANTS[result] ?? "neutral"}>
      <span data-testid="finalize-result-badge" data-result={result}>
        {FINALIZE_RESULT_LABELS[result] ?? result}
      </span>
    </Badge>
  );
}
