"use client";

import type { ReactNode } from "react";
import { helpTopicForTerm, type HelpTopic } from "@/lib/terms";
import { cn } from "@/lib/cn";
import { Tooltip } from "./Tooltip";

export interface MetricHelpProps {
  termKey?: string;
  text?: string;
  label?: ReactNode;
  className?: string;
}

const DETAIL_LABELS: Array<[keyof HelpTopic, string]> = [
  ["purpose", "作用"],
  ["calculation", "计算"],
  ["inputs", "数据"],
  ["interpretation", "解读"],
  ["caveat", "注意"],
];

export function HelpTopicContent({ topic }: { topic: HelpTopic }) {
  return (
    <span className="block select-text space-y-2 text-left">
      <span className="block font-medium text-ink">{topic.summary}</span>
      {DETAIL_LABELS.map(([key, label]) => {
        const value = topic[key];
        if (!value) return null;
        return (
          <span className="block" key={key}>
            <span className="font-medium text-ink">{label}：</span>
            <span className="text-ink-muted">{value}</span>
          </span>
        );
      })}
    </span>
  );
}

export function MetricHelp({ termKey, text, label, className }: MetricHelpProps) {
  const topic = helpTopicForTerm(termKey);

  if (termKey && !topic && process.env.NODE_ENV !== "production") {
    throw new Error(`Unknown help topic: ${termKey}`);
  }

  const content = text ?? (topic ? <HelpTopicContent topic={topic} /> : undefined);

  if (!content) return null;

  return (
    <Tooltip
      content={content}
      align="center"
      clickToggle
      triggerTestId="metric-help-trigger"
      contentTestId="metric-help-tooltip"
      contentClassName="w-[min(24rem,calc(100vw-1rem))] max-w-[calc(100vw-1rem)] p-3"
    >
      <span
        role="button"
        tabIndex={0}
        aria-label={topic ? `查看「${topic.label}」说明` : "查看说明"}
        onKeyDown={(event) => {
          if (event.key !== "Enter" && event.key !== " ") return;
          event.preventDefault();
          event.currentTarget.click();
        }}
        className={cn(
          "ml-0.5 inline-flex h-8 w-8 shrink-0 items-center justify-center rounded-full text-xs text-ink-muted transition-colors hover:bg-surface-muted hover:text-ink focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-primary/40",
          className,
        )}
      >
        <span className="inline-flex h-4 w-4 items-center justify-center rounded-full border border-line bg-surface text-[10px]">
          {label ?? "?"}
        </span>
      </span>
    </Tooltip>
  );
}
