import type { ReactNode } from "react";
import { cn } from "@/lib/cn";

export interface CalculationExplanationProps {
  title?: string;
  summary: ReactNode;
  answer?: ReactNode;
  changed?: ReactNode;
  fixed?: ReactNode;
  data?: ReactNode;
  criterion?: ReactNode;
  uncertainty?: ReactNode;
  nextStep?: ReactNode;
  audit?: ReactNode;
  defaultOpen?: boolean;
  className?: string;
}

type DetailKey =
  | "answer"
  | "changed"
  | "fixed"
  | "data"
  | "criterion"
  | "uncertainty"
  | "nextStep";

const ROWS: Array<[DetailKey, string]> = [
  ["answer", "回答的问题"],
  ["changed", "改变的内容"],
  ["fixed", "保持不变"],
  ["data", "本次数据"],
  ["criterion", "计算与判定"],
  ["uncertainty", "限制与不确定性"],
  ["nextStep", "下一步"],
];

export function CalculationExplanation({
  title = "这次到底计算了什么",
  summary,
  audit,
  defaultOpen = false,
  className,
  ...props
}: CalculationExplanationProps) {
  return (
    <section className={cn("rounded-xl border border-line bg-surface-muted/40 p-4", className)}>
      <h3 className="text-base font-semibold text-ink">{title}</h3>
      <p className="mt-1 text-sm font-medium text-ink">{summary}</p>
      <details className="group mt-3" open={defaultOpen}>
        <summary className="cursor-pointer list-none text-sm font-medium text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40">
          <span className="group-open:hidden">展开详细口径</span>
          <span className="hidden group-open:inline">收起详细口径</span>
        </summary>
        <dl className="mt-3 grid gap-3 text-sm sm:grid-cols-2">
          {ROWS.map(([key, label]) => {
            const value = props[key];
            if (!value) return null;
            return (
              <div key={key}>
                <dt className="font-medium text-ink">{label}</dt>
                <dd className="mt-1 leading-relaxed text-ink-muted">{value}</dd>
              </div>
            );
          })}
        </dl>
        {audit ? (
          <details className="mt-4 border-t border-line pt-3">
            <summary className="cursor-pointer text-xs font-medium text-ink-muted focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40">
              高级计算详情
            </summary>
            <div className="mt-2 break-words text-xs leading-relaxed text-ink-muted">{audit}</div>
          </details>
        ) : null}
      </details>
    </section>
  );
}
