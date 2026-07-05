package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// CreatePlanRequest is the payload for creating a plan.
type CreatePlanRequest struct {
	Name               string  `json:"name"`
	BaseCurrency       string  `json:"base_currency"`
	ValuationDate      string  `json:"valuation_date"`
	SelectedScenarioID *string `json:"selected_scenario_id,omitempty"`
}

// UpdatePlanRequest updates plan metadata.
type UpdatePlanRequest struct {
	ConfigVersion int    `json:"config_version"`
	Name          string `json:"name"`
	BaseCurrency  string `json:"base_currency"`
	ValuationDate string `json:"valuation_date"`
	Status        string `json:"status"`
}

// PlanDetail aggregates plan with config hash.
type PlanDetail struct {
	repository.Plan
	ConfigHash string `json:"config_hash"`
}

// ParametersUpdateRequest updates plan FIRE parameters.
type ParametersUpdateRequest struct {
	ConfigVersion          int                       `json:"config_version"`
	Parameters             repository.PlanParameters `json:"parameters"`
	ApplyUnallocatedToCash bool                      `json:"apply_unallocated_to_cash,omitempty"`
}

// PlanService orchestrates plan lifecycle.
type PlanService struct {
	sql       *sql.DB
	plans     *repository.PlanRepo
	params    *repository.ParametersRepo
	alloc     *repository.AllocationRepo
	scenario  *repository.ScenarioRepo
	holdings  *repository.HoldingsRepo
	assetRepo *repository.MarketAssetRepo
	hash      *ConfigHashService
	snapSvc   *marketdata.SnapshotService
}

func NewPlanService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	scenario *repository.ScenarioRepo,
	holdings *repository.HoldingsRepo,
	assetRepo *repository.MarketAssetRepo,
	hash *ConfigHashService,
	snapSvc *marketdata.SnapshotService,
) *PlanService {
	return &PlanService{
		sql: sqlDB, plans: plans, params: params, alloc: alloc, scenario: scenario,
		holdings: holdings, assetRepo: assetRepo, hash: hash, snapSvc: snapSvc,
	}
}

func defaultParameters(planID string, scenarioID *string) repository.PlanParameters {
	return repository.PlanParameters{
		PlanID:                   planID,
		CurrentAge:               35,
		RetirementAge:            55,
		EndAge:                   90,
		TotalAssetsMinor:         1_000_000_00,
		AnnualSavingsMinor:       200_000_00,
		AnnualSavingsGrowthRate:  0,
		AnnualSpendingMinor:      400_000_00,
		TerminalWealthFloorMinor: 0,
		SelectedScenarioID:       scenarioID,
		InflationMode:            "fixed_real",
		FixedInflationRate:       0.03,
		InflationMu:              0.03,
		InflationPhi:             0.5,
		InflationSigma:           0.01,
		WithdrawalType:           "fixed_real",
		WithdrawalRate:           0.04,
		WithdrawalFloorRatio:     0.70,
		WithdrawalCeilingRatio:   1.30,
		WithdrawalTaxRate:        0,
		TaxableWithdrawalRatio:   0,
		RebalanceFrequency:       "annual",
		RebalanceThreshold:       0.03,
		TransactionCostRate:      0,
		SimulationRuns:           10000,
		StudentTDf:               repository.DefaultStudentTDf,
		// New plans default to the forward-looking,
		// auditable blended_prior calibration with the baseline scenario, following
		// the user's global profile. Existing plans were migrated to historical_cagr
		// and keep their old numbers until the user explicitly switches.
		ReturnAssumptionMode:     repository.ModeBlendedPrior,
		AssumptionSelectionMode:  repository.DefaultAssumptionSelectionMode,
		ReturnAssumptionScenario: repository.DefaultReturnAssumptionScenario,
	}
}

func defaultRegionTargets() []repository.RegionTarget {
	out := make([]repository.RegionTarget, 0, len(domain.Regions)*len(domain.AssetClasses))
	for _, ac := range domain.AssetClasses {
		for _, region := range domain.Regions {
			w := 0.0
			if region == domain.RegionDomestic {
				w = 1.0
			}
			out = append(out, repository.RegionTarget{
				AssetClass: ac, Region: region, WeightWithinClass: w,
			})
		}
	}
	return out
}

