"use client";

export interface AdminFilterSelect {
  id: string;
  label: string;
  value: string;
  options: { value: string; label: string }[];
  onChange: (value: string) => void;
}

export interface AdminFilterSearch {
  value: string;
  placeholder: string;
  onChange: (value: string) => void;
}

export interface AdminFilterBarProps {
  selects?: AdminFilterSelect[];
  search?: AdminFilterSearch;
  onReset: () => void;
  /** Whether any filter deviates from its default (enables 重置). */
  dirty: boolean;
}

/**
 * Shared admin filter row: selects + optional search + reset. Values are
 * fully controlled by the page, which mirrors them into the URL query.
 */
export function AdminFilterBar({ selects, search, onReset, dirty }: AdminFilterBarProps) {
  return (
    <div
      className="mb-4 flex flex-wrap items-center gap-2"
      data-testid="admin-filter-bar"
      role="search"
    >
      {selects?.map((sel) => (
        <label key={sel.id} className="flex items-center gap-1.5 text-xs text-ink-muted">
          {sel.label}
          <select
            value={sel.value}
            onChange={(e) => sel.onChange(e.target.value)}
            data-testid={`admin-filter-${sel.id}`}
            className="rounded-md border border-line bg-surface px-2 py-1.5 text-sm text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/30"
          >
            {sel.options.map((opt) => (
              <option key={opt.value} value={opt.value}>
                {opt.label}
              </option>
            ))}
          </select>
        </label>
      ))}
      {search && (
        <input
          type="search"
          value={search.value}
          placeholder={search.placeholder}
          onChange={(e) => search.onChange(e.target.value)}
          data-testid="admin-filter-search"
          className="min-w-52 flex-1 rounded-md border border-line bg-surface px-3 py-1.5 text-sm text-ink placeholder:text-ink-muted/70 focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/30 sm:max-w-xs"
        />
      )}
      {dirty && (
        <button
          type="button"
          onClick={onReset}
          data-testid="admin-filter-reset"
          className="rounded-md px-2.5 py-1.5 text-sm text-ink-muted transition-colors hover:bg-surface-muted hover:text-ink"
        >
          重置
        </button>
      )}
    </div>
  );
}
