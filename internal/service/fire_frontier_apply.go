package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/frontier"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/simulation"
	"github.com/google/uuid"
)

type FrontierParameterValues struct {
	RetirementAge       int   `json:"retirement_age"`
	AnnualSavingsMinor  int64 `json:"annual_savings_minor"`
	AnnualSpendingMinor int64 `json:"annual_spending_minor"`
}

type FrontierPreview struct {
	RunID                     string                  `json:"run_id"`
	PointID                   string                  `json:"point_id"`
	ExpectedPlanConfigVersion int                     `json:"expected_plan_config_version"`
	Before                    FrontierParameterValues `json:"before"`
	After                     FrontierParameterValues `json:"after"`
	Unchanged                 []string                `json:"unchanged"`
	SourceRunID               string                  `json:"source_run_id"`
	AlgorithmVersion          string                  `json:"algorithm_version"`
	TargetProbability         float64                 `json:"target_probability"`
	Runs                      int                     `json:"runs"`
	SuccessProbability        float64                 `json:"success_probability"`
	SuccessWilsonLow          float64                 `json:"success_wilson_low"`
	SuccessWilsonHigh         float64                 `json:"success_wilson_high"`
	ImprovedPathCount         int                     `json:"improved_path_count"`
	RegressedPathCount        int                     `json:"regressed_path_count"`
	CurrentConfigHash         string                  `json:"current_config_hash"`
	CurrentMarketHash         string                  `json:"current_market_hash"`
	PreviewHash               string                  `json:"preview_hash"`
	PreviewExpiresAt          int64                   `json:"preview_expires_at"`
}

type PreviewFrontierRequest struct {
	ExpectedPlanConfigVersion int `json:"expected_plan_config_version"`
}

type ApplyFrontierRequest struct {
	ExpectedPlanConfigVersion int    `json:"expected_plan_config_version"`
	PreviewHash               string `json:"preview_hash"`
	PreviewExpiresAt          int64  `json:"preview_expires_at"`
}

type ApplyFrontierResponse struct {
	Application repository.FireFrontierApplication `json:"application"`
	Plan        repository.Plan                    `json:"plan"`
	Parameters  PlanParametersAPI                  `json:"parameters"`
}

type applicableFrontier struct {
	record repository.FireFrontierRun
	frozen frontier.FrozenInput
	result frontier.Result
	point  frontier.Point
}

func (s *FireFrontierService) Preview(ctx context.Context, runID, pointID string,
	req PreviewFrontierRequest,
) (FrontierPreview, error) {
	loaded, err := s.loadFrontierPoint(ctx, runID, pointID)
	if err != nil {
		return FrontierPreview{}, err
	}
	if _, err := s.runs.GetApplication(ctx, runID); err == nil {
		return FrontierPreview{}, newErr("frontier_run_already_applied", "frontier run has already been applied", nil)
	} else if !errors.Is(err, repository.ErrFireFrontierNotFound) {
		return FrontierPreview{}, wrapRepo("load frontier application", err)
	}
	plan, params, configHash, marketHash, err := s.currentFrontierState(ctx, loaded)
	if err != nil {
		return FrontierPreview{}, err
	}
	if req.ExpectedPlanConfigVersion != plan.ConfigVersion || configHash != loaded.record.SourceConfigHash ||
		marketHash != loaded.record.SourceMarketHash {
		return FrontierPreview{}, newErr("frontier_preview_stale", "plan or market inputs have changed", nil)
	}
	before := frontierParameterValues(params)
	after := frontierPointValues(params, loaded.record.FrontierType, loaded.point)
	if before == after {
		return FrontierPreview{}, newErr("frontier_point_no_change", "frontier point does not change the plan", nil)
	}
	expiresAt := s.now().Add(frontierPreviewTTL).UnixMilli()
	hash := frontierPreviewHash(loaded.record.ID, loaded.point.ID, plan.ConfigVersion,
		configHash, marketHash, before, after, expiresAt)
	evaluation := loaded.point.Evaluation
	return FrontierPreview{
		RunID: loaded.record.ID, PointID: loaded.point.ID,
		ExpectedPlanConfigVersion: plan.ConfigVersion, Before: before, After: after,
		Unchanged:   []string{"其余 FIRE 参数", "持仓与权重", "收益与风险假设", "通胀与提款策略", "模拟 seed"},
		SourceRunID: loaded.record.SourceSimulationRunID, AlgorithmVersion: loaded.record.AlgorithmVersion,
		TargetProbability: loaded.result.TargetProbability, Runs: evaluation.Runs,
		SuccessProbability: evaluation.SuccessProbability, SuccessWilsonLow: evaluation.SuccessWilsonLow,
		SuccessWilsonHigh: evaluation.SuccessWilsonHigh, ImprovedPathCount: evaluation.ImprovedPathCount,
		RegressedPathCount: evaluation.RegressedPathCount, CurrentConfigHash: configHash,
		CurrentMarketHash: marketHash, PreviewHash: hash, PreviewExpiresAt: expiresAt,
	}, nil
}

