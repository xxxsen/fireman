"use client";

import Link from "next/link";
import { useSearchParams } from "next/navigation";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Suspense, useEffect, useMemo, useState } from "react";
import {
  createAdminDirectoryAutoUpdate,
  listAdminAutoUpdateDirectoryUnits,
  listAdminAutoUpdates,
  listAdminWorkerTasks,
  updateAdminAutoUpdate,
} from "@/lib/api/admin";
import type { AdminAutoUpdateRule, AdminPage } from "@/lib/api/admin";
import {
  adjustPolicyLabel,
  formatDateTimeFromMs,
  formatDateTimeFromMsInTimeZone,
  pointTypeLabel,
} from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import { AdminPagination } from "@/components/admin/AdminTable";
import { isTaskActive } from "@/lib/api/tasks";
import { TaskCancelButton } from "@/components/ui/TaskCancelButton";
import { Tooltip } from "@/components/ui/Tooltip";

const HOURS = [1, 6, 12, 24, 48, 72, 168];
function formatInterval(hours: number): string {
  if (hours >= 24) return `${hours / 24} 天`;
  return `${hours} 小时`;
}
const DIRECTORY_QUERY_KEY = ["admin", "auto-updates", "directories"] as const;
const HISTORY_PAGE_SIZE = 50;
const ACTIVE_POLL_MS = 3000;
const IDLE_POLL_MS = 30_000;

export function autoUpdateRulesPollInterval(
  page: AdminPage<AdminAutoUpdateRule> | undefined,
  scanActive: boolean,
): number {
  return scanActive || page?.items.some((rule) => isTaskActive(rule.task?.status))
    ? ACTIVE_POLL_MS
    : IDLE_POLL_MS;
}
function TaskLink({ rule }: { rule: AdminAutoUpdateRule }) {
  if (!rule.last_task_id) return <>--</>;
  return (
    <span className="inline-flex max-w-48 items-center gap-1 overflow-hidden">
      <Link
        className="truncate text-brand hover:text-brand-strong"
        href={`/admin/worker-tasks?task_id=${encodeURIComponent(rule.last_task_id)}`}
        title={rule.last_task_id}
      >
        {rule.last_task_id}
      </Link>
      {rule.task?.status && (
        <span className="shrink-0 text-ink-muted">({rule.task.status})</span>
      )}
    </span>
  );
}

function ruleStatus(rule: AdminAutoUpdateRule): string {
  if (!rule.enabled) return "已暂停";
  if (isTaskActive(rule.task?.status)) {
    return "任务执行中";
  }
  if (rule.task?.status === "canceled") {
    return "最近任务已取消";
  }
  if (rule.task?.status === "failed") {
    return "最近失败";
  }
  if (
    rule.last_failed_at &&
    rule.last_failed_at > (rule.last_success_at ?? 0)
  ) {
    return "最近失败";
  }
  return rule.last_enqueued_at ? "等待下次执行" : "等待执行";
}

function IntervalEditor({
  rule,
  pending,
  onSave,
}: {
  rule: AdminAutoUpdateRule;
  pending: boolean;
  onSave: (hours: number) => Promise<void>;
}) {
  const [draft, setDraft] = useState(rule.interval_hours);
  const changed = draft !== rule.interval_hours;
  return (
    <div className="flex min-w-36 items-center gap-1">
      <select
        aria-label={`${rule.target_label}更新周期`}
        value={draft}
        disabled={pending}
        onChange={(event) => setDraft(Number(event.target.value))}
      >
        {HOURS.map((value) => (
          <option key={value} value={value}>
            {formatInterval(value)}
          </option>
        ))}
      </select>
      {changed && (
        <>
          <button
            type="button"
            disabled={pending}
            className="text-brand disabled:opacity-60"
            onClick={() => void onSave(draft)}
          >
            {pending ? "保存中…" : "保存"}
          </button>
          <button
            type="button"
            disabled={pending}
            className="text-ink-muted disabled:opacity-60"
            onClick={() => setDraft(rule.interval_hours)}
          >
            取消
          </button>
        </>
      )}
    </div>
  );
}

