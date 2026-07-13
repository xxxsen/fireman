package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// research_optimization_service.go implements the service layer for
// portfolio auto-optimization: creation, readiness, querying
// and the task executor entry point.

// --- optimization readiness ---

// OptimizationReadiness is the response for optimization readiness checks.
type OptimizationReadiness struct {
	Ready           bool                       `json:"ready"`
	CandidateCount  int                        `json:"candidate_count"`
	EnabledCount    int                        `json:"enabled_count"`
	LockedCount     int                        `json:"locked_count"`
	TunableCount    int                        `json:"tunable_count"`
	LockedWeightSum float64                    `json:"locked_weight_sum"`
	BlockingReasons []ResearchReadinessIssue   `json:"blocking_reasons"`
	Warnings        []ResearchReadinessIssue   `json:"warnings"`
	TailRisk        *ResearchTailRiskReadiness `json:"tail_risk,omitempty"`
}

type OptimizationReadinessRequest struct {
	WeightStep  float64
	Confidence  *float64
	HorizonDays *int
}

// GetOptimizationReadiness checks whether a collection is ready for
// auto-optimization readiness.
func (s *ResearchService) GetOptimizationReadiness(
	ctx context.Context, collectionID string, req OptimizationReadinessRequest,
) (OptimizationReadiness, error) {
	var zero OptimizationReadiness
	collection, err := s.research.GetCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found",
				"research collection not found", nil)
		}
		return zero, wrapRepo("load research collection", err)
	}
	if collection.Status != repository.ResearchCollectionStatusActive {
		return zero, newErr("research_collection_archived",
			"归档的集合不能运行调优", nil)
	}

	ds, err := s.loadResearchDataset(ctx, collection)
	if err != nil {
		return zero, err
	}

	spec, err := resolveRequestedTailRisk(collection, req.Confidence, req.HorizonDays)
	if err != nil {
		return zero, tailRiskAppError(err)
	}
	config := OptimizationConfig{WeightStep: req.WeightStep, TailRisk: spec}
	config.NormalizeDefaults()
	if err := config.Validate(); err != nil {
		return zero, newErr("invalid_request", err.Error(), nil)
	}
	out := evaluateOptimizationReadiness(ds, config)

	// Merge data-dependency blocking reasons from standard readiness
	// (excluding weight_sum_invalid) so the readiness endpoint is
	// consistent with the creation endpoint.
	originalConfidence, originalHorizon := ds.Collection.TailRiskConfidence, ds.Collection.TailRiskHorizonDays
	ds.Collection.TailRiskConfidence, ds.Collection.TailRiskHorizonDays = spec.Confidence, spec.HorizonDays
	stdReadiness := evaluateResearchReadiness(ds, s.now())
	ds.Collection.TailRiskConfidence, ds.Collection.TailRiskHorizonDays = originalConfidence, originalHorizon
	out.TailRisk = stdReadiness.TailRisk
	for _, b := range stdReadiness.BlockingReasons {
		if b.Reason == ResearchReasonWeightSumInvalid || b.Reason == ResearchReasonCVARSample {
			continue
		}
		alreadyPresent := false
		for _, existing := range out.BlockingReasons {
			if existing.Reason == b.Reason && existing.AssetKey == b.AssetKey && existing.Pair == b.Pair {
				alreadyPresent = true
				break
			}
		}
		if !alreadyPresent {
			out.BlockingReasons = append(out.BlockingReasons, b)
		}
	}
	appendOptimizationTailRiskIssues(ds, stdReadiness, spec, &out)
	out.Ready = len(out.BlockingReasons) == 0

	return out, nil
}

// appendOptimizationTailRiskIssues checks every asset that can receive a
// positive candidate weight. This is deliberately more conservative than
// checking the current portfolio's union of observations: every generated
// candidate is guaranteed to satisfy the configured CVaR sample gate.
func appendOptimizationTailRiskIssues(
	ds *researchDataset,
	readiness ResearchReadiness,
	spec TailRiskSpec,
	out *OptimizationReadiness,
) {
	lo, loErr := parseResearchDate(readiness.WindowStart)
	hi, hiErr := parseResearchDate(readiness.WindowEnd)
	if loErr != nil || hiErr != nil {
		return
	}
	minimum := MinimumTailRiskScenarios(spec.Confidence)
	minimumEffective, minimumScenarios := -1, -1
	for i := range ds.Enabled {
		asset := ds.Enabled[i]
		if asset.Item.WeightLocked && asset.Item.Weight <= ResearchWeightTolerance {
			continue
		}
		single := *ds
		single.Enabled = append([]researchAssetData(nil), ds.Enabled...)
		for j := range single.Enabled {
			single.Enabled[j].Item.Weight = 0
		}
		single.Enabled[i].Item.Weight = 1
		effectiveCount := len(relevantEffectiveObservationDays(&single, lo, hi))
		scenarioCount := TailRiskScenarioCount(effectiveCount, spec.HorizonDays)
		if minimumEffective < 0 || effectiveCount < minimumEffective {
			minimumEffective = effectiveCount
		}
		if minimumScenarios < 0 || scenarioCount < minimumScenarios {
			minimumScenarios = scenarioCount
		}
		if scenarioCount < minimum {
			out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey,
				Reason:   ResearchReasonCVARSample,
				Message: fmt.Sprintf(
					"资产 %s 的 CVaR 场景数 %d 少于最低要求 %d（%.0f%% / %d 日）",
					asset.Item.AssetKey, scenarioCount, minimum, spec.Confidence*100, spec.HorizonDays,
				),
			})
		}
	}
	if minimumEffective >= 0 {
		out.TailRisk = &ResearchTailRiskReadiness{
			Confidence: spec.Confidence, HorizonDays: spec.HorizonDays,
			EffectiveReturnCount: minimumEffective, ScenarioCount: minimumScenarios,
			MinimumScenarioCount: minimum,
		}
	}
}

func evaluateOptimizationReadiness(
	ds *researchDataset, config OptimizationConfig,
) OptimizationReadiness {
	config.NormalizeDefaults()

	out := OptimizationReadiness{
		BlockingReasons: []ResearchReadinessIssue{},
		Warnings:        []ResearchReadinessIssue{},
	}
	out.EnabledCount = len(ds.Enabled)
	if out.EnabledCount == 0 {
		out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
			Reason: ResearchReasonNoEnabledAssets, Message: "集合没有启用的资产",
		})
	}
	if out.EnabledCount > OptimizationMaxEnabledAssets {
		out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
			Reason: "too_many_enabled_assets",
			Message: fmt.Sprintf("启用资产数量 %d 超过上限 %d",
				out.EnabledCount, OptimizationMaxEnabledAssets),
		})
	}

	assets := summarizeOptimizationAssets(ds, &out)
	if out.LockedWeightSum > 1+1e-12 {
		out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
			Reason:  "locked_weight_exceeds_100",
			Message: fmt.Sprintf("锁定权重合计 %.2f%% 超过 100%%", out.LockedWeightSum*100),
		})
	}

	appendOptimizationDataIssues(ds, &out)
	if out.EnabledCount > 0 && out.EnabledCount <= OptimizationMaxEnabledAssets {
		appendOptimizationCandidateIssues(assets, config, &out)
	}

	out.Ready = len(out.BlockingReasons) == 0
	return out
}