//nolint:funlen,gocognit,gocyclo,wrapcheck // Every preview identity and CAS invariant is checked in one write boundary.
func (s *FireFrontierService) Apply(ctx context.Context, runID, pointID string,
	req ApplyFrontierRequest,
) (ApplyFrontierResponse, error) {
	loaded, err := s.loadFrontierPoint(ctx, runID, pointID)
	if err != nil {
		return ApplyFrontierResponse{}, err
	}
	if existing, err := s.runs.GetApplication(ctx, runID); err == nil {
		if existing.PointID == pointID && existing.PreviewHash == req.PreviewHash {
			return s.appliedFrontierResponse(ctx, existing)
		}
		return ApplyFrontierResponse{}, newErr("frontier_run_already_applied", "frontier run has already been applied", nil)
	} else if !errors.Is(err, repository.ErrFireFrontierNotFound) {
		return ApplyFrontierResponse{}, wrapRepo("load frontier application", err)
	}
	plan, params, configHash, marketHash, err := s.currentFrontierState(ctx, loaded)
	if err != nil {
		return ApplyFrontierResponse{}, err
	}
	now := s.now()
	if req.ExpectedPlanConfigVersion != plan.ConfigVersion || configHash != loaded.record.SourceConfigHash ||
		marketHash != loaded.record.SourceMarketHash || now.UnixMilli() >= req.PreviewExpiresAt ||
		req.PreviewExpiresAt > now.Add(frontierPreviewTTL).UnixMilli() {
		return ApplyFrontierResponse{}, newErr("frontier_preview_stale", "preview expired or inputs changed", nil)
	}
	before := frontierParameterValues(params)
	after := frontierPointValues(params, loaded.record.FrontierType, loaded.point)
	if before == after {
		return ApplyFrontierResponse{}, newErr("frontier_point_no_change", "frontier point does not change the plan", nil)
	}
	wantPreviewHash := frontierPreviewHash(loaded.record.ID, loaded.point.ID, plan.ConfigVersion,
		configHash, marketHash, before, after, req.PreviewExpiresAt)
	if req.PreviewHash == "" || req.PreviewHash != wantPreviewHash {
		return ApplyFrontierResponse{}, newErr("frontier_preview_stale", "preview identity is invalid", nil)
	}
	configInput, err := s.hash.SnapshotReadOnly(ctx, loaded.record.PlanID)
	if err != nil {
		return ApplyFrontierResponse{}, wrapRepo("build frontier apply config", err)
	}
	candidateInput, err := frontier.ApplyConfigCandidate(configInput, loaded.record.FrontierType,
		frontier.Candidate{RetirementAge: loaded.point.RetirementAge, ValueMinor: loaded.point.ValueMinor}, nil)
	if err != nil {
		return ApplyFrontierResponse{}, newErr("frontier_result_inconsistent", err.Error(), nil)
	}
	candidateHash, err := domain.ComputeConfigHash(candidateInput)
	if err != nil || candidateHash != loaded.point.Evaluation.CandidateConfigHash {
		return ApplyFrontierResponse{}, newErr("frontier_result_inconsistent",
			"frontier candidate config hash cannot be reproduced", nil)
	}
	updatedParams := params
	updatedParams.RetirementAge = after.RetirementAge
	if loaded.record.FrontierType == frontier.TypeRetirementAgeMaxSpending {
		updatedParams.AnnualSpendingMinor = after.AnnualSpendingMinor
	} else {
		updatedParams.AnnualSavingsMinor = after.AnnualSavingsMinor
	}
	if err := validateParameters(updatedParams); err != nil {
		return ApplyFrontierResponse{}, newErr("frontier_result_inconsistent", err.Error(), nil)
	}
	beforeRaw, _ := json.Marshal(before)
	afterRaw, _ := json.Marshal(after)
	application := repository.FireFrontierApplication{
		ID: "ffa_" + uuid.NewString(), FrontierRunID: loaded.record.ID,
		PointID: loaded.point.ID, PlanID: loaded.record.PlanID,
		BeforeConfigVersion: plan.ConfigVersion, AfterConfigVersion: plan.ConfigVersion + 1,
		PreviewHash: req.PreviewHash, BeforeJSON: string(beforeRaw), AfterJSON: string(afterRaw),
		AppliedAt: s.now().UnixMilli(),
	}
	err = fdb.WithTx(ctx, s.db, func(tx *sql.Tx) error {
		currentPlan, txErr := s.plans.GetByIDTx(ctx, tx, loaded.record.PlanID)
		if txErr != nil {
			return txErr
		}
		if currentPlan.ConfigVersion != plan.ConfigVersion {
			return repository.ErrVersionConflict
		}
		currentParams, txErr := s.params.GetTx(ctx, tx, loaded.record.PlanID)
		if txErr != nil {
			return txErr
		}
		if frontierParameterValues(currentParams) != before {
			return repository.ErrVersionConflict
		}
		currentMarketHash, txErr := s.simulation.CurrentMarketSnapshotHashReadOnlyTx(
			ctx, tx, loaded.record.PlanID, loaded.frozen.SourceSnapshot)
		if txErr != nil || currentMarketHash != loaded.record.SourceMarketHash {
			return repository.ErrVersionConflict
		}
		if txErr := s.params.Upsert(ctx, tx, updatedParams); txErr != nil {
			return txErr
		}
		version, txErr := s.plans.BumpVersionTx(ctx, tx, loaded.record.PlanID, plan.ConfigVersion)
		if txErr != nil {
			return txErr
		}
		application.AfterConfigVersion = version
		return s.runs.CreateApplicationTx(ctx, tx, application)
	})
	if err != nil {
		if errors.Is(err, repository.ErrVersionConflict) {
			return ApplyFrontierResponse{}, newErr("frontier_preview_stale", "plan changed while applying", nil)
		}
		if isUniqueConstraintErr(err) {
			if existing, getErr := s.runs.GetApplication(ctx, runID); getErr == nil &&
				existing.PointID == pointID && existing.PreviewHash == req.PreviewHash {
				return s.appliedFrontierResponse(ctx, existing)
			}
			return ApplyFrontierResponse{}, newErr("frontier_run_already_applied",
				"frontier run has already been applied", nil)
		}
		return ApplyFrontierResponse{}, wrapRepo("apply frontier transaction", err)
	}
	plan.ConfigVersion = application.AfterConfigVersion
	return ApplyFrontierResponse{Application: application, Plan: plan,
		Parameters: ParametersToAPI(updatedParams)}, nil
}

