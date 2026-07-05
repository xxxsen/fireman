"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Suspense } from "react";
import { AdminFilterBar } from "@/components/admin/AdminFilterBar";
import {
  AdminPagination,
  AdminTable,
  AdminTableSkeleton,
} from "@/components/admin/AdminTable";
import { JOB_TABLE_HEADERS, JobTableRows } from "@/components/admin/JobTable";
import { Alert } from "@/components/ui/Alert";
import { JOB_TYPE_LABELS, listAdminJobs } from "@/lib/api/admin";
import { listPlans } from "@/lib/api/plans";
import { ADMIN_PAGE_SIZE, useAdminListParams } from "@/hooks/useAdminListParams";
import { queryErrorMessage } from "@/lib/query-error";

const FILTER_KEYS = ["type", "status", "plan_id"] as const;

const TYPE_OPTIONS = [
  { value: "", label: "全部类型" },
  ...Object.entries(JOB_TYPE_LABELS).map(([value, label]) => ({ value, label })),
];

const STATUS_OPTIONS = [
  { value: "", label: "全部状态" },
  { value: "active", label: "活跃（排队 + 运行）" },
  { value: "queued", label: "排队中" },
  { value: "running", label: "运行中" },
  { value: "succeeded", label: "已完成" },
  { value: "failed", label: "失败" },
  { value: "canceled", label: "已取消" },
];

const ACTIVE_POLL_MS = 3000;
const IDLE_POLL_MS = 30_000;

// useSearchParams requires a Suspense boundary for static prerendering.
export default function AdminJobsPage() {
  return (
    <Suspense fallback={<AdminTableSkeleton />}>
      <JobsBoard />
    </Suspense>
  );
}

function JobsBoard() {
  const params = useAdminListParams(FILTER_KEYS);
  const type = params.get("type");
  const status = params.get("status");
  const planId = params.get("plan_id");

  const plans = useQuery({ queryKey: ["plans"], queryFn: listPlans });

  const query = useQuery({
    queryKey: ["admin", "jobs", { type, status, planId, offset: params.offset }],
    queryFn: () =>
      listAdminJobs({
        type: type || undefined,
        status: status || undefined,
        planId: planId || undefined,
        limit: ADMIN_PAGE_SIZE,
        offset: params.offset,
      }),
    placeholderData: keepPreviousData,
    refetchInterval: (current) =>
      current.state.data?.items.some((j) => j.status === "queued" || j.status === "running")
        ? ACTIVE_POLL_MS
        : IDLE_POLL_MS,
  });

  const page = query.data;
  const stalePollError = query.isError && page !== undefined;

  return (
    <div data-testid="admin-jobs">
      <AdminFilterBar
        selects={[
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
            id: "plan",
            label: "计划",
            value: planId,
            options: [
              { value: "", label: "全部计划" },
              ...(plans.data ?? []).map((p) => ({ value: p.id, label: p.name })),
            ],
            onChange: (v) => params.setFilter("plan_id", v),
          },
        ]}
        onReset={params.reset}
        dirty={params.dirty}
      />

      {stalePollError && (
        <Alert variant="warning" className="mb-3">
          刷新失败，正在展示上次数据：{queryErrorMessage(query.error, "请求失败")}
        </Alert>
      )}

      <AdminTable
        headers={JOB_TABLE_HEADERS}
        isLoading={query.isLoading}
        error={
          query.isError && page === undefined
            ? queryErrorMessage(query.error, "作业列表加载失败")
            : null
        }
        onRetry={() => void query.refetch()}
        isEmpty={page !== undefined && page.items.length === 0}
        empty={{
          title: "没有匹配的计算作业",
          description: params.dirty
            ? "尝试调整筛选条件。"
            : "在计划页运行一次模拟后，作业会出现在这里。",
        }}
      >
        {page && <JobTableRows items={page.items} />}
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