func summarizeOptimizationAssets(
	ds *researchDataset, out *OptimizationReadiness,
) []OptimizationAsset {
	assets := make([]OptimizationAsset, 0, len(ds.Enabled))
	for _, asset := range ds.Enabled {
		assets = append(assets, OptimizationAsset{
			ItemID: asset.Item.ID, AssetKey: asset.Item.AssetKey, Name: asset.Asset.Name,
			Weight: asset.Item.Weight, Locked: asset.Item.WeightLocked,
		})
		if asset.Item.WeightLocked {
			out.LockedCount++
			out.LockedWeightSum += asset.Item.Weight
		} else {
			out.TunableCount++
		}
	}
	return assets
}

func appendOptimizationDataIssues(ds *researchDataset, out *OptimizationReadiness) {
	for _, asset := range ds.Enabled {
		if asset.IsCash {
			continue
		}
		if len(asset.Points) == 0 {
			out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchReasonHistoryMissing, Message: "缺少历史点位",
			})
		}
		if asset.Task != nil && repository.IsActiveWorkerTaskStatus(asset.Task.Status) {
			out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
				AssetKey: asset.Item.AssetKey, Reason: ResearchReasonHistorySyncing, Message: "历史同步正在运行",
			})
		}
		for _, pair := range asset.FXPairs {
			fx, ok := ds.FX[pair]
			if !ok || !fx.Found {
				out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
					AssetKey: asset.Item.AssetKey, Pair: pair, Reason: ResearchReasonFXMissing,
					Message: fmt.Sprintf("%s 资产需要 %s 历史汇率", asset.Asset.Currency, pair),
				})
			}
		}
	}
}

func appendOptimizationCandidateIssues(
	assets []OptimizationAsset, config OptimizationConfig, out *OptimizationReadiness,
) {
	out.CandidateCount = CountCandidates(assets, config.WeightStep)
	if out.CandidateCount > config.MaxCandidateCount {
		out.Warnings = append(out.Warnings, ResearchReadinessIssue{
			Reason: "candidate_count_exceeds_recommendation",
			Message: fmt.Sprintf("当前候选数量 %d，推荐控制在 %d 以内；超过推荐数量后，模拟耗时和内存占用会急剧增加",
				out.CandidateCount, config.MaxCandidateCount),
		})
	}
	if out.CandidateCount == 0 {
		out.BlockingReasons = append(out.BlockingReasons, ResearchReadinessIssue{
			Reason: "candidate_count_zero", Message: "当前锁定权重与步长无法生成有效候选组合",
		})
	}
	if out.TunableCount == 0 && math.Abs(out.LockedWeightSum-1) <= 1e-12 {
		out.Warnings = append(out.Warnings, ResearchReadinessIssue{
			Reason: "all_locked", Message: "所有资产权重已锁定，自动调优将退化为固定组合回测，建议使用普通回测",
		})
	}
}

// --- create optimization ---

// ResearchOptimizationRequest is the POST /collections/{id}/optimizations body.
type ResearchOptimizationRequest struct {
	WeightStep        float64       `json:"weight_step"`
	MaxCandidateCount int           `json:"max_candidate_count"`
	TopK              int           `json:"top_k"`
	TailRisk          *TailRiskSpec `json:"tail_risk,omitempty"`
	MinimumCAGR       *float64      `json:"minimum_cagr,omitempty"`
}

func resolveRequestedTailRisk(
	collection repository.ResearchCollection, confidence *float64, horizon *int,
) (TailRiskSpec, error) {
	value := collection.TailRiskConfidence
	if confidence != nil {
		value = *confidence
	}
	days := collection.TailRiskHorizonDays
	if horizon != nil {
		days = *horizon
	}
	return CanonicalTailRiskSpec(TailRiskSpec{Confidence: value, HorizonDays: days})
}

func optimizationConfigFromRequest(
	collection repository.ResearchCollection, req ResearchOptimizationRequest,
) (OptimizationConfig, error) {
	var confidence *float64
	var horizon *int
	if req.TailRisk != nil {
		confidence, horizon = &req.TailRisk.Confidence, &req.TailRisk.HorizonDays
	}
	spec, err := resolveRequestedTailRisk(collection, confidence, horizon)
	if err != nil {
		return OptimizationConfig{}, err
	}
	return OptimizationConfig{
		WeightStep: req.WeightStep, MaxCandidateCount: req.MaxCandidateCount,
		TopK: req.TopK, TailRisk: spec, MinimumCAGR: req.MinimumCAGR,
	}, nil
}

func optionalFloatHash(value *float64) string {
	if value == nil {
		return "null"
	}
	return strconv.FormatFloat(*value, 'g', 17, 64)
}

// ResearchOptimizationCreateResult is the creation response.
type ResearchOptimizationCreateResult struct {
	Optimization ResearchOptimizationView `json:"optimization"`
	Reused       bool                     `json:"reused"`
}

// ResearchOptimizationView is the API shape of one optimization run.
type ResearchOptimizationView struct {
	ID              string            `json:"id"`
	CollectionID    string            `json:"collection_id"`
	TaskID          string            `json:"task_id"`
	Status          string            `json:"status"`
	Config          json.RawMessage   `json:"config"`
	CandidateCount  int               `json:"candidate_count"`
	EvaluatedCount  int               `json:"evaluated_count"`
	Result          json.RawMessage   `json:"result,omitempty"`
	ErrorCode       string            `json:"error_code,omitempty"`
	ErrorMessage    string            `json:"error_message,omitempty"`
	BaseCurrency    string            `json:"base_currency"`
	RebalancePolicy string            `json:"rebalance_policy"`
	WindowStart     string            `json:"window_start"`
	WindowEnd       string            `json:"window_end"`
	EngineVersion   string            `json:"engine_version"`
	CreatedAt       int64             `json:"created_at"`
	CompletedAt     *int64            `json:"completed_at,omitempty"`
	Task            *ResearchTaskView `json:"task,omitempty"`
}

func buildOptimizationView(run repository.ResearchOptimizationRun) ResearchOptimizationView {
	view := ResearchOptimizationView{
		ID:              run.ID,
		CollectionID:    run.CollectionID,
		TaskID:          run.TaskID,
		Status:          run.Status,
		CandidateCount:  run.CandidateCount,
		EvaluatedCount:  run.EvaluatedCount,
		ErrorCode:       run.ErrorCode,
		ErrorMessage:    run.ErrorMessage,
		BaseCurrency:    run.BaseCurrency,
		RebalancePolicy: run.RebalancePolicy,
		WindowStart:     run.WindowStart,
		WindowEnd:       run.WindowEnd,
		EngineVersion:   run.EngineVersion,
		CreatedAt:       run.CreatedAt,
		CompletedAt:     run.CompletedAt,
	}
	if run.ConfigJSON != "" && run.ConfigJSON != "{}" {
		view.Config = json.RawMessage(run.ConfigJSON)
	}
	if run.ResultJSON != "" && run.ResultJSON != "{}" &&
		run.Status == repository.ResearchRunStatusSucceeded {
		view.Result = json.RawMessage(run.ResultJSON)
	}
	return view
}

