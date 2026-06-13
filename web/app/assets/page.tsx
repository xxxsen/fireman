"use client";

import Link from "next/link";
import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { deleteInstrument, listInstruments } from "@/lib/api/instruments";
import { ApiError } from "@/lib/api/client";
import {
  assetClassLabel,
  dataSourceLabel,
  instrumentStatusLabel,
  qualityStatusLabel,
  regionLabel,
} from "@/lib/format";
import { queryErrorMessage } from "@/lib/query-error";
import type { Instrument } from "@/types/api";
import { PageHeader } from "@/components/ui/PageHeader";
import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { LoadingState } from "@/components/ui/LoadingState";
import { Skeleton } from "@/components/ui/Skeleton";
import { Badge, instrumentStatusBadgeVariant } from "@/components/ui/Badge";

type StatusFilter = "all" | "pending_fetch" | "fetch_failed" | "available" | "other";

const STATUS_FILTERS: { value: StatusFilter; label: string }[] = [
  { value: "all", label: "全部" },
  { value: "pending_fetch", label: "抓取中" },
  { value: "fetch_failed", label: "抓取失败" },
  { value: "available", label: "可用" },
  { value: "other", label: "其他" },
];

function matchesStatusFilter(inst: Instrument, filter: StatusFilter): boolean {
  if (filter === "all") return true;
  if (filter === "pending_fetch") return inst.status === "pending_fetch";
  if (filter === "fetch_failed") return inst.status === "fetch_failed";
  if (filter === "available") {
    const q = inst.quality_status ?? inst.status;
    return q === "available" || q === "active";
  }
  const q = inst.quality_status ?? inst.status;
  return (
    inst.status !== "pending_fetch" &&
    inst.status !== "fetch_failed" &&
    q !== "available" &&
    q !== "active"
  );
}

function displayStatusLabel(inst: Instrument): string {
  if (inst.status === "pending_fetch" || inst.status === "fetch_failed") {
    return instrumentStatusLabel(inst.status);
  }
  return qualityStatusLabel(inst.quality_status ?? inst.status);
}

function InstrumentStatusBadge({ inst }: { inst: Instrument }) {
  const statusKey =
    inst.status === "pending_fetch" || inst.status === "fetch_failed"
      ? inst.status
      : (inst.quality_status ?? inst.status);
  return <Badge variant={instrumentStatusBadgeVariant(statusKey)}>{displayStatusLabel(inst)}</Badge>;
}

function InstrumentContextAction({ inst }: { inst: Instrument }) {
  if (inst.status === "pending_fetch") {
    return (
      <Link
        href={`/assets/${inst.id}`}
        className="text-xs text-info underline-offset-2 hover:underline"
      >
        查看进度
      </Link>
    );
  }
  if (inst.status === "fetch_failed") {
    return (
      <Link
        href={`/assets/${inst.id}`}
        className="text-xs text-danger underline-offset-2 hover:underline"
      >
        查看并重试
      </Link>
    );
  }
  return null;
}

function InstrumentDeleteAction({ inst }: { inst: Instrument }) {
  const queryClient = useQueryClient();
  const [error, setError] = useState<string | null>(null);
  const referenced = (inst.referencing_plan_count ?? 0) > 0;

  const deleteMut = useMutation({
    mutationFn: () => deleteInstrument(inst.id),
    onSuccess: () => {
      setError(null);
      void queryClient.invalidateQueries({ queryKey: ["instruments"] });
    },
    onError: (err) =>
      setError(err instanceof ApiError ? err.message : "删除失败"),
  });

  if (inst.is_system) return null;

  return (
    <span className="inline-flex flex-col items-start gap-0.5">
      <button
        type="button"
        className="text-xs text-danger underline-offset-2 hover:underline disabled:cursor-not-allowed disabled:text-ink-muted disabled:no-underline"
        disabled={referenced || deleteMut.isPending}
        title={referenced ? "已被计划引用，无法删除" : undefined}
        data-testid={`instrument-delete-${inst.id}`}
        onClick={() => {
          if (window.confirm(`确定删除 ${inst.name}？`)) {
            deleteMut.mutate();
          }
        }}
      >
        {deleteMut.isPending ? "删除中…" : "删除"}
      </button>
      {referenced && (
        <span className="text-[11px] text-ink-muted">已被计划引用，无法删除</span>
      )}
      {error && <span className="text-[11px] text-danger">{error}</span>}
    </span>
  );
}

