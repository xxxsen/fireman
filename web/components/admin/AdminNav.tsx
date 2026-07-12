"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { useQuery } from "@tanstack/react-query";
import { getAdminOverview } from "@/lib/api/admin";
import { cn } from "@/lib/cn";

const TABS = [
  { href: "/admin", label: "概览" },
  { href: "/admin/worker-tasks", label: "任务管理" },
  { href: "/admin/finalizations", label: "终结记录" },
  { href: "/admin/data-versions", label: "数据版本" },
  { href: "/admin/auto-updates", label: "自动更新管理" },
] as const;

export const ADMIN_OVERVIEW_QUERY_KEY = ["admin", "overview"] as const;
export const ADMIN_OVERVIEW_POLL_MS = 10_000;

/**
 * Admin console secondary navigation. The worker-tasks tab carries a danger
 * dot when the overview reports recent failures or stale running tasks.
 */
export function AdminNav() {
  const pathname = usePathname();
  const overview = useQuery({
    queryKey: ADMIN_OVERVIEW_QUERY_KEY,
    queryFn: getAdminOverview,
    refetchInterval: ADMIN_OVERVIEW_POLL_MS,
  });

  const workerTaskAlert =
    (overview.data?.worker_tasks.failed_last_24h ?? 0) > 0 ||
    (overview.data?.worker_tasks.stale_running ?? 0) > 0;

  return (
    <div className="mb-5 overflow-x-auto border-b border-line">
      <nav
        className="flex gap-0.5"
        data-testid="admin-nav"
        aria-label="管理后台导航"
      >
        {TABS.map((tab) => {
          const active =
            tab.href === "/admin"
              ? pathname === "/admin"
              : pathname === tab.href || pathname.startsWith(`${tab.href}/`);
          const showDot = tab.href === "/admin/worker-tasks" && workerTaskAlert;
          return (
            <Link
              key={tab.href}
              href={tab.href}
              aria-current={active ? "page" : undefined}
              className={cn(
                "relative whitespace-nowrap border-b-2 px-4 py-2.5 text-sm transition-colors",
                active
                  ? "border-brand font-medium text-brand-strong"
                  : "border-transparent text-ink-muted hover:border-line hover:text-ink",
              )}
            >
              {tab.label}
              {showDot && (
                <span
                  data-testid="admin-nav-alert-dot"
                  aria-label="存在失败或滞留任务"
                  className="absolute right-1.5 top-2 h-1.5 w-1.5 rounded-full bg-danger"
                />
              )}
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
