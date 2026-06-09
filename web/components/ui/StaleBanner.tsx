import { MetricHelp } from "./MetricHelp";

export function StaleBanner({ message }: { message?: string }) {
  return (
    <div
      role="alert"
      data-testid="stale-banner"
      className="rounded-lg border border-amber-200 bg-amber-50 px-4 py-3 text-sm text-amber-900"
    >
      {message ?? "配置已变化，结果已过期"}
      <MetricHelp termKey="result_stale" />
    </div>
  );
}
