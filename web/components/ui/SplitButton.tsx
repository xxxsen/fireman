"use client";

import { useEffect, useRef, useState } from "react";
import { cn } from "@/lib/cn";

export interface SplitButtonItem {
  key: string;
  label: string;
  disabled?: boolean;
  /** Small hint rendered next to the label (e.g. 同步中). */
  note?: string;
}

export interface SplitButtonProps {
  /** Main action label. */
  children: React.ReactNode;
  onMain: () => void;
  items: SplitButtonItem[];
  onItem: (key: string) => void;
  /** Disables both halves (e.g. while a request is in flight). */
  disabled?: boolean;
  pending?: boolean;
  className?: string;
  "data-testid"?: string;
}

const HALF_BASE =
  "inline-flex items-center justify-center border border-line bg-surface text-xs font-medium text-ink transition-colors hover:bg-surface-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-focus/30 disabled:cursor-not-allowed disabled:bg-surface-muted disabled:text-ink-muted";

/**
 * Split button: a main action on the left plus a dropdown of unit actions on
 * the right. The menu closes on outside click, Escape or after selecting.
 */
export function SplitButton({
  children,
  onMain,
  items,
  onItem,
  disabled,
  pending,
  className,
  "data-testid": testId,
}: SplitButtonProps) {
  const [open, setOpen] = useState(false);
  const rootRef = useRef<HTMLDivElement | null>(null);

  useEffect(() => {
    if (!open) return;
    const onPointerDown = (event: MouseEvent) => {
      if (rootRef.current && !rootRef.current.contains(event.target as Node)) {
        setOpen(false);
      }
    };
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") setOpen(false);
    };
    document.addEventListener("mousedown", onPointerDown);
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.removeEventListener("mousedown", onPointerDown);
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [open]);

  const blocked = disabled || pending;

  return (
    <div ref={rootRef} className={cn("relative inline-flex", className)} data-testid={testId}>
      <button
        type="button"
        className={cn(HALF_BASE, "min-h-8 rounded-l-md px-3 py-1")}
        disabled={blocked}
        aria-busy={pending || undefined}
        onClick={() => {
          setOpen(false);
          onMain();
        }}
        data-testid={testId ? `${testId}-main` : undefined}
      >
        {pending ? "处理中…" : children}
      </button>
      <button
        type="button"
        className={cn(HALF_BASE, "min-h-8 rounded-r-md border-l-0 px-2 py-1")}
        disabled={blocked}
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label="选择同步单元"
        onClick={() => setOpen((prev) => !prev)}
        data-testid={testId ? `${testId}-toggle` : undefined}
      >
        <span aria-hidden>▾</span>
      </button>
      {open && (
        <ul
          role="menu"
          className="absolute right-0 top-full z-20 mt-1 min-w-48 rounded-md border border-line bg-surface py-1 shadow-lg"
          data-testid={testId ? `${testId}-menu` : undefined}
        >
          {items.map((item) => (
            <li key={item.key} role="none">
              <button
                type="button"
                role="menuitem"
                className="flex w-full items-center justify-between gap-3 px-3 py-1.5 text-left text-xs text-ink hover:bg-surface-muted disabled:cursor-not-allowed disabled:text-ink-muted"
                disabled={item.disabled}
                onClick={() => {
                  setOpen(false);
                  onItem(item.key);
                }}
                data-testid={testId ? `${testId}-item-${item.key}` : undefined}
              >
                <span>{item.label}</span>
                {item.note && <span className="shrink-0 text-ink-muted">{item.note}</span>}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
