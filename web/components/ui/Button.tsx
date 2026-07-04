"use client";

import Link from "next/link";
import { cn } from "@/lib/cn";

export type ButtonVariant = "primary" | "secondary" | "ghost" | "danger";
export type ButtonSize = "md" | "lg";

const SIZE_CLASSES: Record<ButtonSize, string> = {
  md: "min-h-10 px-4 py-2",
  lg: "min-h-11 px-5 py-2.5",
};

const VARIANT_CLASSES: Record<ButtonVariant, string> = {
  primary:
    "bg-brand text-surface hover:bg-brand-strong focus-visible:ring-focus/40 disabled:bg-brand/50",
  secondary:
    "border border-line bg-surface text-ink hover:bg-surface-muted focus-visible:ring-focus/30 disabled:bg-surface-muted",
  ghost:
    "text-ink-muted hover:bg-surface-muted hover:text-ink focus-visible:ring-focus/30 disabled:text-ink-muted/60",
  danger:
    "border border-danger/30 bg-surface text-danger hover:bg-danger/5 focus-visible:ring-danger/30 disabled:opacity-60",
};

const BASE =
  "inline-flex items-center justify-center gap-2 rounded-md text-sm font-medium transition-colors focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-offset-2 focus-visible:ring-offset-canvas disabled:cursor-not-allowed";

type ButtonCommon = {
  variant?: ButtonVariant;
  size?: ButtonSize;
  pending?: boolean;
  className?: string;
  children: React.ReactNode;
};

type ButtonAsButton = ButtonCommon & {
  href?: undefined;
} & Omit<React.ButtonHTMLAttributes<HTMLButtonElement>, "children">;

type ButtonAsLink = ButtonCommon & {
  href: string;
  disabled?: boolean;
} & Omit<React.AnchorHTMLAttributes<HTMLAnchorElement>, "children" | "href">;

export type ButtonProps = ButtonAsButton | ButtonAsLink;

export function Button(props: ButtonProps) {
  const {
    variant = "primary",
    size = "md",
    pending = false,
    className,
    children,
    ...rest
  } = props;
  const classes = cn(BASE, SIZE_CLASSES[size], VARIANT_CLASSES[variant], className);
  const label = pending ? "处理中…" : children;

  if ("href" in props && props.href) {
    const { href, disabled, onClick, tabIndex, ...linkRest } = rest as ButtonAsLink;
    const isDisabled = disabled || pending;
    return (
      <Link
        href={href}
        className={classes}
        tabIndex={isDisabled ? -1 : tabIndex}
        aria-disabled={isDisabled || undefined}
        aria-busy={pending || undefined}
        onClick={(event) => {
          if (isDisabled) {
            event.preventDefault();
            return;
          }
          onClick?.(event);
        }}
        {...linkRest}
      >
        {label}
      </Link>
    );
  }

  const buttonRest = rest as Omit<ButtonAsButton, keyof ButtonCommon>;
  return (
    <button
      type="button"
      className={classes}
      disabled={buttonRest.disabled || pending}
      aria-busy={pending || undefined}
      {...buttonRest}
    >
      {label}
    </button>
  );
}
