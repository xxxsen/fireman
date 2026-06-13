package service

import (
	"context"
	"database/sql"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

func applyParametersUpdateInTx(
	ctx context.Context,
	s *PlanService,
	planID string,
	req ParametersUpdateRequest,
	gap int64,
	holds []repository.PlanHolding,
) error {
	return wrapRepo("update plan parameters", fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if req.ApplyUnallocatedToCash && gap > 100 {
			if err := applyUnallocatedGapToCashTx(ctx, tx, s.holdings, planID, holds, gap); err != nil {
				return wrapRepo("apply unallocated gap to cash", err)
			}
		}
		if err := s.params.Upsert(ctx, tx, req.Parameters); err != nil {
			return wrapRepo("upsert plan parameters", err)
		}
		if req.CashFlows != nil {
			if err := s.params.ReplaceCashFlows(ctx, tx, planID, req.CashFlows); err != nil {
				return wrapRepo("replace plan cash flows", err)
			}
		}
		if _, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion); err != nil {
			return wrapRepo("bump plan version", err)
		}
		return nil
	}))
}
