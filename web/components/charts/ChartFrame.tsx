import type { ReactNode } from "react";
import { cn } from "@/lib/cn";
import { HelpLabel } from "@/components/ui/HelpLabel";

export interface ChartFrameProps {
  title: ReactNode;
  termKey?: string;
  xAxis: string;
  yAxis: string;
  unit?: string;
  interpretation: ReactNode;
  legend?: ReactNode;
  dataTable?: ReactNode;
  children: ReactNode;
  className?: string;
}

/** Visible semantics and a data fallback shared by canvas and SVG charts. */
export function ChartFrame({
  title,
  termKey,
  xAxis,
  yAxis,
  unit,
  interpretation,
  legend,
  dataTable,
  children,
  className,
}: ChartFrameProps) {
  return (
    <section className={cn("space-y-3", className)}>
      <div>
        <h3 className="text-sm font-semibold text-ink">
          {termKey && typeof title === "string" ? (
            <HelpLabel label={title} termKey={termKey} />
          ) : (
            title
          )}
        </h3>
        <p className="mt-1 text-xs text-ink-muted">
          横轴：{xAxis} · 纵轴：{yAxis}{unit ? `（${unit}）` : ""}
        </p>
      </div>
      {children}
      {legend ? <div className="text-xs text-ink-muted">{legend}</div> : null}
      <p className="text-xs leading-relaxed text-ink-muted">如何解读：{interpretation}</p>
      {dataTable ? (
        <details>
          <summary className="cursor-pointer text-xs font-medium text-primary focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40">
            查看数据表
          </summary>
          <div className="mt-2 overflow-x-auto">{dataTable}</div>
        </details>
      ) : null}
    </section>
  );
}
