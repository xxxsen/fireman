import { MetricHelp } from "./MetricHelp";

export function StaleBanner({ message }: { message?: string }) {
  return (
    <div
      role="alert"
      data-testid="stale-banner"
      className="rounded-lg border border-warning/30 bg-warning/5 px-4 py-3 text-sm text-warning"
    >
      {message ?? "配置已变化，结果已过期"}
      <MetricHelp termKey="result_stale" />
    </div>
  );
}
