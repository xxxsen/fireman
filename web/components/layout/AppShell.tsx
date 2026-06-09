"use client";

import Link from "next/link";
import { usePathname } from "next/navigation";
import { confirmLeaveIfDirty } from "@/lib/unsavedGuard";

const NAV = [
  { href: "/", label: "计划" },
  { href: "/assets", label: "资产资料库" },
  { href: "/settings", label: "设置" },
];

export function AppShell({ children }: { children: React.ReactNode }) {
  const pathname = usePathname();
  const inPlan = pathname.startsWith("/plans/") && !pathname.startsWith("/plans/new");

  return (
    <div className="flex min-h-screen">
      <aside className="hidden w-56 shrink-0 border-r border-slate-200 bg-slate-50 p-4 md:block">
        <Link href="/" className="text-lg font-semibold text-slate-900">
          Fireman
        </Link>
        <nav className="mt-6 space-y-1">
          {NAV.map((item) => {
            const active =
              item.href === "/"
                ? pathname === "/" || inPlan
                : pathname.startsWith(item.href);
            return (
              <Link
                key={item.href}
                href={item.href}
                onClick={(e) => {
                  if (!confirmLeaveIfDirty()) e.preventDefault();
                }}
                className={`block rounded-md px-3 py-2 text-sm ${
                  active
                    ? "bg-white font-medium text-slate-900 shadow-sm"
                    : "text-slate-600 hover:bg-white/60"
                }`}
              >
                {item.label}
              </Link>
            );
          })}
        </nav>
      </aside>
      <div className="flex min-w-0 flex-1 flex-col">
        <header className="border-b border-slate-200 px-4 py-3 md:hidden">
          <div className="flex gap-3 overflow-x-auto text-sm">
            {NAV.map((item) => (
              <Link
                key={item.href}
                href={item.href}
                onClick={(e) => {
                  if (!confirmLeaveIfDirty()) e.preventDefault();
                }}
                className="whitespace-nowrap text-slate-700"
              >
                {item.label}
              </Link>
            ))}
          </div>
        </header>
        <main className="mx-auto w-full max-w-[1440px] flex-1 p-4 md:p-6">{children}</main>
      </div>
    </div>
  );
}
