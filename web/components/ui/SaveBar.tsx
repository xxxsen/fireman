"use client";

import { Button } from "./Button";

interface SaveBarProps {
  dirty: boolean;
  saving?: boolean;
  onSave: () => void;
  error?: string | null;
}

export function SaveBar({ dirty, saving, onSave, error }: SaveBarProps) {
  if (!dirty && !error) return null;
  return (
    <div
      className="sticky bottom-0 z-20 flex items-center justify-between gap-4 border-t border-line bg-surface/95 px-4 py-3 backdrop-blur"
      style={{ paddingBottom: "calc(0.75rem + var(--safe-area-bottom))" }}
    >
      <div className="text-sm text-ink-muted">
        {error ? (
          <span className="text-danger">{error}</span>
        ) : (
          "有未保存的修改"
        )}
      </div>
      <Button onClick={onSave} disabled={saving || !dirty}>
        {saving ? "保存中…" : "保存"}
      </Button>
    </div>
  );
}