function RuleCells({
  rule,
  pending,
  onUpdate,
  onTaskCanceled,
}: {
  rule: AdminAutoUpdateRule;
  pending: boolean;
  onUpdate: (enabled: boolean, hours: number) => Promise<void>;
  onTaskCanceled: () => void | Promise<void>;
}) {
  return (
    <>
      <td>{ruleStatus(rule)}</td>
      <td className="space-x-2 whitespace-nowrap">
        <IntervalEditor
          key={`${rule.id}-${rule.version}`}
          rule={rule}
          pending={pending}
          onSave={(hours) => onUpdate(rule.enabled, hours)}
        />
      </td>
      <td>
        {rule.next_run_at
          ? formatDateTimeFromMsInTimeZone(rule.next_run_at, "Asia/Shanghai")
          : "--"}
      </td>
      <td>
        {rule.last_success_at
          ? formatDateTimeFromMs(rule.last_success_at)
          : "--"}
      </td>
      <td className="max-w-48">
        {rule.last_error_message ? (
          <Tooltip content={rule.last_error_message}>
            <span className="block truncate">
              {rule.last_failed_at ? formatDateTimeFromMs(rule.last_failed_at) : "--"}
              {rule.last_error_code ? ` (${rule.last_error_code})` : ""}
            </span>
          </Tooltip>
        ) : (
          <span>
            {rule.last_failed_at ? formatDateTimeFromMs(rule.last_failed_at) : "--"}
            {rule.last_error_code ? ` (${rule.last_error_code})` : ""}
          </span>
        )}
      </td>
      <td>
        <TaskLink rule={rule} />
      </td>
      <td>{formatDateTimeFromMs(rule.updated_at)}</td>
      <td>
        <button
          type="button"
          className="text-brand disabled:opacity-60"
          disabled={pending}
          onClick={() => void onUpdate(!rule.enabled, rule.interval_hours)}
        >
          {pending ? "处理中…" : rule.enabled ? "暂停" : "启用"}
        </button>
        <TaskCancelButton
          task={rule.task}
          admin
          label="取消本次任务"
          className="min-h-8 px-2 py-1 text-xs"
          onCanceled={onTaskCanceled}
        />
      </td>
    </>
  );
}

