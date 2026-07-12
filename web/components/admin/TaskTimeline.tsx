import type {
  AdminTaskHeartbeat,
  AdminTaskTimelinePhase,
} from "@/lib/api/admin";
import { formatDateTimeFromMs } from "@/lib/format";
import { formatDurationMs } from "@/lib/admin-format";
import { cn } from "@/lib/cn";

const PHASE_LABELS: Record<AdminTaskTimelinePhase["phase"], string> = {
  created: "任务创建",
  started: "开始执行",
  pre_complete: "结果上传",
  finished: "执行结束",
};

const FINISH_STATUS_LABELS: Record<string, string> = {
  complete: "同步成功",
  failed: "同步失败",
  canceled: "已取消",
};

export interface TaskTimelineProps {
  timeline: AdminTaskTimelinePhase[];
  heartbeat?: AdminTaskHeartbeat | null;
  /** Whether the task is still running (heartbeat row is only shown then). */
  running?: boolean;
}

/**
 * Vertical execution timeline. Each node shows its absolute time and the
 * interval from the previous node; the backend derives which phases exist.
 */
export function TaskTimeline({
  timeline,
  heartbeat,
  running,
}: TaskTimelineProps) {
  return (
    <ol className="space-y-0" data-testid="task-timeline">
      {timeline.map((node, i) => {
        const prev = i > 0 ? timeline[i - 1] : null;
        const isLast = i === timeline.length - 1 && !(running && heartbeat);
        const failed = node.phase === "finished" && node.status === "failed";
        return (
          <li key={node.phase} className="relative flex gap-3 pb-4 last:pb-0">
            {!isLast && (
              <span
                aria-hidden="true"
                className="absolute left-[5px] top-4 h-full w-px bg-line"
              />
            )}
            <span
              aria-hidden="true"
              className={cn(
                "relative mt-1 h-[11px] w-[11px] shrink-0 rounded-full border-2 border-surface",
                failed ? "bg-danger" : "bg-brand",
              )}
            />
            <div className="min-w-0">
              <p className="text-sm text-ink">
                {PHASE_LABELS[node.phase] ?? node.phase}
                {node.phase === "finished" && node.status && (
                  <span
                    className={cn(
                      "ml-2 text-xs",
                      failed ? "text-danger" : "text-ink-muted",
                    )}
                  >
                    {FINISH_STATUS_LABELS[node.status] ?? node.status}
                  </span>
                )}
              </p>
              <p className="text-xs text-ink-muted">
                {formatDateTimeFromMs(node.at)}
                {prev && node.at >= prev.at && (
                  <span className="ml-2 tabular-nums">
                    +{formatDurationMs(node.at - prev.at)}
                  </span>
                )}
              </p>
            </div>
          </li>
        );
      })}
      {running && heartbeat && (
        <li
          className="relative flex gap-3"
          data-testid="task-timeline-heartbeat"
        >
          <span
            aria-hidden="true"
            className={cn(
              "relative mt-1 h-[11px] w-[11px] shrink-0 animate-pulse rounded-full border-2 border-surface motion-reduce:animate-none",
              heartbeat.stale ? "bg-warning" : "bg-info",
            )}
          />
          <div className="min-w-0">
            <p
              className={cn(
                "text-sm",
                heartbeat.stale ? "text-warning" : "text-ink",
              )}
            >
              {heartbeat.stale ? "心跳滞留，等待回收" : "心跳正常"}
            </p>
            <p className="text-xs text-ink-muted">
              {formatDateTimeFromMs(heartbeat.at)}
            </p>
          </div>
        </li>
      )}
    </ol>
  );
}