function InstrumentRowActions({ inst }: { inst: Instrument }) {
  return (
    <div className="flex flex-col gap-1">
      <InstrumentContextAction inst={inst} />
      <InstrumentDeleteAction inst={inst} />
    </div>
  );
}

function InstrumentCardBody({ inst }: { inst: Instrument }) {
  return (
    <>
      <div className="flex items-start justify-between gap-2">
        <div className="min-w-0">
          <p className="font-mono-numeric text-sm text-ink-muted">{inst.code}</p>
          <p className="mt-0.5 line-clamp-2 font-medium text-ink">{inst.name}</p>
        </div>
        <InstrumentStatusBadge inst={inst} />
      </div>
      <dl className="mt-3 grid grid-cols-2 gap-x-3 gap-y-1 text-xs text-ink-muted">
        <div>
          <dt className="sr-only">市场</dt>
          <dd>{inst.market}</dd>
        </div>
        <div>
          <dt className="sr-only">大类</dt>
          <dd>{assetClassLabel(inst.asset_class)}</dd>
        </div>
        <div>
          <dt className="sr-only">地区</dt>
          <dd>{regionLabel(inst.region)}</dd>
        </div>
        <div>
          <dt className="sr-only">数据来源</dt>
          <dd>{dataSourceLabel(inst.data_source_name)}</dd>
        </div>
      </dl>
      <div className="mt-2">
        <InstrumentRowActions inst={inst} />
      </div>
    </>
  );
}

const instrumentCardClassName =
  "block rounded-lg border border-line bg-surface p-4 transition hover:border-brand/30 hover:shadow-sm";

function InstrumentCard({ inst }: { inst: Instrument }) {
  const hasContextAction =
    inst.status === "pending_fetch" || inst.status === "fetch_failed";

  if (hasContextAction) {
    return (
      <div className={instrumentCardClassName} data-testid="instrument-card">
        <InstrumentCardBody inst={inst} />
      </div>
    );
  }

  return (
    <Link
      href={`/assets/${inst.id}`}
      className={instrumentCardClassName}
      data-testid="instrument-card"
    >
      <InstrumentCardBody inst={inst} />
    </Link>
  );
}