func (s *FireFrontierService) loadFrontierPoint(ctx context.Context, runID, pointID string) (
	applicableFrontier, error,
) {
	record, err := s.runs.GetByID(ctx, runID)
	if err != nil {
		return applicableFrontier{}, newErr("frontier_run_not_found", "frontier run not found", nil)
	}
	if record.TaskStatus != repository.WorkerTaskStatusComplete {
		return applicableFrontier{}, newErr("frontier_point_not_applicable", "frontier run is not complete", nil)
	}
	var frozen frontier.FrozenInput
	var result frontier.Result
	if json.Unmarshal([]byte(record.InputSnapshotJSON), &frozen) != nil ||
		json.Unmarshal(record.ResultJSON, &result) != nil || result.AlgorithmVersion != record.AlgorithmVersion ||
		result.FrontierType != record.FrontierType {
		return applicableFrontier{}, newErr("frontier_result_inconsistent", "frontier result cannot be decoded", nil)
	}
	var found *frontier.Point
	for i := range result.Points {
		if result.Points[i].ID == pointID {
			found = &result.Points[i]
			break
		}
	}
	if found == nil {
		return applicableFrontier{}, newErr("frontier_point_not_found", "frontier point not found", nil)
	}
	if !found.Applicable || (record.FrontierType != frontier.TypeRetirementAgeMaxSpending &&
		record.FrontierType != frontier.TypeRetirementAgeMinSavings) ||
		(found.Status != frontier.StatusBoundaryFound && found.Status != frontier.StatusEntireDomainFeasible) ||
		!found.Evaluation.MeetsTarget || found.Evaluation.SuccessWilsonLow < result.TargetProbability {
		return applicableFrontier{}, newErr("frontier_point_not_applicable", "frontier point cannot be applied", nil)
	}
	baseConfig, err := frontier.DecodeConfigHashInput(frozen.ConfigHashInputJSON)
	if err != nil {
		return applicableFrontier{}, newErr("frontier_result_inconsistent", err.Error(), nil)
	}
	candidate := frontier.Candidate{RetirementAge: found.RetirementAge, ValueMinor: found.ValueMinor}
	snapshot, err := frontier.BuildCandidate(frozen.SourceSnapshot, record.FrontierType, candidate)
	if err != nil {
		return applicableFrontier{}, newErr("frontier_result_inconsistent", err.Error(), nil)
	}
	candidateConfig, err := frontier.ApplyConfigCandidate(baseConfig, record.FrontierType, candidate, nil)
	if err != nil {
		return applicableFrontier{}, newErr("frontier_result_inconsistent", err.Error(), nil)
	}
	candidateHash, err := domain.ComputeConfigHash(candidateConfig)
	if err != nil || candidateHash != found.Evaluation.CandidateConfigHash {
		return applicableFrontier{}, newErr("frontier_result_inconsistent", "candidate config hash differs", nil)
	}
	snapshot.ConfigHash = candidateHash
	snapshotHash, err := simulation.HashInput(&snapshot)
	if err != nil || snapshotHash != found.Evaluation.SnapshotHash {
		return applicableFrontier{}, newErr("frontier_result_inconsistent", "candidate snapshot hash differs", nil)
	}
	return applicableFrontier{record: record, frozen: frozen, result: result, point: *found}, nil
}

