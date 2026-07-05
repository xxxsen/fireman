"use client";

import { usePathname, useRouter, useSearchParams } from "next/navigation";
import { useCallback, useMemo } from "react";

export const ADMIN_PAGE_SIZE = 20;

/**
 * URL-synced filter/pagination state for admin list pages. Filters and the
 * page offset live in the query string so refresh, back/forward and shared
 * links all restore the same view.
 */
export function useAdminListParams(filterKeys: readonly string[]) {
  const router = useRouter();
  const pathname = usePathname();
  const searchParams = useSearchParams();

  const get = useCallback((key: string) => searchParams.get(key) ?? "", [searchParams]);

  const rawOffset = Number.parseInt(searchParams.get("offset") ?? "0", 10);
  const offset = Number.isFinite(rawOffset) && rawOffset > 0 ? rawOffset : 0;

  const apply = useCallback(
    (updates: Record<string, string | null>) => {
      const next = new URLSearchParams(searchParams.toString());
      for (const [key, value] of Object.entries(updates)) {
        if (value) next.set(key, value);
        else next.delete(key);
      }
      const qs = next.toString();
      router.replace(qs ? `${pathname}?${qs}` : pathname, { scroll: false });
    },
    [router, pathname, searchParams],
  );

  /** Change a filter; any filter change restarts pagination from page one. */
  const setFilter = useCallback(
    (key: string, value: string) => apply({ [key]: value || null, offset: null }),
    [apply],
  );

  const setOffset = useCallback(
    (value: number) => apply({ offset: value > 0 ? String(value) : null }),
    [apply],
  );

  const reset = useCallback(
    () =>
      apply(
        Object.fromEntries([...filterKeys, "offset"].map((key) => [key, null])),
      ),
    // filterKeys is expected to be a stable module-level constant per page.
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [apply],
  );

  const dirty = filterKeys.some((key) => Boolean(searchParams.get(key)));

  // Stable identity across re-renders with unchanged URL, so effects keyed on
  // this object (e.g. search debounce timers) don't restart spuriously.
  return useMemo(
    () => ({ get, offset, apply, setFilter, setOffset, reset, dirty }),
    [get, offset, apply, setFilter, setOffset, reset, dirty],
  );
}
