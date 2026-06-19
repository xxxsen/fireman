package service

import (
	"context"
	"errors"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// ClassificationUpdateRequest is the body for PATCH
// /instruments/:instrument_id/classification.
type ClassificationUpdateRequest struct {
	AssetClass        string `json:"asset_class"`
	Region            string `json:"region"`
	ExpectedUpdatedAt int64  `json:"expected_updated_at"`
}

// ClassificationUpdateResult returns the updated instrument plus enough impact
// metadata for the UI to explain that existing plans keep their frozen copies.
type ClassificationUpdateResult struct {
	Instrument              repository.InstrumentRecord `json:"instrument"`
	ReferencingPlanCount    int                         `json:"referencing_plan_count"`
	ClassificationSyncScope string                      `json:"classification_sync_scope"`
}

// UpdateClassification edits only the library asset_class/region of a non-system
// instrument under optimistic concurrency. It never rewrites plan_holdings,
// snapshots or jobs: existing plans keep the classification frozen at the time
// they referenced the asset (classification_sync_scope = "future_only").
func (s *InstrumentService) UpdateClassification(
	ctx context.Context, id string, req ClassificationUpdateRequest,
) (ClassificationUpdateResult, error) {
	if !isValidAssetClass(req.AssetClass) || !isValidRegion(req.Region) {
		return ClassificationUpdateResult{}, newErr(
			"instrument_classification_unsupported",
			"asset_class must be equity/bond/cash and region must be domestic/foreign",
			nil,
		)
	}
	inst, err := s.instRepo.GetByID(ctx, id)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return ClassificationUpdateResult{}, newErr("instrument_not_found", "instrument not found", nil)
		}
		return ClassificationUpdateResult{}, wrapRepo("load instrument", err)
	}
	if inst.IsSystem {
		return ClassificationUpdateResult{}, newErr(
			"instrument_not_editable", "system instruments cannot be edited", nil)
	}

	if _, err := s.instRepo.UpdateClassification(
		ctx, id, req.AssetClass, req.Region, req.ExpectedUpdatedAt,
	); err != nil {
		switch {
		case errors.Is(err, repository.ErrInstrumentNotFound):
			return ClassificationUpdateResult{}, newErr("instrument_not_found", "instrument not found", nil)
		case errors.Is(err, repository.ErrInstrumentVersionConflict):
			return ClassificationUpdateResult{}, newErr(
				"instrument_version_conflict", "instrument was updated elsewhere; reload before saving", nil)
		default:
			return ClassificationUpdateResult{}, wrapRepo("update instrument classification", err)
		}
	}

	updated, err := s.Get(ctx, id)
	if err != nil {
		return ClassificationUpdateResult{}, err
	}
	holdRepo := repository.NewHoldingsRepo(s.sql)
	refs, err := holdRepo.ListReferencingPlans(ctx, id)
	if err != nil {
		return ClassificationUpdateResult{}, wrapRepo("list referencing plans", err)
	}
	return ClassificationUpdateResult{
		Instrument:              updated,
		ReferencingPlanCount:    len(refs),
		ClassificationSyncScope: "future_only",
	}, nil
}

func isValidAssetClass(v string) bool {
	for _, ac := range domain.AssetClasses {
		if ac == v {
			return true
		}
	}
	return false
}

func isValidRegion(v string) bool {
	for _, r := range domain.Regions {
		if r == v {
			return true
		}
	}
	return false
}
