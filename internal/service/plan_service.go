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

// ParametersUpdateRequest updates parameters and optional cash flows.
type ParametersUpdateRequest struct {
	ConfigVersion          int                       `json:"config_version"`
	Parameters             repository.PlanParameters `json:"parameters"`
	CashFlows              []repository.PlanCashFlow `json:"cash_flows,omitempty"`
	ApplyUnallocatedToCash bool                      `json:"apply_unallocated_to_cash,omitempty"`
}

// PlanService orchestrates plan lifecycle.
type PlanService struct {
	sql        *sql.DB
	plans      *repository.PlanRepo
	params     *repository.ParametersRepo
	alloc      *repository.AllocationRepo
	scenario   *repository.ScenarioRepo
	holdings   *repository.HoldingsRepo
	instRepo   *repository.InstrumentRepo
	hash       *ConfigHashService
	snapSvc    *marketdata.SnapshotService
	marketRepo *repository.MarketDataRepo
}

func NewPlanService(
	sqlDB *sql.DB,
	plans *repository.PlanRepo,
	params *repository.ParametersRepo,
	alloc *repository.AllocationRepo,
	scenario *repository.ScenarioRepo,
	holdings *repository.HoldingsRepo,
	instRepo *repository.InstrumentRepo,
	hash *ConfigHashService,
	snapSvc *marketdata.SnapshotService,
	marketRepo *repository.MarketDataRepo,
) *PlanService {
	return &PlanService{
		sql: sqlDB, plans: plans, params: params, alloc: alloc, scenario: scenario,
		holdings: holdings, instRepo: instRepo, hash: hash, snapSvc: snapSvc, marketRepo: marketRepo,
	}
}

func defaultParameters(planID string, scenarioID *string) repository.PlanParameters {
	return repository.PlanParameters{
		PlanID:                   planID,
		CurrentAge:               30,
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
		StudentTDf:               7,
	}
}

func defaultRegionTargets() []repository.RegionTarget {
	var out []repository.RegionTarget
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

	var alloc repository.PlanAllocation
	if req.SelectedScenarioID != nil {
		scn, err := s.scenario.GetByID(ctx, *req.SelectedScenarioID)
		if err != nil {
			if errors.Is(err, repository.ErrScenarioNotFound) {
				return PlanDetail{}, newErr("scenario_not_found", "scenario not found", nil)
			}
			return PlanDetail{}, err
		}
		alloc.AssetClassTargets = scn.Weights
	} else {
		for _, ac := range domain.AssetClasses {
			alloc.AssetClassTargets = append(alloc.AssetClassTargets, repository.AssetClassTarget{
				AssetClass: ac, Weight: 0,
			})
		}
	}
	alloc.RegionTargets = defaultRegionTargets()

	if err := s.plans.Create(ctx, plan); err != nil {
		return PlanDetail{}, err
	}
	params.PlanID = planID
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.params.Upsert(ctx, tx, params); err != nil {
			return err
		}
		return s.alloc.Replace(ctx, tx, planID, alloc)
	})
	if err != nil {
		return PlanDetail{}, err
	}
	return s.Get(ctx, planID)
}

func (s *PlanService) List(ctx context.Context) ([]PlanDetail, error) {
	plans, err := s.plans.List(ctx)
	if err != nil {
		return nil, err
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
		return PlanDetail{}, err
	}
	hash, err := s.hash.Compute(ctx, planID)
	if err != nil {
		return PlanDetail{}, err
	}
	return PlanDetail{Plan: plan, ConfigHash: hash}, nil
}

func (s *PlanService) Update(ctx context.Context, planID string, req UpdatePlanRequest) (PlanDetail, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return PlanDetail{}, newErr("plan_not_found", "plan not found", nil)
		}
		return PlanDetail{}, err
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
		return PlanDetail{}, err
	}
	return s.Get(ctx, planID)
}