function AutoUpdatesContent() {
  const qc = useQueryClient();
  const searchParams = useSearchParams();
  const [enabled, setEnabled] = useState("");
  const [q, setQ] = useState(() => searchParams.get("q") ?? "");
  const [debouncedQ, setDebouncedQ] = useState(
    () => searchParams.get("q") ?? "",
  );
  const [historyOffset, setHistoryOffset] = useState(0);
  const [error, setError] = useState<string | null>(null);
  const [directoryIntervals, setDirectoryIntervals] = useState<
    Record<string, number>
  >({});
  const [pendingTarget, setPendingTarget] = useState<string | null>(null);
  const historyQueryKey = useMemo(
    () =>
      [
        "admin",
        "auto-updates",
        "asset_history",
        enabled,
        debouncedQ,
        HISTORY_PAGE_SIZE,
        historyOffset,
      ] as const,
    [enabled, debouncedQ, historyOffset],
  );

  useEffect(() => {
    if (q.trim() === debouncedQ) return;
    const timer = window.setTimeout(() => {
      setDebouncedQ(q.trim());
      setHistoryOffset(0);
    }, 300);
    return () => window.clearTimeout(timer);
  }, [q, debouncedQ]);
  const scanTasks = useQuery({
    queryKey: ["admin", "auto-updates", "active-scan"],
    queryFn: () =>
      listAdminWorkerTasks({
        workerType: "go_worker",
        type: "market_data_auto_update_scan",
        status: "active",
        limit: 1,
      }),
    refetchInterval: (query) =>
      query.state.data?.items.some((task) => isTaskActive(task.status))
        ? ACTIVE_POLL_MS
        : IDLE_POLL_MS,
  });
  const scanActive = Boolean(
    scanTasks.data?.items.some((task) => isTaskActive(task.status)),
  );
  const directories = useQuery({
    queryKey: DIRECTORY_QUERY_KEY,
    queryFn: () =>
      listAdminAutoUpdates({ targetType: "directory_unit", limit: 100 }),
    refetchInterval: (query) =>
      autoUpdateRulesPollInterval(query.state.data, scanActive),
  });
  const directoryUnits = useQuery({
    queryKey: ["admin", "auto-updates", "directory-units"],
    queryFn: listAdminAutoUpdateDirectoryUnits,
    staleTime: Infinity,
  });
  const histories = useQuery({
    queryKey: historyQueryKey,
    queryFn: () =>
      listAdminAutoUpdates({
        targetType: "asset_history",
        enabled,
        q: debouncedQ,
        limit: HISTORY_PAGE_SIZE,
        offset: historyOffset,
      }),
    refetchInterval: (query) =>
      autoUpdateRulesPollInterval(query.state.data, scanActive),
  });

  useEffect(() => {
    const total = histories.data?.total;
    if (total == null || total === 0 || historyOffset < total) return;
    const timer = window.setTimeout(() => {
      setHistoryOffset(
        Math.max(
          0,
          Math.floor((total - 1) / HISTORY_PAGE_SIZE) * HISTORY_PAGE_SIZE,
        ),
      );
    }, 0);
    return () => window.clearTimeout(timer);
  }, [histories.data?.total, historyOffset]);

  const refresh = async () => {
    await qc.invalidateQueries({ queryKey: ["admin", "auto-updates"] });
  };
  const updateRule = async (
    rule: AdminAutoUpdateRule,
    active: boolean,
    hours: number,
  ) => {
    try {
      setError(null);
      setPendingTarget(rule.id);
      const updated = await updateAdminAutoUpdate(rule.id, {
        enabled: active,
        interval_hours: hours,
        version: rule.version,
      });
      const replace = (page: AdminPage<AdminAutoUpdateRule> | undefined) =>
        page
          ? {
              ...page,
              items: page.items.map((item) =>
                item.id === updated.id ? updated : item,
              ),
            }
          : page;
      qc.setQueryData(DIRECTORY_QUERY_KEY, replace);
      qc.setQueryData(historyQueryKey, replace);
      await refresh();
    } catch (err) {
      setError(queryErrorMessage(err));
    } finally {
      setPendingTarget(null);
    }
  };
  const enableDirectory = async (syncKey: string) => {
    try {
      setError(null);
      setPendingTarget(syncKey);
      const created = await createAdminDirectoryAutoUpdate({
        sync_key: syncKey,
        interval_hours: directoryIntervals[syncKey] ?? 24,
      });
      qc.setQueryData<AdminPage<AdminAutoUpdateRule>>(
        DIRECTORY_QUERY_KEY,
        (page) => {
          if (!page)
            return { items: [created], total: 1, limit: 100, offset: 0 };
          const exists = page.items.some((item) => item.sync_key === syncKey);
          return {
            ...page,
            total: exists ? page.total : page.total + 1,
            items: exists
              ? page.items.map((item) =>
                  item.sync_key === syncKey ? created : item,
                )
              : [...page.items, created],
          };
        },
      );
      await refresh();
    } catch (err) {
      setError(queryErrorMessage(err));
    } finally {
      setPendingTarget(null);
    }
  };
  const directoryByKey = new Map(
    (directories.data?.items ?? []).map((rule) => [rule.sync_key, rule]),
  );

  return (
    <div className="space-y-7">
      {error && (
        <p role="alert" className="text-sm text-danger">
          {error}
        </p>
      )}
      <section>
        <h2 className="mb-3 text-base font-semibold text-ink">
          资产目录自动更新
        </h2>
        {directories.isError && (
          <p role="alert" className="mb-2 text-sm text-danger">
            目录自动更新配置加载失败：{queryErrorMessage(directories.error)}
          </p>
        )}
        {directoryUnits.isError && (
          <p role="alert" className="mb-2 text-sm text-danger">
            目录清单加载失败：{queryErrorMessage(directoryUnits.error)}
          </p>
        )}
        {directoryUnits.isLoading && (
          <p className="mb-2 text-sm text-ink-muted">加载目录清单…</p>
        )}
        <div className="overflow-x-auto">
          <table className="w-full min-w-[1040px] text-sm">
            <thead>
              <tr>
                <th>目录</th>
                <th>状态</th>
                <th>周期</th>
                <th>下次执行（北京时间）</th>
                <th>最近成功</th>
                <th>最近失败</th>
                <th>任务</th>
                <th>更新时间</th>
                <th>操作</th>
              </tr>
            </thead>
            <tbody>
              {(directoryUnits.data ?? []).map((unit) => {
                const rule = directoryByKey.get(unit.sync_key);
                const interval = directoryIntervals[unit.sync_key] ?? 24;
                const pending = pendingTarget === (rule?.id ?? unit.sync_key);
                return (
                  <tr
                    key={unit.sync_key}
                    data-testid={`directory-rule-${unit.sync_key}`}
                    className="border-t border-line"
                  >
                    <td>
                      {unit.label}
                      <div className="text-xs text-ink-muted">
                        {unit.sync_key}
                      </div>
                    </td>
                    {rule ? (
                      <RuleCells
                        rule={rule}
                        pending={pending}
                        onUpdate={(active, hours) =>
                          updateRule(rule, active, hours)
                        }
                        onTaskCanceled={refresh}
                      />
                    ) : (
                      <>
                        <td>{directories.isLoading ? "加载中…" : "未启用"}</td>
                        <td>
                          <select
                            aria-label={`${unit.label}更新周期`}
                            value={interval}
                            disabled={directories.isLoading || pending}
                            onChange={(event) =>
                              setDirectoryIntervals((current) => ({
                                ...current,
                                [unit.sync_key]: Number(event.target.value),
                              }))
                            }
                          >
                            {HOURS.map((value) => (
                              <option key={value} value={value}>
                                {formatInterval(value)}
                              </option>
                            ))}
                          </select>
                        </td>
                        <td>--</td>
                        <td>--</td>
                        <td>--</td>
                        <td>--</td>
                        <td>--</td>
                        <td>
                          <button
                            type="button"
                            className="text-brand disabled:opacity-60"
                            disabled={
                              directories.isLoading ||
                              directories.isError ||
                              pending
                            }
                            onClick={() => void enableDirectory(unit.sync_key)}
                          >
                            {pending ? "启用中…" : "启用"}
                          </button>
                        </td>
                      </>
                    )}
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section>
        <div className="mb-3 flex flex-wrap items-center gap-2">
          <h2 className="mr-auto text-base font-semibold text-ink">
            资产历史自动更新
          </h2>
          <select
            aria-label="资产规则状态"
            value={enabled}
            onChange={(event) => {
              setEnabled(event.target.value);
              setHistoryOffset(0);
            }}
          >
            <option value="">全部状态</option>
            <option value="true">已启用</option>
            <option value="false">已暂停</option>
            <option value="failed">最近失败</option>
          </select>
          <input
            value={q}
            onChange={(event) => setQ(event.target.value)}
            placeholder="搜索资产代码或名称"
            className="border border-line px-2 py-1"
          />
        </div>
        {histories.isError && (
          <p role="alert" className="mb-2 text-sm text-danger">
            资产历史自动更新配置加载失败：{queryErrorMessage(histories.error)}
          </p>
        )}
        {histories.isLoading ? (
          <p>加载中…</p>
        ) : (histories.data?.items.length ?? 0) === 0 ? (
          <p>未配置资产历史自动更新。</p>
        ) : (
          <>
            <div className="overflow-x-auto">
              <table className="w-full min-w-[1160px] text-sm">
                <thead>
                  <tr>
                    <th>资产</th>
                    <th>口径</th>
                    <th>状态</th>
                    <th>周期</th>
                    <th>下次执行（北京时间）</th>
                    <th>最近成功</th>
                    <th>最近失败</th>
                    <th>任务</th>
                    <th>更新时间</th>
                    <th>操作</th>
                  </tr>
                </thead>
                <tbody>
                  {histories.data?.items.map((rule) => (
                    <tr key={rule.id} className="border-t border-line">
                      <td>{rule.target_label}</td>
                      <td>
                        {adjustPolicyLabel(rule.adjust_policy)} ·{" "}
                        {pointTypeLabel(rule.point_type)}
                      </td>
                      <RuleCells
                        rule={rule}
                        pending={pendingTarget === rule.id}
                        onUpdate={(active, hours) =>
                          updateRule(rule, active, hours)
                        }
                        onTaskCanceled={refresh}
                      />
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <AdminPagination
              total={histories.data?.total ?? 0}
              limit={HISTORY_PAGE_SIZE}
              offset={historyOffset}
              onOffsetChange={setHistoryOffset}
            />
          </>
        )}
      </section>
    </div>
  );
}

export default function AutoUpdatesPage() {
  return (
    <Suspense
      fallback={<p className="text-sm text-ink-muted">加载自动更新配置…</p>}
    >
      <AutoUpdatesContent />
    </Suspense>
  );
}