func (s *PlanService) Create(ctx context.Context, req CreatePlanRequest) (PlanDetail, error) {
	if req.Name == "" || req.ValuationDate == "" {
		return PlanDetail{}, newErr("validation_failed", "name and valuation_date are required", nil)
	}
	if req.BaseCurrency == "" {
		req.BaseCurrency = "CNY"
	}
	if err := validateBaseCurrency(req.BaseCurrency); err != nil {
		return PlanDetail{}, newErr("validation_failed", err.Error(), nil)
	}
	planID := "plan_" + uuid.New().String()
	now := time.Now().UnixMilli()
	plan := repository.Plan{
		ID: planID, Name: req.Name, BaseCurrency: req.BaseCurrency,
		ValuationDate: req.ValuationDate, Status: "active", ConfigVersion: 1,
		CreatedAt: now, UpdatedAt: now,
	}
	params := defaultParameters(planID, req.SelectedScenarioID)
	if err := validateParameters(params); err != nil {
		return PlanDetail{}, newErr("parameters_invalid", err.Error(), nil)
	}
	if err := validatePinnedProfileActive(ctx, repository.NewAssumptionProfileRepo(s.sql), params); err != nil {
		return PlanDetail{}, newErr("parameters_invalid", err.Error(), nil)
	}

	alloc, err := initialPlanAllocation(ctx, s, req.SelectedScenarioID)
	if err != nil {
		return PlanDetail{}, err
	}

	if err := s.plans.Create(ctx, plan); err != nil {
		return PlanDetail{}, wrapRepo("create plan", err)
	}
	params.PlanID = planID
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.params.Upsert(ctx, tx, params); err != nil {
			return wrapRepo("upsert plan parameters", err)
		}
		return wrapRepo("replace plan allocation", s.alloc.Replace(ctx, tx, planID, alloc))
	})
	if err != nil {
		return PlanDetail{}, wrapRepo("initialize plan", err)
	}
	return s.Get(ctx, planID)
}

func (s *PlanService) List(ctx context.Context) ([]PlanDetail, error) {
	plans, err := s.plans.List(ctx)
	if err != nil {
		return nil, wrapRepo("list plans", err)
	}
	out := make([]PlanDetail, 0, len(plans))
	for _, p := range plans {
		hash, _ := s.hash.Compute(ctx, p.ID)
		out = append(out, PlanDetail{Plan: p, ConfigHash: hash})
	}
	return out, nil
}

func (s *PlanService) Get(ctx context.Context, planID string) (PlanDetail, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return PlanDetail{}, newErr("plan_not_found", "plan not found", nil)
		}
		return PlanDetail{}, wrapRepo("load plan", err)
	}
	hash, err := s.hash.Compute(ctx, planID)
	if err != nil {
		return PlanDetail{}, wrapRepo("compute config hash", err)
	}
	return PlanDetail{Plan: plan, ConfigHash: hash}, nil
}

func (s *PlanService) Update(ctx context.Context, planID string, req UpdatePlanRequest) (PlanDetail, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return PlanDetail{}, newErr("plan_not_found", "plan not found", nil)
		}
		return PlanDetail{}, wrapRepo("load plan", err)
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return PlanDetail{}, newErr("plan_version_conflict", "plan configuration version mismatch", map[string]any{
			"expected": plan.ConfigVersion, "provided": req.ConfigVersion,
		})
	}
	if req.Name != "" {
		plan.Name = req.Name
	}
	if req.BaseCurrency != "" {
		if err := validateBaseCurrency(req.BaseCurrency); err != nil {
			return PlanDetail{}, newErr("validation_failed", err.Error(), nil)
		}
		plan.BaseCurrency = req.BaseCurrency
	}
	if req.ValuationDate != "" {
		plan.ValuationDate = req.ValuationDate
	}
	if req.Status != "" {
		plan.Status = req.Status
	}
	if err := s.plans.Update(ctx, plan, req.ConfigVersion); err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return PlanDetail{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return PlanDetail{}, wrapRepo("update plan", err)
	}
	return s.Get(ctx, planID)
}

func (s *PlanService) Delete(ctx context.Context, planID string) error {
	if err := s.plans.Delete(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return newErr("plan_not_found", "plan not found", nil)
		}
		return wrapRepo("delete plan", err)
	}
	return nil
}

