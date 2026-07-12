"use client";

import { EmptyState } from "@/components/ui/EmptyState";
import { ErrorState } from "@/components/ui/ErrorState";
import { Skeleton } from "@/components/ui/Skeleton";
import { cn } from "@/lib/cn";

/** Loading placeholder shared by the table shell and page Suspense fallbacks. */
export function AdminTableSkeleton() {
  return (
    <div className="space-y-2" data-testid="admin-table-loading">
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-full" />
      <Skeleton className="h-9 w-full" />
    </div>
  );
}

export interface AdminTableProps {
  headers: React.ReactNode[];
  /** Table body rows (already <tr> elements). */
  children?: React.ReactNode;
  isLoading?: boolean;
  error?: string | null;
  onRetry?: () => void;
  isEmpty?: boolean;
  empty?: {
    title: string;
    description?: string;
    action?: { label: string; href: string };
  };
  className?: string;
}

/**
 * Shared admin table shell: surface card, sticky compact header, horizontal
 * scroll on small screens, and loading/error/empty slots.
 */
export function AdminTable({
  headers,
  children,
  isLoading,
  error,
  onRetry,
  isEmpty,
  empty,
  className,
}: AdminTableProps) {
  if (isLoading) {
    return <AdminTableSkeleton />;
  }
  if (error) {
    return <ErrorState message={error} onRetry={onRetry} />;
  }
  if (isEmpty) {
    return (
      <EmptyState
        title={empty?.title ?? "暂无数据"}
        description={empty?.description}
        action={empty?.action}
      />
    );
  }
  return (
    <div
      className={cn(
        "overflow-x-auto rounded-lg border border-line bg-surface",
        className,
      )}
      data-testid="admin-table"
    >
      <table className="w-full min-w-160 border-collapse text-sm">
        <thead className="sticky top-0 z-10 bg-surface/95 backdrop-blur">
          <tr className="border-b border-line text-left">
            {headers.map((h, i) => (
              <th
                key={i}
                className="whitespace-nowrap px-3 py-2 text-xs font-medium text-ink-muted"
              >
                {h}
              </th>
            ))}
          </tr>
        </thead>
        <tbody>{children}</tbody>
      </table>
    </div>
  );
}

export interface AdminPaginationProps {
  total: number;
  limit: number;
  offset: number;
  onOffsetChange: (offset: number) => void;
}

/** Shared pagination footer: 共 N 条 · 上一页 / 第 x / y 页 / 下一页. */
export function AdminPagination({
  total,
  limit,
  offset,
  onOffsetChange,
}: AdminPaginationProps) {
  if (total <= 0) return null;
  const pageCount = Math.max(1, Math.ceil(total / limit));
  const page = Math.min(pageCount, Math.floor(offset / limit) + 1);
  return (
    <div
      className="mt-3 flex flex-wrap items-center justify-between gap-2 text-sm text-ink-muted"
      data-testid="admin-pagination"
    >
      <span>共 {total} 条</span>
      <div className="flex items-center gap-3">
        <button
          type="button"
          disabled={page <= 1}
          onClick={() => onOffsetChange(Math.max(0, offset - limit))}
          data-testid="admin-page-prev"
          className="rounded-md px-2 py-1 transition-colors hover:bg-surface-muted hover:text-ink disabled:cursor-not-allowed disabled:opacity-40"
        >
          上一页
        </button>
        <span className="tabular-nums">
          第 {page} / {pageCount} 页
        </span>
        <button
          type="button"
          disabled={page >= pageCount}
          onClick={() => onOffsetChange(offset + limit)}
          data-testid="admin-page-next"
          className="rounded-md px-2 py-1 transition-colors hover:bg-surface-muted hover:text-ink disabled:cursor-not-allowed disabled:opacity-40"
        >
          下一页
        </button>
      </div>
    </div>
  );
}
