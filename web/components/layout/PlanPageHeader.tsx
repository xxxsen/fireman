"use client";

export interface PlanPageHeaderProps {
  title: string;
  description?: React.ReactNode;
  actions?: React.ReactNode;
  /** Extra hint rows rendered under the description (warnings, progress …). */
  children?: React.ReactNode;
}

/**
 * Unified header for plan sub-pages rendered below the plan breadcrumb and
 * tabs (持仓校正、调仓执行、调仓计划 …): one h1 level, optional description and
 * a right-aligned action group.
 */
export function PlanPageHeader({ title, description, actions, children }: PlanPageHeaderProps) {
  return (
    <div
      className="flex flex-wrap items-start justify-between gap-3"
      data-testid="plan-page-header"
    >
      <div className="min-w-0">
        <h1 className="text-xl font-semibold text-ink">{title}</h1>
        {description && <p className="mt-1 text-sm text-ink-muted">{description}</p>}
        {children}
      </div>
      {actions && <div className="flex flex-wrap gap-2">{actions}</div>}
    </div>
  );
}
