import { Badge, type BadgeVariant } from "@/components/ui/Badge";
import { CALLBACK_RESULT_LABELS, type CallbackResult } from "@/lib/api/admin";

const VARIANTS: Record<CallbackResult, BadgeVariant> = {
  success: "positive",
  retryable_error: "warning",
  permanent_error: "danger",
};

export function CallbackResultBadge({ result }: { result: CallbackResult }) {
  return (
    <Badge variant={VARIANTS[result] ?? "neutral"}>
      <span data-testid="callback-result-badge" data-result={result}>
        {CALLBACK_RESULT_LABELS[result] ?? result}
      </span>
    </Badge>
  );
}
