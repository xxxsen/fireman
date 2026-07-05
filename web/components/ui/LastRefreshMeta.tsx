import { dataSourceLabel, formatDateTimeFromMs } from "@/lib/format";

export interface LastRefreshMetaProps {
  /** Millisecond epoch of the last successful refresh. */
  lastSuccessAt?: number | null;
  /** Data cutoff date (YYYY-MM-DD). */
  dataAsOf?: string;
  sourceName?: string;
  className?: string;
}

/** Compact "last refresh / data as-of / source" metadata line. */
export function LastRefreshMeta({
  lastSuccessAt,
  dataAsOf,
  sourceName,
  className,
}: LastRefreshMetaProps) {
  return (
    <dl
      data-testid="last-refresh-meta"
      className={`flex flex-wrap items-center gap-x-4 gap-y-1 text-xs text-ink-muted ${className ?? ""}`}
    >
      <div className="flex items-center gap-1">
        <dt>最后成功刷新</dt>
        <dd data-testid="last-refresh-at" className="font-mono-numeric text-ink">
          {formatDateTimeFromMs(lastSuccessAt)}
        </dd>
      </div>
      <div className="flex items-center gap-1">
        <dt>数据截至</dt>
        <dd data-testid="last-refresh-data-as-of" className="font-mono-numeric text-ink">
          {dataAsOf?.trim() ? dataAsOf : "—"}
        </dd>
      </div>
      <div className="flex items-center gap-1">
        <dt>数据源</dt>
        <dd data-testid="last-refresh-source" className="text-ink">
          {dataSourceLabel(sourceName)}
        </dd>
      </div>
    </dl>
  );
}
