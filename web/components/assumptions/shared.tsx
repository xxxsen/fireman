import { Badge } from "@/components/ui/Badge";
import { assetClassLabel, regionLabel } from "@/lib/format";
import type { AssumptionProfile, AssumptionProfileSummary, AssumptionReturnPrior } from "@/types/api";

export const SCENARIO_LABELS: Record<string, string> = {
  conservative: "保守",
  baseline: "基准",
  optimistic: "乐观",
};

export function scenarioLabel(s: string): string {
  return SCENARIO_LABELS[s] ?? s;
}

export function statusBadge(status: string) {
  switch (status) {
    case "active":
      return <Badge variant="positive">已激活</Badge>;
    case "draft":
      return <Badge variant="info">草稿</Badge>;
    default:
      return <Badge variant="neutral">已弃用</Badge>;
  }
}

export function factorLabel(key: string): string {
  const parts = key.split(":");
  if (parts[0] === "asset" && parts.length === 3) {
    return `${assetClassLabel(parts[1])}·${regionLabel(parts[2])}`;
  }
  if (parts[0] === "fx" && parts.length === 3) {
    return `汇率 ${parts[1]}→${parts[2]}`;
  }
  return key;
}

export function todayISO(): string {
  return new Date().toISOString().slice(0, 10);
}

export interface EditorState {
  profile: AssumptionProfile;
  /** Human-readable origin, shown in the editor header (e.g. 系统默认@1). */
  sourceLabel: string;
  sourceNote: string;
  reviewedBy: string;
  reviewedAt: string;
}

// nextUserProfileId returns a fresh, collision-resistant id for a custom copy.
export function nextUserProfileId(): string {
  return `user_cma_${Math.random().toString(36).slice(2, 7)}`;
}

// maxVersionForId returns the highest stored version for a profile id, so editing
// an existing user profile always saves as a new version (never in place).
export function maxVersionForId(profiles: AssumptionProfileSummary[], id: string): number {
  return profiles.reduce((m, p) => (p.id === id && p.version > m ? p.version : m), 0);
}

// factorUniverse mirrors the backend canonical factor set: one factor per
// distinct non-cash (asset_class, region) return prior and one per FX prior.
// The correlation editor uses it so every pair is a real factor.
export function factorUniverse(p: AssumptionProfile): string[] {
  const set = new Set<string>();
  for (const rp of p.return_priors) {
    if (rp.asset_class !== "cash") set.add(`asset:${rp.asset_class}:${rp.region}`);
  }
  for (const fx of p.fx_priors ?? []) set.add(`fx:${fx.from_currency}:${fx.base_currency}`);
  return [...set].sort();
}

// REQUIRED_BASE_RETURN_CELLS mirrors the backend RequiredGlobalCoverage: these CNY base-currency cells must exist in every active/global profile,
// so the editor forbids deleting them.
export const REQUIRED_BASE_RETURN_CELLS: ReadonlyArray<readonly [string, string]> = [
  ["equity", "domestic"],
  ["equity", "foreign"],
  ["bond", "domestic"],
  ["bond", "foreign"],
  ["cash", "domestic"],
];

export function isRequiredBaseReturnPrior(r: AssumptionReturnPrior): boolean {
  return (
    r.valuation_currency === "CNY" &&
    REQUIRED_BASE_RETURN_CELLS.some(([c, region]) => c === r.asset_class && region === r.region)
  );
}

export function buildCorrelationMatrix(profile: AssumptionProfile): {
  keys: string[];
  matrix: number[][];
} {
  const idx = new Map<string, number>();
  const keys: string[] = [];
  const add = (k: string) => {
    if (!idx.has(k)) {
      idx.set(k, keys.length);
      keys.push(k);
    }
  };
  for (const c of profile.correlation_priors ?? []) {
    add(c.factor_a);
    add(c.factor_b);
  }
  const n = keys.length;
  const matrix: number[][] = Array.from({ length: n }, (_, i) =>
    Array.from({ length: n }, (_, j): number => (i === j ? 1 : 0)),
  );
  for (const c of profile.correlation_priors ?? []) {
    const i = idx.get(c.factor_a)!;
    const j = idx.get(c.factor_b)!;
    if (i === j) continue;
    matrix[i][j] = c.rho;
    matrix[j][i] = c.rho;
  }
  return { keys, matrix };
}
