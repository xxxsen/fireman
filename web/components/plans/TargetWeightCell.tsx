import { InlineTooltip } from "@/components/ui/InlineTooltip";
import type { RebalanceWorkspaceRow } from "@/lib/allocation-summary";
import { assetClassLabel, formatPercent, regionLabel } from "@/lib/format";
import { helpForTerm } from "@/lib/terms";

type RegionWeightKind = "target" | "current";

function regionWeightTooltip(row: RebalanceWorkspaceRow, kind: RegionWeightKind): string {
  const portfolioWeight =
    kind === "target" ? row.target_weight : row.current_weight;
  const withinParent =
    kind === "target"
      ? row.target_weight_within_parent
      : row.current_weight_within_parent;
  const portfolioHelp =
    helpForTerm(
      kind === "target" ? "target_weight_portfolio" : "current_weight_portfolio",
    ) ?? "占全组合占比";
  const withinClassHelp =
    helpForTerm(
      kind === "target"
        ? "target_weight_within_asset_class"
        : "current_weight_within_asset_class",
    ) ?? "占大类内配比";
  return [
    `${portfolioHelp}：${formatPercent(portfolioWeight)}`,
    `${withinClassHelp}（${assetClassLabel(row.asset_class)} · ${regionLabel(row.region ?? "")}）：${formatPercent(withinParent ?? 0)}`,
  ].join("\n");
}

function RegionWeightCell({
  row,
  kind,
}: {
  row: RebalanceWorkspaceRow;
  kind: RegionWeightKind;
}) {
  const portfolioWeight =
    kind === "target" ? row.target_weight : row.current_weight;
  const withinParent =
    kind === "target"
      ? row.target_weight_within_parent
      : row.current_weight_within_parent;

  if (row.level === "region" && withinParent !== undefined && row.region) {
    return (
      <InlineTooltip content={regionWeightTooltip(row, kind)}>
        {formatPercent(portfolioWeight)}
        <span className="text-ink-muted">
          {" "}
          ({formatPercent(withinParent)})
        </span>
      </InlineTooltip>
    );
  }

  return <>{formatPercent(portfolioWeight)}</>;
}

export function TargetWeightCell({ row }: { row: RebalanceWorkspaceRow }) {
  return <RegionWeightCell row={row} kind="target" />;
}

export function CurrentWeightCell({ row }: { row: RebalanceWorkspaceRow }) {
  return <RegionWeightCell row={row} kind="current" />;
}
