"use client";

import { useState } from "react";
import { useQuery } from "@tanstack/react-query";
import { listResearchAssets, type ResearchAssetView } from "@/lib/api/research";
import { instrumentTypeLabel } from "@/lib/format";
import { Dialog } from "@/components/ui/Dialog";
import { Button } from "@/components/ui/Button";
import { LoadingState } from "@/components/ui/LoadingState";
import { QualityBadges } from "@/components/research/qualityBadges";

export interface AddAssetDialogProps {
  open: boolean;
  onClose: () => void;
  existingAssetKeys: Set<string>;
  existingCanonicalFundSymbols: Set<string>;
  onAdd: (asset: ResearchAssetView) => void;
  addPending: boolean;
}

/** Search the asset directory and add entries to the collection. */
export function AddAssetDialog({
  open,
  onClose,
  existingAssetKeys,
  existingCanonicalFundSymbols,
  onAdd,
  addPending,
}: AddAssetDialogProps) {
  const [query, setQuery] = useState("");

  const searchQuery = useQuery({
    queryKey: ["research", "add-asset-search", query],
    queryFn: () => listResearchAssets({ q: query, limit: 20 }),
    enabled: open,
  });

  const assets = searchQuery.data?.assets ?? [];

  return (
    <Dialog
      open={open}
      onClose={onClose}
      title="添加资产"
      className="max-w-2xl"
      footer={
        <div className="flex justify-end">
          <Button variant="secondary" onClick={onClose}>
            完成
          </Button>
        </div>
      }
    >
      <div className="flex h-[min(32rem,70vh)] flex-col gap-3">
        <input
          type="search"
          value={query}
          onChange={(e) => setQuery(e.target.value)}
          placeholder="搜索代码 / 名称（含现金资产）…"
          className="w-full rounded-md border border-line bg-surface px-3 py-2 text-sm text-ink focus:border-brand focus:outline-none"
          data-testid="add-asset-search"
          autoFocus
        />

        <div
          className="min-h-0 flex-1 overflow-y-auto"
          data-testid="add-asset-results"
        >
          {searchQuery.isLoading ? (
            <div className="flex h-full items-center justify-center">
              <LoadingState label="搜索中…" />
            </div>
          ) : assets.length === 0 ? (
            <p className="flex h-full items-center justify-center text-sm text-ink-muted">
              无匹配资产。
            </p>
          ) : (
            <ul className="space-y-1">
              {assets.map((a) => {
                const added = existingAssetKeys.has(a.asset_key);
                const canonicalFundSymbol =
                  a.instrument_type === "cn_mutual_fund"
                    ? a.canonical_symbol || a.symbol
                    : "";
                const sameFundAdded =
                  !added &&
                  canonicalFundSymbol !== "" &&
                  existingCanonicalFundSymbols.has(canonicalFundSymbol);
                return (
                  <li
                    key={a.asset_key}
                    className="flex items-center gap-3 rounded-md border border-line px-3 py-2 text-sm"
                  >
                    <span className="min-w-0 flex-1">
                      <span className="block truncate font-medium text-ink">
                        {a.name}
                      </span>
                      <span className="block text-xs text-ink-muted">
                        {a.symbol} ·{" "}
                        {a.instrument_type_label ||
                          instrumentTypeLabel(a.instrument_type)}{" "}
                        · {a.currency}
                      </span>
                      {(a.fee_mode === "front_end" ||
                        a.fee_mode === "back_end") && (
                        <span className="mt-0.5 block text-xs text-info">
                          {a.fee_mode === "front_end" ? "前端收费" : "后端收费"}{" "}
                          · 净值主代码 {canonicalFundSymbol}
                        </span>
                      )}
                    </span>
                    <QualityBadges badges={a.quality_badges} />
                    <Button
                      variant="secondary"
                      disabled={added || sameFundAdded || addPending}
                      onClick={() => onAdd(a)}
                      data-testid={`add-${a.asset_key}`}
                    >
                      {added
                        ? "已加入"
                        : sameFundAdded
                          ? "同一基金已加入"
                          : "加入"}
                    </Button>
                  </li>
                );
              })}
            </ul>
          )}
        </div>
      </div>
    </Dialog>
  );
}
