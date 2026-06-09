"use client";

interface SaveBarProps {
  dirty: boolean;
  saving?: boolean;
  onSave: () => void;
  error?: string | null;
}

export function SaveBar({ dirty, saving, onSave, error }: SaveBarProps) {
  if (!dirty && !error) return null;
  return (
    <div className="sticky bottom-0 z-20 flex items-center justify-between gap-4 border-t border-slate-200 bg-white/95 px-4 py-3 backdrop-blur">
      <div className="text-sm text-slate-600">
        {error ? (
          <span className="text-red-600">{error}</span>
        ) : (
          "有未保存的修改"
        )}
      </div>
      <button
        type="button"
        onClick={onSave}
        disabled={saving || !dirty}
        className="rounded-md bg-slate-900 px-4 py-2 text-sm font-medium text-white disabled:opacity-50"
      >
        {saving ? "保存中…" : "保存"}
      </button>
    </div>
  );
}
