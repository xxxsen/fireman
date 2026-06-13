import type { Metadata } from "next";
import { QueryProvider } from "@/components/providers/QueryProvider";
import { AppShell } from "@/components/layout/AppShell";
import "./globals.css";

/*
 * Font assets (Noto Sans SC + IBM Plex Mono) will be loaded via next/font/local
 * once WOFF2 subsets are added under web/public/fonts/. Until then, CSS variables
 * --font-sans and --font-mono in globals.css provide fallbacks.
 */

export const metadata: Metadata = {
  title: "Fireman",
  description: "本地优先的 FIRE 资产配置与风险模拟系统",
};

export default function RootLayout({
  children,
}: {
  children: React.ReactNode;
}) {
  return (
    <html lang="zh-CN">
      <body className="min-h-screen bg-canvas text-ink antialiased">
        <QueryProvider>
          <AppShell>{children}</AppShell>
        </QueryProvider>
      </body>
    </html>
  );
}
