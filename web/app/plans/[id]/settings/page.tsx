"use client";

import { useEffect } from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { PlanTargetsContent } from "@/components/plans/AllocationSettings";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import { ParametersContent } from "../parameters/page";
import { AnalysisContent } from "../analysis/page";

const SECTIONS = [
  { key: "plan-targets", label: "目标配置" },
  { key: "fire-params", label: "FIRE 参数" },
  { key: "simulation", label: "模拟与分析" },
] as const;

type SectionKey = (typeof SECTIONS)[number]["key"];

const LEGACY_SECTION_MAP: Record<string, SectionKey> = {
  scenarios: "plan-targets",
};

function resolveSection(requested: string | null): SectionKey {
  if (requested && LEGACY_SECTION_MAP[requested]) {
    return LEGACY_SECTION_MAP[requested];
  }
  if (requested && SECTIONS.some((item) => item.key === requested)) {
    return requested as SectionKey;
  }
  return "plan-targets";
}

export default function PlanSettingsPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const searchParams = useSearchParams();
  const { confirmLeave, markClean } = usePlanEdit();
  const requested = searchParams.get("section");
  const section = resolveSection(requested);

  useEffect(() => {
    const canonical = resolveSection(requested);
    if (requested !== canonical) {
      router.replace(`/plans/${planId}/settings?section=${canonical}`);
    }
  }, [planId, requested, router]);

  const switchSection = (next: SectionKey) => {
    if (next === section || !confirmLeave()) return;
    markClean();
    router.replace(`/plans/${planId}/settings?section=${next}`);
  };

  const onTablistKeyDown = (event: React.KeyboardEvent<HTMLDivElement>) => {
    if (event.key !== "ArrowLeft" && event.key !== "ArrowRight") return;
    event.preventDefault();
    const index = SECTIONS.findIndex((item) => item.key === section);
    const delta = event.key === "ArrowRight" ? 1 : -1;
    const next = SECTIONS[(index + delta + SECTIONS.length) % SECTIONS.length];
    switchSection(next.key);
    document.getElementById(`settings-tab-${next.key}`)?.focus();
  };

  return (
    <div className="content-enter space-y-6">
      <div
        className="inline-flex max-w-full overflow-x-auto rounded-lg border border-line bg-surface-muted p-1"
        role="tablist"
        aria-label="计划设置分区"
        onKeyDown={onTablistKeyDown}
      >
        {SECTIONS.map((item) => (
          <button
            key={item.key}
            type="button"
            role="tab"
            id={`settings-tab-${item.key}`}
            aria-selected={section === item.key}
            aria-controls={`settings-panel-${item.key}`}
            tabIndex={section === item.key ? 0 : -1}
            className={`min-h-11 whitespace-nowrap rounded-md px-4 text-sm font-medium transition-colors ${
              section === item.key
                ? "bg-surface text-ink shadow-sm"
                : "text-ink-muted hover:text-ink"
            }`}
            onClick={() => switchSection(item.key)}
          >
            {item.label}
          </button>
        ))}
      </div>

      <div
        role="tabpanel"
        id={`settings-panel-${section}`}
        aria-labelledby={`settings-tab-${section}`}
      >
        {section === "plan-targets" && <PlanTargetsContent />}
        {section === "fire-params" && (
          <ParametersContent showAllocation={false} showStale={false} />
        )}
        {section === "simulation" && <AnalysisContent />}
      </div>
    </div>
  );
}
