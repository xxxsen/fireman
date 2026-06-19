"use client";

import { Dialog } from "./Dialog";
import { Button, type ButtonVariant } from "./Button";

export interface ConfirmDialogProps {
  open: boolean;
  title: string;
  description?: React.ReactNode;
  confirmLabel?: string;
  cancelLabel?: string;
  variant?: Extract<ButtonVariant, "primary" | "danger">;
  pending?: boolean;
  error?: string | null;
  onConfirm: () => void;
  onClose: () => void;
}

export function ConfirmDialog({
  open,
  title,
  description,
  confirmLabel = "确认",
  cancelLabel = "取消",
  variant = "primary",
  pending = false,
  error,
  onConfirm,
  onClose,
}: ConfirmDialogProps) {
  return (
    <Dialog
      open={open}
      onClose={() => {
        if (pending) return;
        onClose();
      }}
      title={title}
      className="max-w-md"
      footer={
        <div className="flex flex-wrap justify-end gap-2" data-testid="confirm-dialog-footer">
          <Button variant="secondary" disabled={pending} onClick={onClose}>
            {cancelLabel}
          </Button>
          <Button
            variant={variant}
            pending={pending}
            onClick={onConfirm}
            data-testid="confirm-dialog-confirm"
          >
            {confirmLabel}
          </Button>
        </div>
      }
    >
      {description && <div className="text-sm text-ink">{description}</div>}
      {error && (
        <p className="mt-3 text-sm text-danger" role="alert">
          {error}
        </p>
      )}
    </Dialog>
  );
}