func (s *FireFrontierService) currentFrontierState(ctx context.Context, loaded applicableFrontier) (
	repository.Plan, repository.PlanParameters, string, string, error,
) {
	plan, err := s.plans.GetByID(ctx, loaded.record.PlanID)
	if err != nil {
		return repository.Plan{}, repository.PlanParameters{}, "", "", wrapRepo("load frontier plan", err)
	}
	params, err := s.params.Get(ctx, loaded.record.PlanID)
	if err != nil {
		return plan, repository.PlanParameters{}, "", "", wrapRepo("load frontier parameters", err)
	}
	input, err := s.hash.SnapshotReadOnly(ctx, loaded.record.PlanID)
	if err != nil {
		return plan, params, "", "", wrapRepo("build frontier config hash", err)
	}
	configHash, err := domain.ComputeConfigHash(input)
	if err != nil {
		return plan, params, "", "", err
	}
	marketHash, err := s.simulation.CurrentMarketSnapshotHashReadOnly(ctx,
		loaded.record.PlanID, loaded.frozen.SourceSnapshot)
	if err != nil {
		return plan, params, configHash, "", wrapRepo("build frontier market hash", err)
	}
	return plan, params, configHash, marketHash, nil
}

func (s *FireFrontierService) appliedFrontierResponse(ctx context.Context,
	application repository.FireFrontierApplication,
) (ApplyFrontierResponse, error) {
	plan, err := s.plans.GetByID(ctx, application.PlanID)
	if err != nil {
		return ApplyFrontierResponse{}, wrapRepo("load applied frontier plan", err)
	}
	params, err := s.params.Get(ctx, application.PlanID)
	if err != nil {
		return ApplyFrontierResponse{}, wrapRepo("load applied frontier parameters", err)
	}
	return ApplyFrontierResponse{Application: application, Plan: plan,
		Parameters: ParametersToAPI(params)}, nil
}

func frontierParameterValues(params repository.PlanParameters) FrontierParameterValues {
	return FrontierParameterValues{RetirementAge: params.RetirementAge,
		AnnualSavingsMinor: params.AnnualSavingsMinor, AnnualSpendingMinor: params.AnnualSpendingMinor}
}

func frontierPointValues(params repository.PlanParameters, frontierType string,
	point frontier.Point,
) FrontierParameterValues {
	out := frontierParameterValues(params)
	out.RetirementAge = point.RetirementAge
	if frontierType == frontier.TypeRetirementAgeMaxSpending {
		out.AnnualSpendingMinor = point.ValueMinor
	} else {
		out.AnnualSavingsMinor = point.ValueMinor
	}
	return out
}