// optimizationInputSnapshot freezes the optimization input.
type optimizationInputSnapshot struct {
	EngineVersion  string                   `json:"engine_version"`
	SourceHash     string                   `json:"source_hash"`
	CommonStart    string                   `json:"common_start"`
	CommonEnd      string                   `json:"common_end"`
	WindowStart    string                   `json:"window_start"`
	WindowEnd      string                   `json:"window_end"`
	Collection     researchSnapshotParams   `json:"collection"`
	Assets         []researchSnapshotAsset  `json:"assets"`
	FX             []researchSnapshotSeries `json:"fx"`
	Benchmark      *researchSnapshotAsset   `json:"benchmark,omitempty"`
	LockedWeights  map[string]float64       `json:"locked_weights"`
	TunableItemIDs []string                 `json:"tunable_item_ids"`
	Config         OptimizationConfig       `json:"config"`
}

type optimizationCreationContext struct {
	collection repository.ResearchCollection
	dataset    *researchDataset
	config     OptimizationConfig
	readiness  ResearchReadiness
}

func (s *ResearchService) prepareOptimizationCreation(
	ctx context.Context, collectionID string, req ResearchOptimizationRequest,
) (optimizationCreationContext, error) {
	var zero optimizationCreationContext
	collection, err := s.research.GetCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return zero, wrapRepo("load research collection", err)
	}
	if collection.Status != repository.ResearchCollectionStatusActive {
		return zero, newErr("research_collection_archived", "归档的集合不能运行调优", nil)
	}
	dataset, err := s.loadResearchDataset(ctx, collection)
	if err != nil {
		return zero, err
	}
	config, err := optimizationConfigFromRequest(collection, req)
	if err != nil {
		return zero, tailRiskAppError(err)
	}
	config.NormalizeDefaults()
	if err := config.Validate(); err != nil {
		return zero, newErr("invalid_request", err.Error(), nil)
	}
	readiness, err := validateOptimizationCreationReadiness(dataset, config, s.now())
	if err != nil {
		return zero, err
	}
	return optimizationCreationContext{
		collection: collection, dataset: dataset, config: config, readiness: readiness,
	}, nil
}

// CreateOptimization creates an optimization run.
func (s *ResearchService) CreateOptimization(
	ctx context.Context, collectionID string, req ResearchOptimizationRequest,
) (ResearchOptimizationCreateResult, error) {
	var zero ResearchOptimizationCreateResult
	prepared, err := s.prepareOptimizationCreation(ctx, collectionID, req)
	if err != nil {
		return zero, err
	}
	collection := prepared.collection
	ds := prepared.dataset
	config := prepared.config
	stdReadiness := prepared.readiness

	snapshot := buildOptimizationSnapshot(ds, stdReadiness, config)
	inputHash := computeOptimizationInputHash(snapshot)
	snapshotJSON, err := json.Marshal(snapshot)
	if err != nil {
		return zero, fmt.Errorf("marshal optimization snapshot: %w", err)
	}
	configJSON, err := json.Marshal(config)
	if err != nil {
		return zero, fmt.Errorf("marshal optimization config: %w", err)
	}

	if reused, found, err := s.findReusableOptimization(ctx, collectionID, inputHash); err != nil {
		return zero, err
	} else if found {
		return reused, nil
	}

	now := s.now().UnixMilli()
	runID := "ror_" + uuid.New().String()
	taskID := "task_" + uuid.New().String()
	payloadJSON, err := json.Marshal(map[string]string{
		"optimization_run_id": runID, "collection_id": collectionID,
	})
	if err != nil {
		return zero, fmt.Errorf("marshal task payload: %w", err)
	}

	candidateCount := CountCandidates(buildOptimizationAssets(ds), config.WeightStep)

	run := repository.ResearchOptimizationRun{
		ID: runID, CollectionID: collectionID, TaskID: taskID,
		Status: repository.WorkerTaskStatusPending, InputHash: inputHash,
		SourceHash: snapshot.SourceHash, EngineVersion: OptimizationEngineVersion,
		BaseCurrency: collection.BaseCurrency, RebalancePolicy: collection.RebalancePolicy,
		WindowStart: stdReadiness.WindowStart, WindowEnd: stdReadiness.WindowEnd,
		ConfigJSON: string(configJSON), InputSnapshotJSON: string(snapshotJSON),
		CandidateCount: candidateCount, EvaluatedCount: 0, ResultJSON: "{}",
		CreatedAt: now,
	}

	bound, racedReuse, err := s.persistQueuedOptimization(ctx, run, inputHash, string(payloadJSON), now)
	if err != nil {
		return zero, err
	}
	if racedReuse {
		existing, getErr := s.research.GetOptimizationRunByTaskID(ctx, bound.ID)
		if getErr != nil {
			return zero, wrapRepo("load active optimization", getErr)
		}
		view := buildOptimizationView(existing)
		applyOptimizationTaskState(&view, bound)
		return ResearchOptimizationCreateResult{Optimization: view, Reused: true}, nil
	}

	view := buildOptimizationView(run)
	return ResearchOptimizationCreateResult{Optimization: view}, nil
}

func validateOptimizationCreationReadiness(
	ds *researchDataset, config OptimizationConfig, now time.Time,
) (ResearchReadiness, error) {
	optimizationReadiness := evaluateOptimizationReadiness(ds, config)
	if !optimizationReadiness.Ready {
		return ResearchReadiness{}, newErr(
			"research_optimization_not_ready", "集合未通过调优准入检查",
			map[string]any{"readiness": optimizationReadiness},
		)
	}
	originalConfidence, originalHorizon := ds.Collection.TailRiskConfidence, ds.Collection.TailRiskHorizonDays
	ds.Collection.TailRiskConfidence = config.TailRisk.Confidence
	ds.Collection.TailRiskHorizonDays = config.TailRisk.HorizonDays
	readiness := evaluateResearchReadiness(ds, now)
	ds.Collection.TailRiskConfidence, ds.Collection.TailRiskHorizonDays = originalConfidence, originalHorizon
	blocking := make([]ResearchReadinessIssue, 0, len(readiness.BlockingReasons))
	for _, issue := range readiness.BlockingReasons {
		if issue.Reason != ResearchReasonWeightSumInvalid && issue.Reason != ResearchReasonCVARSample {
			blocking = append(blocking, issue)
		}
	}
	appendOptimizationTailRiskIssues(ds, readiness, config.TailRisk, &optimizationReadiness)
	if len(optimizationReadiness.BlockingReasons) > 0 {
		return ResearchReadiness{}, newErr(
			"research_optimization_not_ready", "集合未通过调优准入检查",
			map[string]any{"readiness": optimizationReadiness},
		)
	}
	if len(blocking) > 0 {
		return ResearchReadiness{}, newErr(
			"research_optimization_not_ready", "集合未通过数据准入检查",
			map[string]any{"blocking_reasons": blocking},
		)
	}
	return readiness, nil
}

