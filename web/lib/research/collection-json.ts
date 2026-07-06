import type {
  ResearchCollectionDetail,
  ResearchCollectionInput,
  ResearchCollectionItemInput,
  ResearchRebalancePolicy,
  ResearchStartPolicy,
} from "@/lib/api/research";

/** Versioned export document so future format changes stay detectable. */
export interface CollectionExport {
  format: "fireman.research_collection";
  version: 1;
  collection: ResearchCollectionInput;
}

const REBALANCE_POLICIES = new Set([
  "monthly",
  "quarterly",
  "yearly",
  "buy_hold",
  "fixed",
  "threshold",
]);
const START_POLICIES = new Set(["common_intersection", "custom_range"]);

/** Serialize a collection (params + items, no run history) for download. */
export function collectionToJSON(detail: ResearchCollectionDetail): CollectionExport {
  return {
    format: "fireman.research_collection",
    version: 1,
    collection: {
      name: detail.name,
      description: detail.description,
      base_currency: detail.base_currency,
      initial_amount_minor: detail.initial_amount_minor,
      rebalance_policy: detail.rebalance_policy,
      rebalance_threshold: detail.rebalance_threshold,
      start_policy: detail.start_policy,
      window_start: detail.window_start,
      window_end: detail.window_end,
      benchmark_asset_key: detail.benchmark_asset_key || undefined,
      risk_free_rate: detail.risk_free_rate,
      transaction_cost_rate: detail.transaction_cost_rate,
      tags: detail.tags,
      items: detail.items.map((it) => ({
        asset_key: it.asset_key,
        weight: it.weight,
        enabled: it.enabled,
        weight_locked: it.weight_locked,
        adjust_policy: it.adjust_policy,
        point_type: it.point_type,
        asset_class: it.asset_class,
        region: it.region,
        note: it.note,
      })),
    },
  };
}

function str(v: unknown): string | undefined {
  return typeof v === "string" && v !== "" ? v : undefined;
}

function num(v: unknown): number | undefined {
  return typeof v === "number" && Number.isFinite(v) ? v : undefined;
}

function bool(v: unknown): boolean | undefined {
  return typeof v === "boolean" ? v : undefined;
}

function parseItem(raw: unknown): ResearchCollectionItemInput | null {
  if (typeof raw !== "object" || raw === null) return null;
  const o = raw as Record<string, unknown>;
  const assetKey = str(o.asset_key);
  if (!assetKey) return null;
  return {
    asset_key: assetKey,
    weight: num(o.weight),
    enabled: bool(o.enabled),
    weight_locked: bool(o.weight_locked),
    adjust_policy: str(o.adjust_policy),
    point_type: str(o.point_type),
    asset_class: str(o.asset_class),
    region: str(o.region),
    note: str(o.note),
  };
}

/**
 * Parse an exported collection JSON document back into a create request.
 * Throws with a readable message when the document is not usable.
 */
export function parseCollectionJSON(text: string): ResearchCollectionInput {
  let raw: unknown;
  try {
    raw = JSON.parse(text);
  } catch {
    throw new Error("文件不是有效的 JSON。");
  }
  if (typeof raw !== "object" || raw === null) {
    throw new Error("JSON 结构无效。");
  }
  const doc = raw as Record<string, unknown>;
  // Accept either the wrapped export document or a bare collection object.
  const source = (
    doc.format === "fireman.research_collection" && typeof doc.collection === "object"
      ? doc.collection
      : doc
  ) as Record<string, unknown>;

  const name = str(source.name);
  if (!name) {
    throw new Error("缺少集合名称（name）。");
  }
  const rebalance = str(source.rebalance_policy);
  if (rebalance && !REBALANCE_POLICIES.has(rebalance)) {
    throw new Error(`不支持的再平衡策略：${rebalance}`);
  }
  const startPolicy = str(source.start_policy);
  if (startPolicy && !START_POLICIES.has(startPolicy)) {
    throw new Error(`不支持的区间策略：${startPolicy}`);
  }

  const items: ResearchCollectionItemInput[] = [];
  if (Array.isArray(source.items)) {
    for (const rawItem of source.items) {
      const item = parseItem(rawItem);
      if (!item) {
        throw new Error("items 中存在缺少 asset_key 的条目。");
      }
      items.push(item);
    }
  }

  return {
    name,
    description: str(source.description),
    base_currency: str(source.base_currency),
    initial_amount_minor: num(source.initial_amount_minor),
    rebalance_policy: rebalance as ResearchRebalancePolicy | undefined,
    rebalance_threshold: num(source.rebalance_threshold),
    start_policy: startPolicy as ResearchStartPolicy | undefined,
    window_start: str(source.window_start),
    window_end: str(source.window_end),
    benchmark_asset_key: str(source.benchmark_asset_key),
    risk_free_rate: num(source.risk_free_rate),
    transaction_cost_rate: num(source.transaction_cost_rate),
    tags: Array.isArray(source.tags)
      ? source.tags.filter((t): t is string => typeof t === "string")
      : undefined,
    items,
  };
}