// GetParameters returns plan FIRE parameters.
func (s *PlanService) GetParameters(ctx context.Context, planID string) (repository.PlanParameters, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PlanParameters{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PlanParameters{}, wrapRepo("load plan", err)
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return repository.PlanParameters{}, wrapRepo("load plan parameters", err)
	}
	return params, nil
}

func (s *PlanService) UpdateParameters(ctx context.Context, planID string,
	req ParametersUpdateRequest,
) (repository.PlanParameters, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PlanParameters{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PlanParameters{}, wrapRepo("load plan", err)
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return repository.PlanParameters{}, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}
	req.Parameters.PlanID = planID
	// student_t_df is a read-only legacy field: forward runs freeze the profile df,
	// so an API caller must not be able to change the plan value (which would churn
	// the config hash and mark runs stale for no modeling effect). Preserve the
	// stored value and ignore whatever the client sent.
	if existing, perr := s.params.Get(ctx, planID); perr == nil {
		req.Parameters.StudentTDf = existing.StudentTDf
	}
	if err := validateParameters(req.Parameters); err != nil {
		return repository.PlanParameters{}, newErr("parameters_invalid", err.Error(), nil)
	}
	if err := validatePinnedProfileActive(
		ctx, repository.NewAssumptionProfileRepo(s.sql), req.Parameters,
	); err != nil {
		return repository.PlanParameters{}, newErr("parameters_invalid", err.Error(), nil)
	}

	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return repository.PlanParameters{}, wrapRepo("list plan holdings", err)
	}
	if err := validateParametersAssetsGap(req.Parameters, holds, req.ApplyUnallocatedToCash); err != nil {
		return repository.PlanParameters{}, err
	}
	gap := req.Parameters.TotalAssetsMinor - enabledHoldingsSum(holds)

	err = applyParametersUpdateInTx(ctx, s, planID, req, gap, holds)
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return repository.PlanParameters{}, newErr(
				"plan_version_conflict",
				"plan configuration version mismatch",
				nil,
			)
		}
		return repository.PlanParameters{}, wrapRepo("update plan parameters", err)
	}
	return s.GetParameters(ctx, planID)
}

// CreatePortfolioSnapshot records holdings as a snapshot and optionally updates current amounts.
type CreatePortfolioSnapshotRequest struct {
	SnapshotDate   string                             `json:"snapshot_date"`
	Note           string                             `json:"note"`
	Items          []repository.PortfolioSnapshotItem `json:"items"`
	UpdateHoldings bool                               `json:"update_holdings"`
}

func (s *PlanService) CreatePortfolioSnapshot(ctx context.Context, planID string,
	req CreatePortfolioSnapshotRequest,
) (repository.PortfolioSnapshot, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PortfolioSnapshot{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PortfolioSnapshot{}, wrapRepo("load plan", err)
	}
	if req.SnapshotDate == "" {
		return repository.PortfolioSnapshot{}, newErr("validation_failed", "snapshot_date is required", nil)
	}
	var total int64
	for _, it := range req.Items {
		total += it.AmountMinor
	}
	snap := repository.PortfolioSnapshot{
		ID: "psnap_" + uuid.New().String(), PlanID: planID,
		SnapshotDate: req.SnapshotDate, TotalAmountMinor: total,
		Note: req.Note, Items: req.Items,
	}
	snapRepo := repository.NewPortfolioSnapshotRepo(s.sql)
	if req.UpdateHoldings {
		err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
			if err := snapRepo.CreateTx(ctx, tx, snap); err != nil {
				return wrapRepo("create portfolio snapshot", err)
			}
			if err := s.holdings.UpdateCurrentAmountsTx(ctx, tx, planID, req.Items); err != nil {
				return wrapRepo("update holding amounts", err)
			}
			_, err := s.plans.BumpVersionTx(ctx, tx, planID, plan.ConfigVersion)
			return wrapRepo("bump plan version", err)
		})
	} else if err = snapRepo.Create(ctx, snap); err != nil {
		return repository.PortfolioSnapshot{}, fmt.Errorf("create snapshot: %w", err)
	}
	if err != nil {
		return repository.PortfolioSnapshot{}, fmt.Errorf("create snapshot: %w", err)
	}
	return snap, nil
}