func (s *ResearchService) findReusableOptimization(
	ctx context.Context, collectionID, inputHash string,
) (ResearchOptimizationCreateResult, bool, error) {
	run, err := s.research.FindSucceededOptimizationByInputHash(ctx, collectionID, inputHash)
	if err == nil {
		return ResearchOptimizationCreateResult{
			Optimization: buildOptimizationView(run), Reused: true,
		}, true, nil
	}
	if !errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
		return ResearchOptimizationCreateResult{}, false, wrapRepo("find succeeded optimization", err)
	}
	run, err = s.research.FindActiveOptimizationByInputHash(ctx, collectionID, inputHash)
	if errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
		return ResearchOptimizationCreateResult{}, false, nil
	}
	if err != nil {
		return ResearchOptimizationCreateResult{}, false, wrapRepo("find active optimization", err)
	}
	view := buildOptimizationView(run)
	if task, taskErr := s.tasks.GetByID(ctx, run.TaskID); taskErr == nil {
		applyOptimizationTaskState(&view, task)
	}
	return ResearchOptimizationCreateResult{Optimization: view, Reused: true}, true, nil
}

func (s *ResearchService) persistQueuedOptimization(
	ctx context.Context,
	run repository.ResearchOptimizationRun,
	inputHash, payloadJSON string,
	now int64,
) (repository.WorkerTask, bool, error) {
	task := repository.WorkerTask{
		ID: run.TaskID, WorkerType: repository.WorkerTypeGo,
		Type:      repository.WorkerTaskTypeResearchOptimization,
		Status:    repository.WorkerTaskStatusPending,
		ScopeType: "research_collection", ScopeID: run.CollectionID,
		DedupeKey: repository.WorkerTaskTypeResearchOptimization +
			"|collection:" + run.CollectionID,
		InputHash: inputHash, PayloadJSON: payloadJSON, ProgressTotal: run.CandidateCount,
		CreatedAt: now,
	}
	return s.persistQueuedResearchTask(ctx, task, "create optimization run", func(tx *sql.Tx) error {
		if err := s.research.CreateOptimizationRunTx(ctx, tx, run); err != nil {
			return fmt.Errorf("create optimization run: %w", err)
		}
		return nil
	})
}

func buildOptimizationAssets(ds *researchDataset) []OptimizationAsset {
	out := make([]OptimizationAsset, 0, len(ds.Enabled))
	for _, a := range ds.Enabled {
		out = append(out, OptimizationAsset{
			ItemID: a.Item.ID, AssetKey: a.Item.AssetKey,
			Name: a.Asset.Name, Weight: a.Item.Weight, Locked: a.Item.WeightLocked,
		})
	}
	return out
}

func buildOptimizationSnapshot(
	ds *researchDataset, readiness ResearchReadiness, config OptimizationConfig,
) optimizationInputSnapshot {
	baseSnapshot := buildResearchSnapshot(ds, readiness)
	snapshot := optimizationInputSnapshot{
		EngineVersion: OptimizationEngineVersion, SourceHash: baseSnapshot.SourceHash,
		CommonStart: readiness.CommonStart, CommonEnd: readiness.CommonEnd,
		WindowStart: readiness.WindowStart, WindowEnd: readiness.WindowEnd,
		Collection: baseSnapshot.Collection, Assets: baseSnapshot.Assets,
		FX: baseSnapshot.FX, Benchmark: baseSnapshot.Benchmark,
		LockedWeights: map[string]float64{}, Config: config,
	}
	for _, a := range ds.Enabled {
		if a.Item.WeightLocked {
			snapshot.LockedWeights[a.Item.ID] = a.Item.Weight
		} else {
			snapshot.TunableItemIDs = append(snapshot.TunableItemIDs, a.Item.ID)
		}
	}
	return snapshot
}

func computeOptimizationInputHash(snapshot optimizationInputSnapshot) string {
	h := sha256.New()
	fmt.Fprintf(h, "optimizer_source:%s\n", snapshot.SourceHash)
	fmt.Fprintf(h, "optimizer_engine:%s\n", snapshot.EngineVersion)
	c := snapshot.Collection
	fmt.Fprintf(h, "params:%s|%d|%s|%s|%s|%s|%s|%s\n",
		c.BaseCurrency, c.InitialAmountMinor, c.RebalancePolicy,
		strconv.FormatFloat(c.RebalanceThreshold, 'g', 17, 64), c.StartPolicy,
		strconv.FormatFloat(c.RiskFreeRate, 'g', 17, 64),
		strconv.FormatFloat(c.TransactionCostRate, 'g', 17, 64), c.BenchmarkAssetKey)
	fmt.Fprintf(h, "window:%s..%s\n", snapshot.WindowStart, snapshot.WindowEnd)
	for _, a := range snapshot.Assets {
		fmt.Fprintf(h, "asset:%s|%s|%s\n", a.AssetKey, a.AdjustPolicy, a.PointType)
	}
	lockedIDs := make([]string, 0, len(snapshot.LockedWeights))
	for id := range snapshot.LockedWeights {
		lockedIDs = append(lockedIDs, id)
	}
	sort.Strings(lockedIDs)
	for _, id := range lockedIDs {
		fmt.Fprintf(h, "locked:%s|%s\n", id, strconv.FormatFloat(snapshot.LockedWeights[id], 'g', 17, 64))
	}
	sortedTunable := make([]string, len(snapshot.TunableItemIDs))
	copy(sortedTunable, snapshot.TunableItemIDs)
	sort.Strings(sortedTunable)
	for _, id := range sortedTunable {
		fmt.Fprintf(h, "tunable:%s\n", id)
	}
	fmt.Fprintf(h, "config:%s|%d|%d|%s|%d|%s|%s\n",
		strconv.FormatFloat(snapshot.Config.WeightStep, 'g', 17, 64),
		snapshot.Config.MaxCandidateCount, snapshot.Config.TopK,
		strconv.FormatFloat(snapshot.Config.TailRisk.Confidence, 'g', 17, 64),
		snapshot.Config.TailRisk.HorizonDays, optionalFloatHash(snapshot.Config.MinimumCAGR),
		TailRiskAlgorithmVersion)
	return hex.EncodeToString(h.Sum(nil))
}

// --- get optimization ---

func (s *ResearchService) GetOptimization(
	ctx context.Context, optimizationID string,
) (ResearchOptimizationView, error) {
	run, err := s.research.GetOptimizationRun(ctx, optimizationID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
			return ResearchOptimizationView{}, newErr("research_optimization_not_found",
				"optimization run not found", nil)
		}
		return ResearchOptimizationView{}, wrapRepo("load optimization run", err)
	}
	view := buildOptimizationView(run)
	if task, err := s.tasks.GetByID(ctx, run.TaskID); err == nil {
		applyOptimizationTaskState(&view, task)
	}
	return view, nil
}

// GetLatestOptimization returns the most recent optimization run for a
// collection (any status), used for the collection detail page entry point.
func (s *ResearchService) GetLatestOptimization(
	ctx context.Context, collectionID string,
) (ResearchOptimizationView, bool, error) {
	run, err := s.research.LatestOptimizationByCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
			return ResearchOptimizationView{}, false, nil
		}
		return ResearchOptimizationView{}, false, wrapRepo("load latest optimization", err)
	}
	view := buildOptimizationView(run)
	if task, err := s.tasks.GetByID(ctx, run.TaskID); err == nil {
		applyOptimizationTaskState(&view, task)
	}
	return view, true, nil
}

