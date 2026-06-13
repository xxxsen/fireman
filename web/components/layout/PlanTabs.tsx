"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/cn";

const TABS = [
  { segment: "overview", label: "组合总览" },
  { segment: "rebalance", label: "调仓工作台" },
  { segment: "holdings", label: "持仓管理" },
  { segment: "settings", label: "计划设置" },
] as const;

export function PlanTabs({
  planId,
  onNavigate,
}: {
  planId: string;
  onNavigate?: (href: string) => boolean;
}) {
  const pathname = usePathname();
  const base = `/plans/${planId}`;

  return (
    <div className="mb-5 overflow-x-auto border-b border-line">
      <nav className="flex gap-0.5" data-testid="plan-tabs" aria-label="计划导航">
        {TABS.map((tab) => {
          const href = `${base}/${tab.segment}`;
          const active = pathname === href || pathname.startsWith(`${href}/`);
          return (
            <Link
              key={tab.segment}
              href={href}
              aria-current={active ? "page" : undefined}
              onClick={(e) => {
                if (onNavigate && !onNavigate(href)) {
                  e.preventDefault();
                }
              }}
              className={cn(
                "whitespace-nowrap border-b-2 px-4 py-2.5 text-sm transition-colors",
                active
                  ? "border-brand font-medium text-brand-strong"
                  : "border-transparent text-ink-muted hover:border-line hover:text-ink",
              )}
            >
              {tab.label}
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
