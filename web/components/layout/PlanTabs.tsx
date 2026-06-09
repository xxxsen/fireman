"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";

const TABS = [
  { segment: "dashboard", label: "仪表盘" },
  { segment: "parameters", label: "参数配置" },
  { segment: "scenarios", label: "场景配置" },
  { segment: "instruments", label: "标的配置" },
  { segment: "targets", label: "目标配置" },
  { segment: "rebalance", label: "调仓检查" },
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
    <div className="mb-6 overflow-x-auto border-b border-slate-200">
      <nav className="flex gap-1" data-testid="plan-tabs">
        {TABS.map((tab) => {
          const href = `${base}/${tab.segment}`;
          const active = pathname === href || pathname.startsWith(`${href}/`);
          return (
            <Link
              key={tab.segment}
              href={href}
              onClick={(e) => {
                if (onNavigate && !onNavigate(href)) {
                  e.preventDefault();
                }
              }}
              className={`whitespace-nowrap border-b-2 px-4 py-2 text-sm ${
                active
                  ? "border-slate-900 font-medium text-slate-900"
                  : "border-transparent text-slate-600 hover:text-slate-900"
              }`}
            >
              {tab.label}
            </Link>
          );
        })}
      </nav>
    </div>
  );
}