func applyOptimizationTaskState(view *ResearchOptimizationView, task repository.WorkerTask) {
	view.Task = buildResearchTaskView(task)
	view.Status = task.Status
	if task.ErrorCode != "" {
		view.ErrorCode = task.ErrorCode
	}
	if task.ErrorMessage != "" {
		view.ErrorMessage = task.ErrorMessage
	}
	if task.FinishedAt != nil {
		view.CompletedAt = task.FinishedAt
	}
}

// ResearchOptimizationApplyRequest selects one immutable ranked result and
// guards the write with the collection version shown in the preview.
type ResearchOptimizationApplyRequest struct {
	Objective                   OptimizationObjective `json:"objective"`
	Rank                        int                   `json:"rank"`
	ExpectedCollectionUpdatedAt int64                 `json:"expected_collection_updated_at"`
}

// ApplyOptimization applies one ranked result atomically to its collection.
func (s *ResearchService) ApplyOptimization(
	ctx context.Context, optimizationID string, req ResearchOptimizationApplyRequest,
) (ResearchCollectionDetail, error) {
	var zero ResearchCollectionDetail
	if !validOptimizationApplyRequest(req) {
		return zero, newErr("invalid_request", "objective, rank and expected_collection_updated_at are required", nil)
	}
	run, selected, err := s.loadOptimizationApplySelection(ctx, optimizationID, req)
	if err != nil {
		return zero, err
	}
	err = s.applyOptimizationSelection(ctx, run, selected, req.ExpectedCollectionUpdatedAt)
	if err != nil {
		var appErr *AppError
		if errors.As(err, &appErr) {
			return zero, appErr
		}
		if errors.Is(err, repository.ErrResearchCollectionNotFound) {
			return zero, newErr("research_collection_not_found", "research collection not found", nil)
		}
		return zero, wrapRepo("apply optimization result", err)
	}
	return s.GetCollection(ctx, run.CollectionID)
}

func validOptimizationApplyRequest(req ResearchOptimizationApplyRequest) bool {
	validObjective := req.Objective == ObjectiveMaxCAGR ||
		req.Objective == ObjectiveMinDrawdown || req.Objective == ObjectiveMaxCalmar ||
		req.Objective == ObjectiveMinCVaR
	return req.Rank > 0 && req.ExpectedCollectionUpdatedAt > 0 && validObjective
}

func (s *ResearchService) loadOptimizationApplySelection(
	ctx context.Context, optimizationID string, req ResearchOptimizationApplyRequest,
) (repository.ResearchOptimizationRun, OptimizationResultItem, error) {
	var zeroRun repository.ResearchOptimizationRun
	var zeroItem OptimizationResultItem
	run, err := s.research.GetOptimizationRun(ctx, optimizationID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
			return zeroRun, zeroItem, newErr("research_optimization_not_found", "optimization run not found", nil)
		}
		return zeroRun, zeroItem, wrapRepo("load optimization run", err)
	}
	if run.Status != repository.ResearchRunStatusSucceeded {
		return zeroRun, zeroItem, newErr("invalid_request", "only a succeeded optimization can be applied", nil)
	}
	var result OptimizationResult
	if err := json.Unmarshal([]byte(run.ResultJSON), &result); err != nil {
		return zeroRun, zeroItem, newErr(
			"research_optimization_result_stale", "optimization result is invalid", nil,
		)
	}
	selected, ok := findOptimizationResult(result, req.Objective, req.Rank)
	if !ok {
		return zeroRun, zeroItem, newErr("invalid_request", "objective and rank do not identify a result", nil)
	}
	var snapshot optimizationInputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot); err != nil ||
		!optimizationResultMatchesSnapshot(selected.Weights, snapshot.Assets) {
		return zeroRun, zeroItem, newErr(
			"research_optimization_result_stale", "optimization result does not match its frozen input", nil,
		)
	}
	return run, selected, nil
}

func optimizationResultMatchesSnapshot(
	entries []OptimizationWeightEntry, assets []researchSnapshotAsset,
) bool {
	if len(entries) != len(assets) || len(assets) == 0 {
		return false
	}
	identities := make(map[string]string, len(assets))
	for _, asset := range assets {
		if asset.ItemID == "" || asset.AssetKey == "" {
			return false
		}
		identities[asset.ItemID] = asset.AssetKey
	}
	seen := make(map[string]struct{}, len(entries))
	for _, entry := range entries {
		assetKey, ok := identities[entry.ItemID]
		if !ok || assetKey != entry.AssetKey {
			return false
		}
		if _, duplicate := seen[entry.ItemID]; duplicate {
			return false
		}
		seen[entry.ItemID] = struct{}{}
	}
	return len(seen) == len(assets)
}

func (s *ResearchService) applyOptimizationSelection(
	ctx context.Context,
	run repository.ResearchOptimizationRun,
	selected OptimizationResultItem,
	expectedUpdatedAt int64,
) error {
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		collection, err := s.research.GetCollectionTx(ctx, tx, run.CollectionID)
		if err != nil {
			return fmt.Errorf("load collection in apply transaction: %w", err)
		}
		if collection.UpdatedAt != expectedUpdatedAt {
			return newErr("research_collection_changed", "集合已发生变化，请刷新后重新预览", map[string]any{
				"expected_updated_at": expectedUpdatedAt, "actual_updated_at": collection.UpdatedAt,
			})
		}
		items, err := s.research.ListItemsTx(ctx, tx, collection.ID)
		if err != nil {
			return fmt.Errorf("list collection items in apply transaction: %w", err)
		}
		weights, err := validateOptimizationResultWeights(items, selected.Weights)
		if err != nil {
			return newErr("research_optimization_result_stale", err.Error(), nil)
		}
		now := maxInt64(s.now().UnixMilli(), collection.UpdatedAt+1)
		if err := s.updateOptimizationAppliedItems(ctx, tx, items, weights, now); err != nil {
			return err
		}
		collection.StartPolicy = ResearchStartPolicyCustom
		collection.WindowStart, collection.WindowEnd = run.WindowStart, run.WindowEnd
		if optimizationEngineHasTailRiskSnapshot(run.EngineVersion) {
			var snapshot optimizationInputSnapshot
			if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot); err != nil {
				return newErr("research_optimization_result_stale", "调优快照无法读取", nil)
			}
			spec, err := CanonicalTailRiskSpec(snapshot.Config.TailRisk)
			if err != nil {
				return newErr("research_optimization_result_stale", "调优快照的尾部风险口径无效", nil)
			}
			collection.TailRiskConfidence = spec.Confidence
			collection.TailRiskHorizonDays = spec.HorizonDays
		}
		collection.UpdatedAt = now
		if err := s.research.UpdateCollectionTx(ctx, tx, collection); err != nil {
			return fmt.Errorf("update collection in apply transaction: %w", err)
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("apply optimization transaction: %w", err)
	}
	return nil
}

