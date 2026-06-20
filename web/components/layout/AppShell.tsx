"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { cn } from "@/lib/cn";
import { confirmLeaveIfDirty } from "@/lib/unsavedGuard";

const NAV = [
  { href: "/", label: "计划" },
  { href: "/assets", label: "资产资料库" },
  { href: "/assumptions", label: "模拟假设" },
  { href: "/scenarios", label: "场景配置" },
  { href: "/settings", label: "设置" },
] as const;

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const inPlanModule = pathname === "/" || pathname.startsWith("/plans/");

  const isNavActive = (href: string) => {
    if (href === "/") {
      return inPlanModule;
    }
    return pathname.startsWith(href);
  };

  return (
    <div className="flex min-h-screen bg-canvas">
      <aside
        data-testid="app-sidebar"
        className="hidden w-60 shrink-0 self-start border-r border-line bg-surface/80 p-5 md:sticky md:top-0 md:block md:h-screen md:overflow-y-auto"
        style={{
          backgroundImage:
            "radial-gradient(ellipse at top left, color-mix(in srgb, var(--brand) 6%, transparent), transparent 55%)",
        }}
      >
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
          </nav>
        </header>

        <main className="mx-auto w-full max-w-[1440px] flex-1 p-4 md:p-6">{children}</main>
      </div>
    </div>
  );
}
