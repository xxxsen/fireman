"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/cn";
import { confirmLeaveIfDirty } from "@/lib/unsavedGuard";

const ITEMS = [
  { href: "/research/investment-paths", label: "单资产实验", section: "investment-paths" },
  { href: "/research", label: "组合研究", section: "portfolio" },
] as const;

/** Secondary navigation shared by the two peer data-research capabilities. */
export function ResearchNav() {
  const pathname = usePathname();

  return (
    <aside className="w-full shrink-0 self-start lg:sticky lg:top-6 lg:w-52">
      <div className="rounded-lg border border-line bg-surface p-2">
        <p className="px-3 pb-2 pt-1 text-xs font-semibold uppercase tracking-wide text-ink-muted">
          数据研究
        </p>
        <nav
          className="flex gap-1 overflow-x-auto lg:flex-col"
          aria-label="数据研究导航"
          data-testid="research-nav"
        >
          {ITEMS.map((item) => {
            const inInvestmentPaths = pathname.startsWith("/research/investment-paths");
            const active =
              item.section === "investment-paths"
                ? inInvestmentPaths
                : pathname.startsWith("/research") && !inInvestmentPaths;
            return (
              <Link
                key={item.href}
                href={item.href}
                aria-current={active ? "page" : undefined}
                onClick={(event) => {
                  if (!confirmLeaveIfDirty()) event.preventDefault();
                }}
                className={cn(
                  "relative whitespace-nowrap rounded-md px-3 py-2 text-sm transition-colors",
                  active
                    ? "bg-brand/10 font-medium text-brand-strong lg:before:absolute lg:before:inset-y-1.5 lg:before:left-0 lg:before:w-0.5 lg:before:rounded-full lg:before:bg-brand"
                    : "text-ink-muted hover:bg-surface-muted hover:text-ink",
                )}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
      </div>
    </aside>
  );
}
