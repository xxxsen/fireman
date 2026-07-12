"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Suspense, useEffect, useState } from "react";
import { AdminFilterBar } from "@/components/admin/AdminFilterBar";
import {
  AdminPagination,
  AdminTable,
  AdminTableSkeleton,
} from "@/components/admin/AdminTable";
import {
  WORKER_TASK_TABLE_HEADERS,
  WorkerTaskTableRows,
} from "@/components/admin/WorkerTaskTable";
import { WorkerTaskDetailDrawer } from "@/components/admin/WorkerTaskDetailDrawer";
import { Alert } from "@/components/ui/Alert";
import { listAdminWorkerTasks, WORKER_TASK_TYPE_LABELS } from "@/lib/api/admin";
import { isTaskActive } from "@/lib/api/market-assets";
import {
  ADMIN_PAGE_SIZE,
  useAdminListParams,
} from "@/hooks/useAdminListParams";
import { queryErrorMessage } from "@/lib/query-error";

const FILTER_KEYS = [
  "worker_type",
  "type",
  "status",
  "scope_type",
  "scope_id",
  "q",
] as const;

const WORKER_OPTIONS = [
  { value: "", label: "全部 Worker" },
  { value: "go_worker", label: "Go Worker" },
  { value: "sidecar_worker", label: "Sidecar Worker" },
];

const TYPE_OPTIONS = [
  { value: "", label: "全部类型" },
  ...Object.entries(WORKER_TASK_TYPE_LABELS).map(([value, label]) => ({
    value,
    label,
  })),
];

const STATUS_OPTIONS = [
  { value: "", label: "全部状态" },
  { value: "active", label: "活跃（含排队）" },
  { value: "pending", label: "等待执行" },
  { value: "running", label: "执行中" },
  { value: "pre_complete", label: "等待终结" },
  { value: "complete", label: "已完成" },
  { value: "failed", label: "执行失败" },
  { value: "canceled", label: "已取消" },
];

const SCOPE_OPTIONS = [
  { value: "", label: "全部范围" },
  { value: "plan", label: "FIRE 计划" },
  { value: "research_collection", label: "研究组合" },
  { value: "market_asset", label: "市场资产" },
  { value: "system", label: "系统" },
];

const SEARCH_DEBOUNCE_MS = 300;
const ACTIVE_POLL_MS = 3000;
const IDLE_POLL_MS = 30_000;

// useSearchParams requires a Suspense boundary for static prerendering.
export default function AdminWorkerTasksPage() {
  return (
    <Suspense fallback={<AdminTableSkeleton />}>
      <WorkerTasksBoard />
    </Suspense>
  );
}

function WorkerTasksBoard() {
  const params = useAdminListParams(FILTER_KEYS);
  const type = params.get("type");
  const workerType = params.get("worker_type");
  const status = params.get("status");
  const scopeType = params.get("scope_type");
  const scopeId = params.get("scope_id");
  const q = params.get("q");
  const selectedTaskId = params.get("task_id") || null;

  const [searchInput, setSearchInput] = useState(q);
  // Adjust the input when the URL q changes externally (reset / navigation),
  // via render-time state adjustment instead of an effect.
  const [lastUrlQ, setLastUrlQ] = useState(q);
  if (lastUrlQ !== q) {
    setLastUrlQ(q);
    setSearchInput(q);
  }
  useEffect(() => {
    if (searchInput === q) return;
    const timer = setTimeout(
      () => params.setFilter("q", searchInput),
      SEARCH_DEBOUNCE_MS,
    );
    return () => clearTimeout(timer);
  }, [searchInput, q, params]);

  const query = useQuery({
    queryKey: [
      "admin",
      "worker-tasks",
      {
        workerType,
        type,
        status,
        scopeType,
        scopeId,
        q,
        offset: params.offset,
      },
    ],
    queryFn: () =>
      listAdminWorkerTasks({
        workerType: workerType || undefined,
        type: type || undefined,
        status: status || undefined,
        scopeType: scopeType || undefined,
        scopeId: scopeId || undefined,
        q: q || undefined,
        limit: ADMIN_PAGE_SIZE,
        offset: params.offset,
      }),
    placeholderData: keepPreviousData,
    refetchInterval: (current) =>
      current.state.data?.items.some((t) => isTaskActive(t.status))
        ? ACTIVE_POLL_MS
        : IDLE_POLL_MS,
  });

  const page = query.data;
  const stalePollError = query.isError && page !== undefined;

  return (
    <div data-testid="admin-worker-tasks">
      <AdminFilterBar
        selects={[
          {
            id: "worker-type",
            label: "Worker",
            value: workerType,
            options: WORKER_OPTIONS,
            onChange: (v) => params.setFilter("worker_type", v),
          },
          {
            id: "type",
            label: "类型",
            value: type,
            options: TYPE_OPTIONS,
            onChange: (v) => params.setFilter("type", v),
          },
          {
            id: "status",
            label: "状态",
            value: status,
            options: STATUS_OPTIONS,
            onChange: (v) => params.setFilter("status", v),
          },
          {
            id: "scope-type",
            label: "范围",
            value: scopeType,
            options: SCOPE_OPTIONS,
            onChange: (v) => params.setFilter("scope_type", v),
          },
        ]}
        search={{
          value: searchInput,
          placeholder: "搜索 task id 前缀或 dedupe_key…",
          onChange: setSearchInput,
        }}
        inputs={[
          {
            id: "scope-id",
            value: scopeId,
            placeholder: "精确筛选 scope id…",
            onChange: (value) => params.setFilter("scope_id", value.trim()),
          },
        ]}
        onReset={params.reset}
        dirty={params.dirty}
      />

      {stalePollError && (
        <Alert variant="warning" className="mb-3">
          刷新失败，正在展示上次数据：
          {queryErrorMessage(query.error, "请求失败")}
        </Alert>
      )}

      <AdminTable
        headers={WORKER_TASK_TABLE_HEADERS}
        isLoading={query.isLoading}
        error={
          query.isError && page === undefined
            ? queryErrorMessage(query.error, "任务列表加载失败")
            : null
        }
        onRetry={() => void query.refetch()}
        isEmpty={page !== undefined && page.items.length === 0}
        empty={{
          title: "没有匹配的任务",
          description: params.dirty
            ? "尝试调整筛选条件。"
            : "发起模拟、研究或市场数据更新后，任务会出现在这里。",
        }}
      >
        {page && (
          <WorkerTaskTableRows
            items={page.items}
            onSelect={(taskId) => params.apply({ task_id: taskId })}
          />
        )}
      </AdminTable>

      {page && (
        <AdminPagination
          total={page.total}
          limit={page.limit}
          offset={page.offset}
          onOffsetChange={params.setOffset}
        />
      )}

      <WorkerTaskDetailDrawer
        taskId={selectedTaskId}
        onClose={() => params.apply({ task_id: null })}
      />
    </div>
  );
}
