package service

import (
	"context"
	"database/sql"
	"time"

	"github.com/google/uuid"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

func applyUnallocatedGapToCashTx(
	ctx context.Context,
	tx *sql.Tx,
	holdings *repository.HoldingsRepo,
	planID string,
	existing []repository.PlanHolding,
	gapMinor int64,
) error {
	updated := append([]repository.PlanHolding(nil), existing...)
	cashIdx := -1
	for i, h := range updated {
		if h.AssetKey == repository.SystemCashAssetKey {
			cashIdx = i
			break
		}
	}
	now := time.Now().UnixMilli()
	if cashIdx >= 0 {
		updated[cashIdx].CurrentAmountMinor += gapMinor
		updated[cashIdx].Enabled = true
		updated[cashIdx].UpdatedAt = now
	} else {
		updated = append(updated, repository.PlanHolding{
			ID:                   "hold_" + uuid.New().String(),
			PlanID:               planID,
			AssetKey:             repository.SystemCashAssetKey,
			Enabled:              true,
			AssetClass:           domain.AssetClassCash,
			Region:               domain.RegionDomestic,
			WeightWithinGroup:    1.0,
			CurrentAmountMinor:   gapMinor,
			SimulationSnapshotID: repository.SystemCashSnapshotID,
			SortOrder:            9999,
			CreatedAt:            now,
			UpdatedAt:            now,
		})
	}
	return wrapRepo("replace holdings with cash gap", holdings.Replace(ctx, tx, planID, updated))
}