export default function AssetsPage() {
  const [search, setSearch] = useState("");
  const [statusFilter, setStatusFilter] = useState<StatusFilter>("all");

  const { data, isLoading, isError, error, refetch, isFetching } = useQuery({
    queryKey: ["instruments"],
    queryFn: () => listInstruments(),
  });

  const userInstruments = useMemo(
    () => data?.instruments.filter((i) => !i.is_system) ?? [],
    [data],
  );

  const filtered = useMemo(() => {
    const q = search.trim().toLowerCase();
    return userInstruments.filter((inst) => {
      if (!matchesStatusFilter(inst, statusFilter)) return false;
      if (!q) return true;
      return (
        inst.code.toLowerCase().includes(q) ||
        inst.name.toLowerCase().includes(q) ||
        inst.market.toLowerCase().includes(q)
      );
    });
  }, [userInstruments, search, statusFilter]);

  const toolbar = (
    <div className="mb-4 flex flex-col gap-3 sm:flex-row sm:items-center sm:justify-between">
      <div className="flex min-w-0 flex-1 flex-col gap-2 sm:flex-row sm:items-center">
        <input
          type="search"
          value={search}
          onChange={(e) => setSearch(e.target.value)}
          placeholder="搜索代码或名称…"
          className="input-base max-w-md"
          aria-label="搜索资产"
          data-testid="assets-search"
        />
        <select
          value={statusFilter}
          onChange={(e) => setStatusFilter(e.target.value as StatusFilter)}
          className="input-base max-w-xs"
          aria-label="按状态筛选"
          data-testid="assets-status-filter"
        >
          {STATUS_FILTERS.map((opt) => (
            <option key={opt.value} value={opt.value}>
              {opt.label}
            </option>
          ))}
        </select>
      </div>
      {isFetching && !isLoading && data && (
        <LoadingState label="刷新中…" className="text-xs" />
      )}
    </div>
  );

  if (isLoading && !data) {
    return (
      <div className="content-enter">
        <PageHeader
          title="资产资料库"
          description="维护 AKShare 标的资料与市场数据，供各计划持仓引用。"
          primaryAction={{ label: "录入资产", href: "/assets/import" }}
        />
        {toolbar}
        <div className="space-y-2">
          {Array.from({ length: 4 }).map((_, i) => (
            <Skeleton key={i} className="h-12 w-full" />
          ))}
        </div>
      </div>
    );
  }

  if (isError && !data) {
    return (
      <div className="content-enter">
        <PageHeader title="资产资料库" />
        <ErrorState
          message="无法加载资产资料库。请确认后端服务可用后重试。"
          onRetry={() => void refetch()}
          backHref="/"
          technicalDetail={queryErrorMessage(error)}
        />
      </div>
    );
  }

  return (
    <div className="content-enter">
      <PageHeader
        title="资产资料库"
        description="维护 AKShare 标的资料与市场数据，供各计划持仓引用。"
        primaryAction={{ label: "录入资产", href: "/assets/import" }}
      />

      {toolbar}

      {!userInstruments.length ? (
        <EmptyState
          title="资料库为空"
          description="尚未录入任何 AKShare 标的。录入后可被计划持仓引用并自动抓取历史数据。"
          action={{ label: "录入资产", href: "/assets/import" }}
        />
      ) : !filtered.length ? (
        <EmptyState
          title="没有匹配的标的"
          description="尝试调整搜索关键词或状态筛选条件。"
          action={{
            label: "清除筛选",
            onClick: () => {
              setSearch("");
              setStatusFilter("all");
            },
          }}
        />
      ) : (
        <>
          <div className="hidden overflow-x-auto rounded-lg border border-line bg-surface md:block">
            <table className="min-w-full text-sm">
              <thead className="border-b border-line bg-surface-muted/60 text-left text-ink-muted">
                <tr>
                  <th className="px-3 py-2.5 font-medium">代码</th>
                  <th className="px-3 py-2.5 font-medium">名称</th>
                  <th className="px-3 py-2.5 font-medium">市场</th>
                  <th className="px-3 py-2.5 font-medium">大类</th>
                  <th className="px-3 py-2.5 font-medium">地区</th>
                  <th className="px-3 py-2.5 font-medium">数据状态</th>
                  <th className="px-3 py-2.5 font-medium">数据来源</th>
                  <th className="px-3 py-2.5 font-medium">操作</th>
                </tr>
              </thead>
              <tbody>
                {filtered.map((inst) => (
                  <tr key={inst.id} className="border-t border-line hover:bg-surface-muted/40">
                    <td className="px-3 py-2.5">
                      <Link
                        href={`/assets/${inst.id}`}
                        className="font-mono-numeric text-brand underline-offset-2 hover:underline"
                      >
                        {inst.code}
                      </Link>
                    </td>
                    <td className="max-w-[200px] px-3 py-2.5">
                      <Link
                        href={`/assets/${inst.id}`}
                        className="line-clamp-2 text-ink underline-offset-2 hover:text-brand hover:underline"
                      >
                        {inst.name}
                      </Link>
                    </td>
                    <td className="px-3 py-2.5">{inst.market}</td>
                    <td className="px-3 py-2.5">{assetClassLabel(inst.asset_class)}</td>
                    <td className="px-3 py-2.5">{regionLabel(inst.region)}</td>
                    <td className="px-3 py-2.5">
                      <InstrumentStatusBadge inst={inst} />
                    </td>
                    <td className="px-3 py-2.5 text-xs text-ink-muted">
                      {dataSourceLabel(inst.data_source_name)}
                    </td>
                    <td className="px-3 py-2.5">
                      <InstrumentRowActions inst={inst} />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>

          <div className="grid gap-3 md:hidden" data-testid="instrument-cards">
            {filtered.map((inst) => (
              <InstrumentCard key={inst.id} inst={inst} />
            ))}
          </div>
        </>
      )}
    </div>
  );
}