func (s *ResearchService) updateOptimizationAppliedItems(
	ctx context.Context,
	tx *sql.Tx,
	items []repository.ResearchCollectionItem,
	weights map[string]float64,
	now int64,
) error {
	for _, item := range items {
		weight := weights[item.ID]
		item.Enabled, item.Weight, item.WeightLocked = weight > 0, weight, weight > 0
		item.UpdatedAt = now
		if err := s.research.UpdateItemTx(ctx, tx, item); err != nil {
			return fmt.Errorf("update collection item in apply transaction: %w", err)
		}
	}
	return nil
}

func maxInt64(a, b int64) int64 {
	if a > b {
		return a
	}
	return b
}

func findOptimizationResult(
	result OptimizationResult, objective OptimizationObjective, rank int,
) (OptimizationResultItem, bool) {
	var items []OptimizationResultItem
	switch objective {
	case ObjectiveMaxCAGR:
		items = result.BestByCAGR
	case ObjectiveMinDrawdown:
		items = result.BestByDrawdown
	case ObjectiveMaxCalmar:
		items = result.BestByCalmar
	case ObjectiveMinCVaR:
		items = result.BestByCVaR
	}
	for _, item := range items {
		if item.Rank == rank && item.Objective == objective {
			return item, true
		}
	}
	return OptimizationResultItem{}, false
}

func validateOptimizationResultWeights(
	items []repository.ResearchCollectionItem, entries []OptimizationWeightEntry,
) (map[string]float64, error) {
	const eps = 1e-12
	byID := make(map[string]repository.ResearchCollectionItem, len(items))
	for _, item := range items {
		byID[item.ID] = item
	}
	weights := make(map[string]float64, len(entries))
	sum := 0.0
	for _, entry := range entries {
		if entry.ItemID == "" || entry.AssetKey == "" || !optimizationFinite(entry.Weight) || entry.Weight < 0 {
			return nil, errOptimizationInvalidEntry
		}
		item, ok := byID[entry.ItemID]
		if !ok || item.AssetKey != entry.AssetKey {
			return nil, errOptimizationAssetMismatch
		}
		if _, duplicate := weights[entry.ItemID]; duplicate {
			return nil, errOptimizationDuplicateAsset
		}
		weights[entry.ItemID] = entry.Weight
		sum += entry.Weight
	}
	if math.Abs(sum-1) > eps {
		return nil, errOptimizationWeightSum
	}
	return weights, nil
}

// --- optimization task executor ---

// ExecuteOptimizationTask is the entry point called by the task worker
// for one research_optimization_backtest task.
func (s *ResearchService) ExecuteOptimizationTask(
	ctx context.Context,
	taskID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error {
	return s.ExecuteOptimizationTaskOwned(ctx, taskID, cancelCheck, progress, nil)
}

func (s *ResearchService) ExecuteOptimizationTaskOwned(
	ctx context.Context,
	taskID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
	complete func(*sql.Tx) error,
) error {
	run, err := s.research.GetOptimizationRunByTaskID(ctx, taskID)
	if err != nil {
		return fmt.Errorf("load optimization run for task %s: %w", taskID, err)
	}
	if run.Status == repository.ResearchRunStatusSucceeded {
		return nil
	}
	snapshot, ds, err := s.loadOptimizationExecution(ctx, run, progress)
	if err != nil {
		return err
	}
	if cancelCheck != nil && cancelCheck() {
		s.cancelOptimization(ctx, run.ID)
		return context.Canceled
	}
	assets := buildOptimizationExecutionAssets(snapshot, ds)
	candidateCount := CountCandidates(assets, snapshot.Config.WeightStep)
	if progress != nil {
		progress(0, candidateCount, "evaluating")
	}
	optResult, err := s.evaluateOptimizationCandidates(
		ctx, run.ID, snapshot, ds, assets, candidateCount, cancelCheck, progress,
	)
	if err != nil {
		return err
	}
	resultJSON, err := json.Marshal(optResult)
	if err != nil {
		s.failOptimization(ctx, run.ID, "result_marshal_failed", err.Error())
		return fmt.Errorf("marshal optimization result: %w", err)
	}

	completedAt := s.now().UnixMilli()
	if err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.research.CompleteOptimizationRunTx(ctx, tx, run.ID,
			string(resultJSON), optResult.EvaluatedCount, completedAt); err != nil {
			return fmt.Errorf("persist optimization result: %w", err)
		}
		if complete != nil {
			return complete(tx)
		}
		return nil
	}); err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("complete optimization after cancellation: %w", err)
		}
		s.failOptimization(ctx, run.ID, "persist_failed", err.Error())
		return fmt.Errorf("complete optimization run: %w", err)
	}

	if progress != nil {
		progress(candidateCount, candidateCount, "done")
	}
	return nil
}

func (s *ResearchService) loadOptimizationExecution(
	ctx context.Context,
	run repository.ResearchOptimizationRun,
	progress func(int, int, string),
) (optimizationInputSnapshot, *researchDataset, error) {
	var snapshot optimizationInputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot); err != nil {
		s.failOptimization(ctx, run.ID, "invalid_snapshot", "failed to decode snapshot")
		return snapshot, nil, fmt.Errorf("decode optimization snapshot: %w", err)
	}
	if err := s.research.MarkOptimizationRunning(ctx, run.ID); err != nil {
		return snapshot, nil, fmt.Errorf("mark optimization running: %w", err)
	}
	if progress != nil {
		progress(0, run.CandidateCount, "loading")
	}
	ds, err := s.loadDatasetFromSnapshot(ctx, researchInputSnapshot{
		EngineVersion: snapshot.EngineVersion, SourceHash: snapshot.SourceHash,
		CommonStart: snapshot.CommonStart, CommonEnd: snapshot.CommonEnd,
		WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
		Collection: snapshot.Collection, Assets: snapshot.Assets, FX: snapshot.FX,
		Benchmark: snapshot.Benchmark,
	})
	if err != nil {
		if ctx.Err() == nil {
			s.failOptimization(ctx, run.ID, "data_load_failed", err.Error())
		}
		return snapshot, nil, fmt.Errorf("load optimization dataset: %w", err)
	}
	rebuilt := buildResearchSnapshot(ds, ResearchReadiness{
		CommonStart: snapshot.CommonStart, CommonEnd: snapshot.CommonEnd,
		WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
	})
	if rebuilt.SourceHash != snapshot.SourceHash {
		s.failOptimization(ctx, run.ID, "source_changed", "source data changed since run was created")
		return snapshot, nil, fmt.Errorf("%w (optimization %s)", ErrResearchSourceChanged, run.ID)
	}
	return snapshot, ds, nil
}

func buildOptimizationExecutionAssets(
	snapshot optimizationInputSnapshot, ds *researchDataset,
) []OptimizationAsset {
	assets := make([]OptimizationAsset, 0, len(ds.Enabled))
	for _, asset := range ds.Enabled {
		lockedWeight, locked := snapshot.LockedWeights[asset.Item.ID]
		weight := asset.Item.Weight
		if locked {
			weight = lockedWeight
		}
		assets = append(assets, OptimizationAsset{
			ItemID: asset.Item.ID, AssetKey: asset.Item.AssetKey,
			Name: asset.Asset.Name, Weight: weight, Locked: locked,
		})
	}
	return assets
}