func (s *PlanService) Delete(ctx context.Context, planID string) error {
	if err := s.plans.Delete(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return newErr("plan_not_found", "plan not found", nil)
		}
		return err
	}
	return nil
}

// GetParameters returns parameters and cash flows.
func (s *PlanService) GetParameters(ctx context.Context, planID string) (repository.PlanParameters, []repository.PlanCashFlow, error) {
	if _, err := s.plans.GetByID(ctx, planID); err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PlanParameters{}, nil, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PlanParameters{}, nil, err
	}
	params, err := s.params.Get(ctx, planID)
	if err != nil {
		return repository.PlanParameters{}, nil, err
	}
	flows, err := s.params.ListCashFlows(ctx, planID)
	if err != nil {
		return repository.PlanParameters{}, nil, err
	}
	return params, flows, nil
}

func (s *PlanService) UpdateParameters(ctx context.Context, planID string, req ParametersUpdateRequest) (repository.PlanParameters, []repository.PlanCashFlow, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PlanParameters{}, nil, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PlanParameters{}, nil, err
	}
	if req.ConfigVersion != plan.ConfigVersion {
		return repository.PlanParameters{}, nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
	}
	req.Parameters.PlanID = planID
	if err := validateParameters(req.Parameters); err != nil {
		return repository.PlanParameters{}, nil, newErr("parameters_invalid", err.Error(), nil)
	}

	holds, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return repository.PlanParameters{}, nil, err
	}
	enabledSum := int64(0)
	for _, h := range holds {
		if h.Enabled {
			enabledSum += h.CurrentAmountMinor
		}
	}
	gap := req.Parameters.TotalAssetsMinor - enabledSum
	if gap < -100 {
		return repository.PlanParameters{}, nil, newErr("holdings_exceed_total", "enabled holdings exceed total assets", map[string]any{
			"total_assets_minor": req.Parameters.TotalAssetsMinor, "holdings_sum_minor": enabledSum,
		})
	}
	if gap > 100 && !req.ApplyUnallocatedToCash {
		return repository.PlanParameters{}, nil, newErr("unallocated_gap_unresolved", "unallocated gap must be applied to cash or resolved via holdings", map[string]any{
			"gap_minor": gap,
		})
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if req.ApplyUnallocatedToCash && gap > 100 {
			if err := applyUnallocatedGapToCashTx(ctx, tx, s.holdings, planID, holds, gap); err != nil {
				return err
			}
		}
		if err := s.params.Upsert(ctx, tx, req.Parameters); err != nil {
			return err
		}
		if req.CashFlows != nil {
			if err := s.params.ReplaceCashFlows(ctx, tx, planID, req.CashFlows); err != nil {
				return err
			}
		}
		if _, err := s.plans.BumpVersionTx(ctx, tx, planID, req.ConfigVersion); err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return repository.PlanParameters{}, nil, newErr("plan_version_conflict", "plan configuration version mismatch", nil)
		}
		return repository.PlanParameters{}, nil, err
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

func (s *PlanService) CreatePortfolioSnapshot(ctx context.Context, planID string, req CreatePortfolioSnapshotRequest) (repository.PortfolioSnapshot, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		if errors.Is(err, repository.ErrPlanNotFound) {
			return repository.PortfolioSnapshot{}, newErr("plan_not_found", "plan not found", nil)
		}
		return repository.PortfolioSnapshot{}, err
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
				return err
			}
			if err := s.holdings.UpdateCurrentAmountsTx(ctx, tx, planID, req.Items); err != nil {
				return err
			}
			_, err := s.plans.BumpVersionTx(ctx, tx, planID, plan.ConfigVersion)
			return err
		})
	} else if err = snapRepo.Create(ctx, snap); err != nil {
		return repository.PortfolioSnapshot{}, fmt.Errorf("create snapshot: %w", err)
	}
	if err != nil {
		return repository.PortfolioSnapshot{}, fmt.Errorf("create snapshot: %w", err)
	}
	return snap, nil
}
