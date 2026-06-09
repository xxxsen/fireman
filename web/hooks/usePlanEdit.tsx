"use client";

import { createContext, useContext } from "react";

export interface PlanEditContextValue {
  dirty: boolean;
  markDirty: () => void;
  markClean: () => void;
  confirmLeave: () => boolean;
}

export const PlanEditContext = createContext<PlanEditContextValue | null>(null);

export function usePlanEdit() {
  const ctx = useContext(PlanEditContext);
  if (!ctx) throw new Error("usePlanEdit must be used within plan layout");
  return ctx;
}
