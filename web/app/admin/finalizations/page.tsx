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
  FINALIZE_RECORD_TABLE_HEADERS,
  FinalizeRecordTableRows,
} from "@/components/admin/FinalizeRecordTable";
import { Alert } from "@/components/ui/Alert";
import {
  FINALIZE_RESULT_LABELS,
  listAdminFinalizeRecords,
  WORKER_TASK_TYPE_LABELS,
} from "@/lib/api/admin";
import {
  ADMIN_PAGE_SIZE,
  useAdminListParams,
} from "@/hooks/useAdminListParams";
import { queryErrorMessage } from "@/lib/query-error";

const FILTER_KEYS = ["result", "task_type", "task_id"] as const;

const RESULT_OPTIONS = [
  { value: "", label: "全部结果" },
  ...Object.entries(FINALIZE_RESULT_LABELS).map(([value, label]) => ({
    value,
    label,
  })),
];

const TYPE_OPTIONS = [
  { value: "", label: "全部任务类型" },
  ...Object.entries(WORKER_TASK_TYPE_LABELS).map(([value, label]) => ({
    value,
    label,
  })),
];

const SEARCH_DEBOUNCE_MS = 300;
const POLL_MS = 30_000;

// useSearchParams requires a Suspense boundary for static prerendering.
export default function AdminFinalizationsPage() {
  return (
    <Suspense fallback={<AdminTableSkeleton />}>
      <FinalizationsBoard />
    </Suspense>
  );
}

function FinalizationsBoard() {
  const params = useAdminListParams(FILTER_KEYS);
  const result = params.get("result");
  const taskType = params.get("task_type");
  const taskId = params.get("task_id");

  const [taskIdInput, setTaskIdInput] = useState(taskId);
  // Adjust the input when the URL task_id changes externally (reset /
  // navigation), via render-time state adjustment instead of an effect.
  const [lastUrlTaskId, setLastUrlTaskId] = useState(taskId);
  if (lastUrlTaskId !== taskId) {
    setLastUrlTaskId(taskId);
    setTaskIdInput(taskId);
  }
  useEffect(() => {
    if (taskIdInput === taskId) return;
    const timer = setTimeout(
      () => params.setFilter("task_id", taskIdInput.trim()),
      SEARCH_DEBOUNCE_MS,
    );
    return () => clearTimeout(timer);
  }, [taskIdInput, taskId, params]);

  const query = useQuery({
    queryKey: [
      "admin",
      "finalizations",
      { result, taskType, taskId, offset: params.offset },
    ],
    queryFn: () =>
      listAdminFinalizeRecords({
        result: result || undefined,
        taskType: taskType || undefined,
        taskId: taskId || undefined,
        limit: ADMIN_PAGE_SIZE,
        offset: params.offset,
      }),
    placeholderData: keepPreviousData,
    refetchInterval: POLL_MS,
  });

  const page = query.data;
  const stalePollError = query.isError && page !== undefined;

  return (
    <div data-testid="admin-finalizations">
      <p className="mb-4 text-sm text-ink-muted">
        每条记录对应 Go finalizer 的一次业务落库尝试；可重试失败由 Go
        按退避策略继续处理。
      </p>

      <AdminFilterBar
        selects={[
          {
            id: "result",
            label: "结果",
            value: result,
            options: RESULT_OPTIONS,
            onChange: (v) => params.setFilter("result", v),
          },
          {
            id: "task-type",
            label: "任务类型",
            value: taskType,
            options: TYPE_OPTIONS,
            onChange: (v) => params.setFilter("task_type", v),
          },
        ]}
        search={{
          value: taskIdInput,
          placeholder: "按 task id 精确搜索…",
          onChange: setTaskIdInput,
        }}
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
        headers={FINALIZE_RECORD_TABLE_HEADERS}
        isLoading={query.isLoading}
        error={
          query.isError && page === undefined
            ? queryErrorMessage(query.error, "终结记录加载失败")
            : null
        }
        onRetry={() => void query.refetch()}
        isEmpty={page !== undefined && page.items.length === 0}
        empty={{
          title: "没有匹配的终结记录",
          description: params.dirty
            ? "尝试调整筛选条件。"
            : "需要业务落库的任务经过 Go finalizer 处理后，记录会出现在这里。",
        }}
      >
        {page && <FinalizeRecordTableRows items={page.items} />}
      </AdminTable>

      {page && (
        <AdminPagination
          total={page.total}
          limit={page.limit}
          offset={page.offset}
          onOffsetChange={params.setOffset}
        />
      )}
    </div>
  );
}