type optimizationCandidateTrackers struct {
	cagr, drawdown, calmar, cvar *TopKTracker
}

func newOptimizationCandidateTrackers(topK int) optimizationCandidateTrackers {
	return optimizationCandidateTrackers{
		cagr:     NewTopKTracker(ObjectiveMaxCAGR, topK),
		drawdown: NewTopKTracker(ObjectiveMinDrawdown, topK),
		calmar:   NewTopKTracker(ObjectiveMaxCalmar, topK),
		cvar:     NewTopKTracker(ObjectiveMinCVaR, topK),
	}
}

type optimizationCandidateEvaluation struct {
	candidate OptimizationWeightVector
	summary   *BacktestSummary
	err       error
}

type optimizationCandidateGeneration struct {
	count        int
	userCanceled bool
}

func (s *ResearchService) optimizationWorkerCount(candidateCount int) int {
	workers := s.optimizationConcurrency
	if workers < 1 {
		workers = DefaultResearchOptimizationConcurrency
	}
	if maxProcs := runtime.GOMAXPROCS(0); workers > maxProcs {
		workers = maxProcs
	}
	if candidateCount > 0 && workers > candidateCount {
		workers = candidateCount
	}
	return maxInt(1, workers)
}

func (s *ResearchService) startOptimizationCandidatePool(
	ctx context.Context,
	stop context.CancelFunc,
	snapshot optimizationInputSnapshot,
	ds *researchDataset,
	assets []OptimizationAsset,
	candidateCount int,
	cancelCheck func() bool,
) (<-chan optimizationCandidateEvaluation, <-chan optimizationCandidateGeneration) {
	workerCount := s.optimizationWorkerCount(candidateCount)
	candidates := make(chan OptimizationWeightVector, workerCount*2)
	generationDone := startOptimizationCandidateGenerator(
		ctx, stop, assets, snapshot.Config.WeightStep, cancelCheck, candidates,
	)
	evaluations := s.startOptimizationCandidateEvaluators(
		ctx, snapshot, ds, workerCount, candidates,
	)
	return evaluations, generationDone
}

func startOptimizationCandidateGenerator(
	ctx context.Context,
	stop context.CancelFunc,
	assets []OptimizationAsset,
	weightStep float64,
	cancelCheck func() bool,
	candidates chan<- OptimizationWeightVector,
) <-chan optimizationCandidateGeneration {
	done := make(chan optimizationCandidateGeneration, 1)
	go func() {
		defer close(candidates)
		userCanceled := false
		generated := VisitOptimizationCandidates(assets, weightStep, func(candidate OptimizationWeightVector) bool {
			if cancelCheck != nil && cancelCheck() {
				userCanceled = true
				stop()
				return false
			}
			select {
			case <-ctx.Done():
				return false
			case candidates <- candidate:
				return true
			}
		})
		done <- optimizationCandidateGeneration{count: generated, userCanceled: userCanceled}
	}()
	return done
}

func (s *ResearchService) startOptimizationCandidateEvaluators(
	ctx context.Context,
	snapshot optimizationInputSnapshot,
	ds *researchDataset,
	workerCount int,
	candidates <-chan OptimizationWeightVector,
) <-chan optimizationCandidateEvaluation {
	evaluations := make(chan optimizationCandidateEvaluation, workerCount)
	backtest := s.optimizationBacktest
	if backtest == nil {
		backtest = RunResearchBacktest
	}
	var workers sync.WaitGroup
	workers.Add(workerCount)
	for range workerCount {
		go func() {
			defer workers.Done()
			runOptimizationCandidateEvaluator(ctx, snapshot, ds, candidates, evaluations, backtest)
		}()
	}
	go func() {
		workers.Wait()
		close(evaluations)
	}()
	return evaluations
}

func runOptimizationCandidateEvaluator(
	ctx context.Context,
	snapshot optimizationInputSnapshot,
	ds *researchDataset,
	candidates <-chan OptimizationWeightVector,
	evaluations chan<- optimizationCandidateEvaluation,
	backtest func(BacktestInput) (*BacktestResult, error),
) {
	for {
		select {
		case <-ctx.Done():
			return
		case candidate, ok := <-candidates:
			if !ok {
				return
			}
			result, err := backtest(buildBacktestInputForCandidate(snapshot, ds, candidate))
			evaluation := optimizationCandidateEvaluation{candidate: candidate, err: err}
			if err == nil && result != nil {
				evaluation.summary = &result.Summary
			}
			select {
			case <-ctx.Done():
				return
			case evaluations <- evaluation:
			}
		}
	}
}

type optimizationEvaluationAccumulator struct {
	trackers                                     optimizationCandidateTrackers
	evaluated, skipped, cvarEligible, visited    int
	expectedEffectiveDays, expectedScenarioCount int
	progressInterval, candidateCount             int
	minimumCAGR                                  *float64
}

func newOptimizationEvaluationAccumulator(
	snapshot optimizationInputSnapshot, candidateCount int,
) *optimizationEvaluationAccumulator {
	return &optimizationEvaluationAccumulator{
		trackers:              newOptimizationCandidateTrackers(snapshot.Config.TopK),
		expectedEffectiveDays: -1, expectedScenarioCount: -1,
		progressInterval: maxInt(1, candidateCount/100), candidateCount: candidateCount,
		minimumCAGR: snapshot.Config.MinimumCAGR,
	}
}

func (a *optimizationEvaluationAccumulator) accept(evaluation optimizationCandidateEvaluation) error {
	if evaluation.err != nil || evaluation.summary == nil || evaluation.summary.TailRisk == nil {
		a.skipped++
	} else {
		if err := validateOptimizationCandidateSamples(
			*evaluation.summary, &a.expectedEffectiveDays, &a.expectedScenarioCount,
		); err != nil {
			return err
		}
		a.evaluated++
		if trackOptimizationCandidate(a.trackers, evaluation.candidate, *evaluation.summary, a.minimumCAGR) {
			a.cvarEligible++
		}
	}
	a.visited++
	return nil
}

func (a *optimizationEvaluationAccumulator) result() OptimizationResult {
	result := OptimizationResult{
		CandidateCount: a.candidateCount, EvaluatedCount: a.evaluated, SkippedCount: a.skipped,
		BestByCAGR: a.trackers.cagr.Results(), BestByDrawdown: a.trackers.drawdown.Results(),
		BestByCalmar: a.trackers.calmar.Results(), BestByCVaR: a.trackers.cvar.Results(),
		CVaREligibleCount: a.cvarEligible,
	}
	if a.cvarEligible == 0 {
		result.Warnings = append(result.Warnings, OptimizationWarning{
			Code: "cvar_minimum_cagr_unmet", Message: "没有候选达到最低 CAGR 门槛",
		})
	}
	return result
}

