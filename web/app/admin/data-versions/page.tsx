"use client";

import { keepPreviousData, useQuery } from "@tanstack/react-query";
import { Suspense } from "react";
import { AdminFilterBar } from "@/components/admin/AdminFilterBar";
import {
  ADMIN_OVERVIEW_POLL_MS,
  ADMIN_OVERVIEW_QUERY_KEY,
} from "@/components/admin/AdminNav";
import {
  AdminPagination,
  AdminTable,
  AdminTableSkeleton,
} from "@/components/admin/AdminTable";
import {
  DATA_VERSION_TABLE_HEADERS,
  DataVersionTableRows,
} from "@/components/admin/DataVersionTable";
import { SyncHealthPanel } from "@/components/admin/SyncHealthPanel";
import { Alert } from "@/components/ui/Alert";
import { Skeleton } from "@/components/ui/Skeleton";
import { getAdminOverview, listAdminDataVersions } from "@/lib/api/admin";
import { ADMIN_PAGE_SIZE, useAdminListParams } from "@/hooks/useAdminListParams";
import { queryErrorMessage } from "@/lib/query-error";

const FILTER_KEYS = ["prefix"] as const;

const PREFIX_OPTIONS = [
  { value: "", label: "全部" },
  { value: "asset_directory", label: "目录同步" },
  { value: "asset_history", label: "历史同步" },
  { value: "fx_rate", label: "汇率同步" },
];

const POLL_MS = 30_000;

// useSearchParams requires a Suspense boundary for static prerendering.
export default function AdminDataVersionsPage() {
  return (
    <Suspense fallback={<AdminTableSkeleton />}>
      <DataVersionsBoard />
    </Suspense>
  );
}

function DataVersionsBoard() {
  const params = useAdminListParams(FILTER_KEYS);
  const prefix = params.get("prefix");

  // Same key as the overview page so the sync-health block shares the cache.
  const overview = useQuery({
    queryKey: ADMIN_OVERVIEW_QUERY_KEY,
    queryFn: getAdminOverview,
    refetchInterval: ADMIN_OVERVIEW_POLL_MS,
  });

  const query = useQuery({
    queryKey: ["admin", "data-versions", { prefix, offset: params.offset }],
    queryFn: () =>
      listAdminDataVersions({
        prefix: prefix || undefined,
        limit: ADMIN_PAGE_SIZE,
        offset: params.offset,
      }),
    placeholderData: keepPreviousData,
    refetchInterval: POLL_MS,
  });

  const page = query.data;
  const stalePollError = query.isError && page !== undefined;

  return (
    <div className="space-y-5" data-testid="admin-data-versions">
      {overview.isLoading ? (
        <Skeleton className="h-40 w-full" />
      ) : overview.data ? (
        <SyncHealthPanel health={overview.data.sync_health} />
      ) : (
        <Alert variant="warning">
          同步健康数据加载失败：{queryErrorMessage(overview.error, "请求失败")}
        </Alert>
      )}

      <div>
        <AdminFilterBar
          selects={[
            {
              id: "prefix",
              label: "版本键分类",
              value: prefix,
              options: PREFIX_OPTIONS,
              onChange: (v) => params.setFilter("prefix", v),
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
          headers={DATA_VERSION_TABLE_HEADERS}
          isLoading={query.isLoading}
          error={
            query.isError && page === undefined
              ? queryErrorMessage(query.error, "数据版本加载失败")
              : null
          }
          onRetry={() => void query.refetch()}
          isEmpty={page !== undefined && page.items.length === 0}
          empty={{
            title: "尚无数据版本记录",
            description: "先在资产页发起一次目录同步。",
            action: { label: "前往资产页", href: "/assets" },
          }}
        >
          {page && <DataVersionTableRows items={page.items} />}
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
    </div>
  );
}
