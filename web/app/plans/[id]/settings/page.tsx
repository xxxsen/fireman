"use client";

import { useEffect } from "react";
import { useParams, useRouter, useSearchParams } from "next/navigation";
import { AllocationSettings } from "@/components/plans/AllocationSettings";
import { usePlanEdit } from "@/hooks/usePlanEdit";
import { ScenariosContent } from "../scenarios/page";
import { ParametersContent } from "../parameters/page";
import { AnalysisContent } from "../analysis/page";

const SECTIONS = [
  { key: "scenarios", label: "场景与权重" },
  { key: "fire-params", label: "FIRE 参数" },
  { key: "simulation", label: "FIRE 模拟" },
] as const;

type SectionKey = (typeof SECTIONS)[number]["key"];

export default function PlanSettingsPage() {
  const planId = useParams().id as string;
  const router = useRouter();
  const searchParams = useSearchParams();
  const { confirmLeave, markClean } = usePlanEdit();
  const requested = searchParams.get("section");
  const returnTo = searchParams.get("return");
  const section: SectionKey = SECTIONS.some((item) => item.key === requested)
    ? (requested as SectionKey)
    : "scenarios";

  useEffect(() => {
    if (!requested || !SECTIONS.some((item) => item.key === requested)) {
      const returnQuery = returnTo ? `&return=${encodeURIComponent(returnTo)}` : "";
      router.replace(`/plans/${planId}/settings?section=scenarios${returnQuery}`);
    }
  }, [planId, requested, returnTo, router]);

  const switchSection = (next: SectionKey) => {
    if (next === section || !confirmLeave()) return;
    markClean();
    const returnQuery = returnTo ? `&return=${encodeURIComponent(returnTo)}` : "";
    router.replace(`/plans/${planId}/settings?section=${next}${returnQuery}`);
  };

  return (
    <div className="space-y-6">
      <div
        className="inline-flex max-w-full overflow-x-auto rounded-lg border border-slate-200 bg-slate-50 p-1"
        role="tablist"
        aria-label="计划设置分区"
      >
        {SECTIONS.map((item) => (
          <button
            key={item.key}
            type="button"
            role="tab"
            aria-selected={section === item.key}
            className={`min-h-11 whitespace-nowrap rounded-md px-4 text-sm font-medium ${
              section === item.key
                ? "bg-white text-slate-900 shadow-sm"
                : "text-slate-600"
            }`}
            onClick={() => switchSection(item.key)}
          >
            {item.label}
          </button>
        ))}
      </div>

      {section === "scenarios" && (
        <div className="space-y-8">
          <AllocationSettings />
          <ScenariosContent />
        </div>
      )}
      {section === "fire-params" && (
        <ParametersContent showAllocation={false} showStale={false} />
      )}
      {section === "simulation" && <AnalysisContent />}
    </div>
  );
}
