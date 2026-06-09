"use client";

import { useQuery } from "@tanstack/react-query";
import { listSimulations } from "@/lib/api/simulations";

/** True when any simulation result for the plan is stale (config changed). */
export function usePlanResultStale(planId: string) {
  const simQ = useQuery({
    queryKey: ["simulations", planId],
    queryFn: () => listSimulations(planId),
  });
  const stale = simQ.data?.simulations.some((s) => s.result_stale) ?? false;
  return { stale, isLoading: simQ.isLoading };
}
