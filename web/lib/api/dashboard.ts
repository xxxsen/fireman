import type { DashboardView } from "@/types/api";
import { apiGet } from "./client";

export function getDashboard(planId: string): Promise<DashboardView> {
  return apiGet(`/api/v1/plans/${planId}/dashboard`);
}
