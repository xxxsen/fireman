"use client";

import { useEffect, useMemo } from "react";
import { useParams } from "next/navigation";
import { PlanContextBar } from "@/components/layout/PlanContextBar";
import { PlanTabs } from "@/components/layout/PlanTabs";
import { PlanEditContext } from "@/hooks/usePlanEdit";
import { useUnsavedChanges } from "@/hooks/useUnsavedChanges";
import { setRecentPlanId } from "@/lib/recentPlan";

export { usePlanEdit } from "@/hooks/usePlanEdit";

export default function PlanLayout({ children }: { children: React.ReactNode }) {
  const params = useParams();
  const planId = params.id as string;
  const { dirty, markDirty, markClean, confirmLeave } = useUnsavedChanges();

  useEffect(() => {
    setRecentPlanId(planId);
  }, [planId]);

  const value = useMemo(
    () => ({ dirty, markDirty, markClean, confirmLeave }),
    [dirty, markDirty, markClean, confirmLeave],
  );

  return (
    <PlanEditContext.Provider value={value}>
      <PlanContextBar currentPlanId={planId} />
      <PlanTabs
        planId={planId}
        onNavigate={() => confirmLeave()}
      />
      {children}
    </PlanEditContext.Provider>
  );
}
