/** Pure formatting helpers for the admin console (duration, relative time). */

/**
 * Format a millisecond duration compactly: `45ms`, `19s`, `3分12秒`,
 * `2小时5分`. Null/undefined/negative values render as "—" (an unfinished or
 * unknown span).
 */
export function formatDurationMs(ms: number | null | undefined): string {
  if (ms == null || Number.isNaN(ms) || ms < 0) return "—";
  if (ms < 1000) return `${Math.round(ms)}ms`;
  const totalSeconds = Math.round(ms / 1000);
  if (totalSeconds < 60) return `${totalSeconds}s`;
  const totalMinutes = Math.floor(totalSeconds / 60);
  const seconds = totalSeconds % 60;
  if (totalMinutes < 60) {
    return seconds > 0 ? `${totalMinutes}分${seconds}秒` : `${totalMinutes}分钟`;
  }
  const hours = Math.floor(totalMinutes / 60);
  const minutes = totalMinutes % 60;
  return minutes > 0 ? `${hours}小时${minutes}分` : `${hours}小时`;
}

/**
 * Relative time for epoch-ms timestamps: `刚刚` (<1min), `n分钟前`,
 * `n小时前`, `昨天`, `n天前` (<30d), otherwise a local date string.
 * Future timestamps (clock skew) collapse to `刚刚`. Empty values render "—".
 */
export function formatRelativeTime(
  ts: number | null | undefined,
  now: number = Date.now(),
): string {
  if (!ts || Number.isNaN(ts)) return "—";
  const diff = now - ts;
  if (diff < 60_000) return "刚刚";
  if (diff < 3600_000) return `${Math.floor(diff / 60_000)}分钟前`;
  if (diff < 24 * 3600_000) return `${Math.floor(diff / 3600_000)}小时前`;
  const days = Math.floor(diff / (24 * 3600_000));
  if (days === 1) return "昨天";
  if (days < 30) return `${days}天前`;
  return new Date(ts).toLocaleDateString("zh-CN");
}

/** Format byte counts: `10.0 MB`, `512 KB`, `18 B`. Zero renders `0 B`. */
export function formatBytes(bytes: number | null | undefined): string {
  if (bytes == null || Number.isNaN(bytes) || bytes < 0) return "—";
  if (bytes < 1024) return `${bytes} B`;
  if (bytes < 1024 * 1024) return `${(bytes / 1024).toFixed(1)} KB`;
  if (bytes < 1024 * 1024 * 1024) return `${(bytes / (1024 * 1024)).toFixed(1)} MB`;
  return `${(bytes / (1024 * 1024 * 1024)).toFixed(2)} GB`;
}

/**
 * Middle-truncate long machine strings (ids, dedupe keys) keeping both ends
 * readable: `asset_history|CN|cn…|none|close`.
 */
export function middleTruncate(value: string, max = 40): string {
  if (value.length <= max) return value;
  const keep = Math.max(4, Math.floor((max - 1) / 2));
  return `${value.slice(0, keep)}…${value.slice(-keep)}`;
}