func (s *ResearchService) evaluateOptimizationCandidates(
	ctx context.Context,
	runID string,
	snapshot optimizationInputSnapshot,
	ds *researchDataset,
	assets []OptimizationAsset,
	candidateCount int,
	cancelCheck func() bool,
	progress func(int, int, string),
) (OptimizationResult, error) {
	accumulator := newOptimizationEvaluationAccumulator(snapshot, candidateCount)
	var stopErr error
	poolCtx, stopPool := context.WithCancel(ctx)
	defer stopPool()
	evaluations, generationDone := s.startOptimizationCandidatePool(
		poolCtx, stopPool, snapshot, ds, assets, candidateCount, cancelCheck,
	)
	for evaluation := range evaluations {
		if stopErr != nil {
			continue
		}
		if sampleErr := accumulator.accept(evaluation); sampleErr != nil {
			s.failOptimization(ctx, runID, "candidate_sample_mismatch", sampleErr.Error())
			stopErr = sampleErr
			stopPool()
			continue
		}
		if accumulator.visited%accumulator.progressInterval == 0 {
			publishOptimizationProgress(ctx, s.research, runID,
				accumulator.evaluated, accumulator.skipped, candidateCount, progress)
		}
	}
	generation := <-generationDone
	if generation.userCanceled {
		s.cancelOptimization(ctx, runID)
		return OptimizationResult{}, context.Canceled
	}
	if ctx.Err() != nil {
		return OptimizationResult{}, fmt.Errorf("optimization context ended: %w", ctx.Err())
	}
	if stopErr != nil {
		return OptimizationResult{}, stopErr
	}
	if generation.count != candidateCount || accumulator.visited != candidateCount {
		countErr := fmt.Errorf("%w: expected=%d generated=%d evaluated_or_skipped=%d",
			errOptimizationCandidateCount, candidateCount, generation.count, accumulator.visited)
		s.failOptimization(ctx, runID, "candidate_count_mismatch", countErr.Error())
		return OptimizationResult{}, countErr
	}
	if accumulator.evaluated == 0 {
		s.failOptimization(ctx, runID, "all_candidates_failed", "所有候选组合回测失败")
		return OptimizationResult{}, fmt.Errorf(
			"%w: candidate_count=%d", errOptimizationAllCandidates, candidateCount,
		)
	}
	return accumulator.result(), nil
}

func validateOptimizationCandidateSamples(
	summary BacktestSummary, expectedEffectiveDays, expectedScenarioCount *int,
) error {
	effectiveDays := summary.EffectiveReturnDays
	scenarioCount := summary.TailRisk.ScenarioCount
	if *expectedEffectiveDays < 0 {
		*expectedEffectiveDays, *expectedScenarioCount = effectiveDays, scenarioCount
		return nil
	}
	if effectiveDays == *expectedEffectiveDays && scenarioCount == *expectedScenarioCount {
		return nil
	}
	return fmt.Errorf(
		"%w: 候选组合使用了不一致的有效样本：期望 %d 个有效收益日/%d 个尾部场景，实际 %d/%d",
		errOptimizationSampleMismatch, *expectedEffectiveDays, *expectedScenarioCount,
		effectiveDays, scenarioCount,
	)
}

func trackOptimizationCandidate(
	trackers optimizationCandidateTrackers,
	candidate OptimizationWeightVector,
	summary BacktestSummary, minimumCAGR *float64,
) bool {
	item := OptimizationResultItem{Weights: candidate.Weights, Summary: summary}
	for objective, tracker := range map[OptimizationObjective]*TopKTracker{
		ObjectiveMaxCAGR:     trackers.cagr,
		ObjectiveMinDrawdown: trackers.drawdown,
		ObjectiveMaxCalmar:   trackers.calmar,
	} {
		if score, ok := ScoreForObjective(objective, summary); ok {
			scored := item
			scored.Score = score
			tracker.Push(scored)
		}
	}
	eligible := summary.TailRisk != nil && (minimumCAGR == nil || summary.CAGR >= *minimumCAGR)
	if eligible {
		if score, ok := ScoreForObjective(ObjectiveMinCVaR, summary); ok {
			scored := item
			scored.Score = score
			trackers.cvar.Push(scored)
		}
	}
	return eligible
}

func publishOptimizationProgress(
	ctx context.Context,
	repo *repository.ResearchRepo,
	runID string,
	evaluated, skipped, total int,
	progress func(int, int, string),
) {
	if progress != nil {
		progress(evaluated+skipped, total, "evaluating")
	}
	_ = repo.UpdateOptimizationProgress(ctx, runID, evaluated)
}

func buildBacktestInputForCandidate(
	snapshot optimizationInputSnapshot,
	ds *researchDataset,
	candidate OptimizationWeightVector,
) BacktestInput {
	weightByKey := map[string]float64{}
	for _, w := range candidate.Weights {
		weightByKey[w.AssetKey] = w.Weight
	}

	input := BacktestInput{
		BaseCurrency:            snapshot.Collection.BaseCurrency,
		InitialAmountMinor:      snapshot.Collection.InitialAmountMinor,
		RebalancePolicy:         snapshot.Collection.RebalancePolicy,
		RebalanceThreshold:      snapshot.Collection.RebalanceThreshold,
		RiskFreeRate:            snapshot.Collection.RiskFreeRate,
		TransactionCostRate:     snapshot.Collection.TransactionCostRate,
		TailRisk:                &snapshot.Config.TailRisk,
		FreezeEffectiveCalendar: true,
		WindowStart:             snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
		FX: map[string][]ResearchSeriesPoint{},
	}

	for _, a := range ds.Enabled {
		weight := weightByKey[a.Item.AssetKey]
		input.Assets = append(input.Assets, BacktestAssetInput{
			AssetKey: a.Item.AssetKey, Name: a.Asset.Name,
			Currency: a.Asset.Currency, Weight: weight, IsCash: a.IsCash,
			MaxFillGapDays: ResearchFillGapToleranceDays(a.Asset.InstrumentType),
			Points:         assetSeriesPoints(a.Points),
		})
	}

	for pair, fx := range ds.FX {
		if fx == nil || !fx.Found {
			continue
		}
		points := make([]ResearchSeriesPoint, 0, len(fx.Points))
		for _, p := range fx.Points {
			points = append(points, ResearchSeriesPoint{Date: p.TradeDate, Value: p.Value})
		}
		input.FX[pair] = points
	}
	if b := ds.Benchmark; b != nil {
		input.Benchmark = &BacktestBenchmarkInput{
			AssetKey: b.Item.AssetKey, Name: b.Asset.Name, Currency: b.Asset.Currency,
			IsCash: b.IsCash, MaxFillGapDays: ResearchFillGapToleranceDays(b.Asset.InstrumentType),
			Points: assetSeriesPoints(b.Points),
		}
	}

	return input
}

func (s *ResearchService) failOptimization(ctx context.Context, runID, code, msg string) {
	writeCtx := context.WithoutCancel(ctx)
	_ = s.research.FailOptimizationRun(writeCtx, runID,
		repository.ResearchRunStatusFailed, code, msg, s.now().UnixMilli())
}

func (s *ResearchService) cancelOptimization(ctx context.Context, runID string) {
	writeCtx := context.WithoutCancel(ctx)
	_ = s.research.FailOptimizationRun(writeCtx, runID,
		repository.ResearchRunStatusCanceled, "canceled", "user canceled", s.now().UnixMilli())
}
