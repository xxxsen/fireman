"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/cn";
import { confirmLeaveIfDirty } from "@/lib/unsavedGuard";

const NAV = [
  { href: "/", label: "计划" },
  { href: "/assets", label: "资产" },
  { href: "/assumptions", label: "模拟假设" },
  { href: "/scenarios", label: "配置模板" },
  { href: "/settings", label: "设置" },
] as const;

/**
 * Admin console entry, kept apart from NAV: semantically it is a system
 * observation area, not part of the business navigation.
 */
const ADMIN_NAV_ITEM = { href: "/admin", label: "管理后台" } as const;

function AdminGaugeIcon({ className }: { className?: string }) {
  return (
    <svg
      viewBox="0 0 16 16"
      fill="none"
      stroke="currentColor"
      strokeWidth="1.5"
      strokeLinecap="round"
      aria-hidden="true"
      className={className}
    >
      <path d="M3 5h7" />
      <circle cx="12" cy="5" r="1.5" />
      <path d="M13 11H6" />
      <circle cx="4" cy="11" r="1.5" />
    </svg>
  );
}

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const inPlanModule = pathname === "/" || pathname.startsWith("/plans/");

  const isNavActive = (href: string) => {
    if (href === "/") {
      return inPlanModule;
    }
    return pathname.startsWith(href);
  };

  const adminActive = pathname.startsWith(ADMIN_NAV_ITEM.href);

  return (
    <div className="flex min-h-screen bg-canvas">
      <aside
        data-testid="app-sidebar"
        className="hidden w-60 shrink-0 self-start border-r border-line bg-surface/80 md:sticky md:top-0 md:flex md:h-screen md:flex-col"
        style={{
          backgroundImage:
            "radial-gradient(ellipse at top left, color-mix(in srgb, var(--brand) 6%, transparent), transparent 55%)",
        }}
      >
        <div className="flex-1 overflow-y-auto p-5">
          <Link href="/" className="block">
            <span className="text-lg font-semibold tracking-tight text-brand">Fireman</span>
            <span className="mt-0.5 block text-xs text-ink-muted">FIRE 资产配置工作台</span>
          </Link>
          <nav className="mt-8 space-y-1" aria-label="主导航">
            {NAV.map((item) => {
              const active = isNavActive(item.href);
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={(e) => {
                    if (!confirmLeaveIfDirty()) e.preventDefault();
                  }}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "relative block rounded-md px-3 py-2 text-sm transition-colors",
                    active
                      ? "bg-brand/10 font-medium text-brand-strong before:absolute before:inset-y-1.5 before:left-0 before:w-0.5 before:rounded-full before:bg-brand"
                      : "text-ink-muted hover:bg-surface-muted hover:text-ink",
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
          </nav>
        </div>
        <div className="border-t border-line p-3" data-testid="sidebar-admin-entry">
          <Link
            href={ADMIN_NAV_ITEM.href}
            onClick={(e) => {
              if (!confirmLeaveIfDirty()) e.preventDefault();
            }}
            aria-current={adminActive ? "page" : undefined}
            className={cn(
              "relative flex items-center gap-2 rounded-md px-3 py-2 text-sm transition-colors",
              adminActive
                ? "bg-brand/10 font-medium text-brand-strong before:absolute before:inset-y-1.5 before:left-0 before:w-0.5 before:rounded-full before:bg-brand"
                : "text-ink-muted hover:bg-surface-muted hover:text-ink",
            )}
          >
            <AdminGaugeIcon className="h-4 w-4 shrink-0" />
            {ADMIN_NAV_ITEM.label}
          </Link>
        </div>
      </aside>

      <div className="flex min-w-0 flex-1 flex-col">
        <header className="border-b border-line bg-surface/90 px-4 py-3 backdrop-blur-sm md:hidden">
          <div className="mb-2 flex items-center justify-between">
            <Link href="/" className="text-base font-semibold text-brand">
              Fireman
            </Link>
          </div>
          <nav className="flex gap-1 overflow-x-auto text-sm" aria-label="主导航">
            {NAV.map((item) => {
              const active = isNavActive(item.href);
              return (
                <Link
                  key={item.href}
                  href={item.href}
                  onClick={(e) => {
                    if (!confirmLeaveIfDirty()) e.preventDefault();
                  }}
                  aria-current={active ? "page" : undefined}
                  className={cn(
                    "whitespace-nowrap rounded-full px-3 py-1.5 transition-colors",
                    active
                      ? "bg-brand/10 font-medium text-brand-strong"
                      : "text-ink-muted hover:bg-surface-muted hover:text-ink",
                  )}
                >
                  {item.label}
                </Link>
              );
            })}
            <Link
              href={ADMIN_NAV_ITEM.href}
              onClick={(e) => {
                if (!confirmLeaveIfDirty()) e.preventDefault();
              }}
              aria-current={adminActive ? "page" : undefined}
              data-testid="mobile-admin-entry"
              className={cn(
                "whitespace-nowrap rounded-full px-3 py-1.5 transition-colors",
                adminActive
                  ? "bg-brand/10 font-medium text-brand-strong"
                  : "text-ink-muted hover:bg-surface-muted hover:text-ink",
              )}
            >
              管理
            </Link>
          </nav>
        </header>

        <main className="mx-auto w-full max-w-[1440px] flex-1 p-4 md:p-6">{children}</main>
      </div>
    </div>
  );
}
