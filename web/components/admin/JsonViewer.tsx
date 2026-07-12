"use client";

import { useMemo, useState } from "react";

export interface JsonViewerProps {
  label: string;
  /** Raw string as stored (payload_json / result_data). */
  raw: string;
}

/**
 * Collapsed-by-default JSON block for task payload / result envelopes.
 * Parses and pretty-prints on expand; falls back to the raw text when the
 * content is not valid JSON.
 */
export function JsonViewer({ label, raw }: JsonViewerProps) {
  const [copied, setCopied] = useState(false);

  const pretty = useMemo(() => {
    if (!raw.trim()) return "";
    try {
      return JSON.stringify(JSON.parse(raw), null, 2);
    } catch {
      return raw;
    }
  }, [raw]);

  if (!pretty) {
    return (
      <div data-testid="json-viewer-empty" className="text-xs text-ink-muted">
        {label}：空
      </div>
    );
  }

  const copy = async () => {
    try {
      await navigator.clipboard.writeText(raw);
      setCopied(true);
      setTimeout(() => setCopied(false), 1500);
    } catch {
      // Clipboard may be unavailable (permissions/insecure context); ignore.
    }
  };

  return (
    <details
      className="group rounded-md border border-line"
      data-testid="json-viewer"
    >
      <summary className="flex cursor-pointer select-none items-center justify-between gap-2 px-3 py-2 text-xs text-ink-muted transition-colors hover:bg-surface-muted hover:text-ink">
        <span>{label}</span>
        <span className="text-[10px] group-open:hidden">展开</span>
        <span className="hidden text-[10px] group-open:inline">收起</span>
      </summary>
      <div className="border-t border-line">
        <div className="flex justify-end px-2 pt-1.5">
          <button
            type="button"
            onClick={copy}
            data-testid="json-viewer-copy"
            className="rounded px-1.5 py-0.5 text-[10px] text-ink-muted transition-colors hover:bg-surface-muted hover:text-ink"
          >
            {copied ? "已复制" : "复制"}
          </button>
        </div>
        <pre className="max-h-72 overflow-auto px-3 pb-3 font-mono text-xs leading-relaxed text-ink">
          {pretty}
        </pre>
      </div>
    </details>
  );
}
