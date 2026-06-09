const KEY = "fireman:lastPlanId";

export function getRecentPlanId(): string | null {
  if (typeof window === "undefined") return null;
  return localStorage.getItem(KEY);
}

export function setRecentPlanId(planId: string) {
  if (typeof window === "undefined") return;
  localStorage.setItem(KEY, planId);
}
