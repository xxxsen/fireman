/** API fields that must always be arrays, never null. */
const ARRAY_FIELD_NAMES = new Set([
  "instruments",
  "holdings",
  "annual_returns",
  "historical_snapshots",
  "referencing_plans",
  "allocation_bars",
  "top_deviations",
  "data_warnings",
  "checks",
  "lines",
  "weights",
  "asset_class_targets",
  "region_targets",
  "region_bars",
  "asset_class_region_groups",
  "regions",
  "scenarios",
  "tornado",
  "curves",
  "heatmap",
  "points",
  "paths",
  "simulations",
  "stress_tests",
  "sensitivity_tests",
  "selected_years",
  "excluded_years",
  "complete_years",
  "monthly",
  "yearly",
  "monthly_wealth_quantiles",
  "model_warnings",
  "top_parameters",
  "asset_participation",
  "items",
  "years",
]);

/** Recursively replace known null array fields with []. */
export function normalizeApiArrays<T>(value: T): T {
  return normalizeValue(value) as T;
}

function normalizeValue(value: unknown): unknown {
  if (value === null || value === undefined) {
    return value;
  }
  if (Array.isArray(value)) {
    return value.map(normalizeValue);
  }
  if (typeof value === "object") {
    const out: Record<string, unknown> = {};
    for (const [key, child] of Object.entries(value as Record<string, unknown>)) {
      if (child === null && ARRAY_FIELD_NAMES.has(key)) {
        out[key] = [];
      } else {
        out[key] = normalizeValue(child);
      }
    }
    return out;
  }
  return value;
}
