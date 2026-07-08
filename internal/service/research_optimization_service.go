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
	"sort"
	"strconv"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/repository"
)

// research_optimization_service.go implements the service layer for
// portfolio auto-optimization (td/103): creation, readiness, querying
// and the job executor entry point.

// --- optimization readiness ---

// OptimizationReadiness is the response for optimization readiness checks.
type OptimizationReadiness struct {
	Ready           bool                     `json:"ready"`
	CandidateCount  int                      `json:"candidate_count"`
	EnabledCount    int                      `json:"enabled_count"`
	LockedCount     int                      `json:"locked_count"`
	TunableCount    int                      `json:"tunable_count"`
	LockedWeightSum float64                  `json:"locked_weight_sum"`
	BlockingReasons []ResearchReadinessIssue `json:"blocking_reasons"`
	Warnings        []ResearchReadinessIssue `json:"warnings"`
}

// GetOptimizationReadiness checks whether a collection is ready for
// auto-optimization (td/103 §Readiness).
func (s *ResearchService) GetOptimizationReadiness(
	ctx context.Context, collectionID string, weightStep float64,
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

	out := evaluateOptimizationReadiness(ds, weightStep)

	// Merge data-dependency blocking reasons from standard readiness
	// (excluding weight_sum_invalid) so the readiness endpoint is
	// consistent with the creation endpoint.
	stdReadiness := evaluateResearchReadiness(ds, s.now())
	for _, b := range stdReadiness.BlockingReasons {
		if b.Reason == ResearchReasonWeightSumInvalid {
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
	out.Ready = len(out.BlockingReasons) == 0

	return out, nil
}

func evaluateOptimizationReadiness(
	ds *researchDataset, weightStep float64,
) OptimizationReadiness {
	if weightStep <= 0 {
		weightStep = OptimizationDefaultWeightStep
	}

	out := OptimizationReadiness{
		BlockingReasons: []ResearchReadinessIssue{},
		Warnings:        []ResearchReadinessIssue{},
	}
	block := func(issue ResearchReadinessIssue) {
		out.BlockingReasons = append(out.BlockingReasons, issue)
	}

	out.EnabledCount = len(ds.Enabled)
	if out.EnabledCount == 0 {
		block(ResearchReadinessIssue{
			Reason: ResearchReasonNoEnabledAssets, Message: "集合没有启用的资产",
		})
	}
	if out.EnabledCount > OptimizationMaxEnabledAssets {
		block(ResearchReadinessIssue{
			Reason: "too_many_enabled_assets",
			Message: fmt.Sprintf("启用资产数量 %d 超过上限 %d",
				out.EnabledCount, OptimizationMaxEnabledAssets),
		})
	}

	var assets []OptimizationAsset
	for _, a := range ds.Enabled {
		assets = append(assets, OptimizationAsset{
			ItemID:   a.Item.ID,
			AssetKey: a.Item.AssetKey,
			Name:     a.Asset.Name,
			Weight:   a.Item.Weight,
			Locked:   a.Item.WeightLocked,
		})
		if a.Item.WeightLocked {
			out.LockedCount++
			out.LockedWeightSum += a.Item.Weight
		} else {
			out.TunableCount++
		}
	}

	if out.LockedWeightSum > 1+ResearchWeightTolerance {
		block(ResearchReadinessIssue{
			Reason:  "locked_weight_exceeds_100",
			Message: fmt.Sprintf("锁定权重合计 %.2f%% 超过 100%%", out.LockedWeightSum*100),
		})
	}

	for _, a := range ds.Enabled {
		if a.IsCash {
			continue
		}
		if len(a.Points) == 0 {
			block(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchReasonHistoryMissing,
				Message: "缺少历史点位",
			})
		}
		if a.Task != nil && repository.IsActiveWorkerTaskStatus(a.Task.Status) {
			block(ResearchReadinessIssue{
				AssetKey: a.Item.AssetKey, Reason: ResearchReasonHistorySyncing,
				Message: "历史同步正在运行",
			})
		}
		for _, pair := range a.FXPairs {
			fx, ok := ds.FX[pair]
			if !ok || !fx.Found {
				block(ResearchReadinessIssue{
					AssetKey: a.Item.AssetKey, Pair: pair, Reason: ResearchReasonFXMissing,
					Message: fmt.Sprintf("%s 资产需要 %s 历史汇率", a.Asset.Currency, pair),
				})
			}
		}
	}

	if out.EnabledCount > 0 && out.EnabledCount <= OptimizationMaxEnabledAssets {
		out.CandidateCount = CountCandidates(assets, weightStep)
		if out.CandidateCount > OptimizationHardMaxCandidate {
			block(ResearchReadinessIssue{
				Reason: "candidate_count_exceeds_limit",
				Message: fmt.Sprintf("候选数量 %d 超过上限 %d，请增大步长或减少资产",
					out.CandidateCount, OptimizationHardMaxCandidate),
			})
		}
		if out.CandidateCount == 0 && out.TunableCount == 0 &&
			math.Abs(out.LockedWeightSum-1) <= ResearchWeightTolerance {
			out.Warnings = append(out.Warnings, ResearchReadinessIssue{
				Reason:  "all_locked",
				Message: "所有资产权重已锁定，自动调优将退化为固定组合回测，建议使用普通回测",
			})
		}
	}

	out.Ready = len(out.BlockingReasons) == 0
	return out
}

// --- create optimization ---

// ResearchOptimizationRequest is the POST /collections/{id}/optimizations body.
type ResearchOptimizationRequest struct {
	WeightStep        float64 `json:"weight_step"`
	MaxCandidateCount int     `json:"max_candidate_count"`
	TopK              int     `json:"top_k"`
}

// ResearchOptimizationCreateResult is the creation response.
type ResearchOptimizationCreateResult struct {
	Optimization ResearchOptimizationView `json:"optimization"`
	Reused       bool                     `json:"reused"`
}

// ResearchOptimizationView is the API shape of one optimization run.
type ResearchOptimizationView struct {
	ID              string           `json:"id"`
	CollectionID    string           `json:"collection_id"`
	JobID           string           `json:"job_id"`
	Status          string           `json:"status"`
	Config          json.RawMessage  `json:"config"`
	CandidateCount  int              `json:"candidate_count"`
	EvaluatedCount  int              `json:"evaluated_count"`
	Result          json.RawMessage  `json:"result,omitempty"`
	ErrorCode       string           `json:"error_code,omitempty"`
	ErrorMessage    string           `json:"error_message,omitempty"`
	BaseCurrency    string           `json:"base_currency"`
	RebalancePolicy string           `json:"rebalance_policy"`
	WindowStart     string           `json:"window_start"`
	WindowEnd       string           `json:"window_end"`
	EngineVersion   string           `json:"engine_version"`
	CreatedAt       int64            `json:"created_at"`
	CompletedAt     *int64           `json:"completed_at,omitempty"`
	Job             *ResearchJobView `json:"job,omitempty"`
}

func buildOptimizationView(run repository.ResearchOptimizationRun) ResearchOptimizationView {
	view := ResearchOptimizationView{
		ID:              run.ID,
		CollectionID:    run.CollectionID,
		JobID:           run.JobID,
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
	LockedWeights  map[string]float64       `json:"locked_weights"`
	TunableItemIDs []string                 `json:"tunable_item_ids"`
	Config         OptimizationConfig       `json:"config"`
}

// CreateOptimization creates an optimization run (td/103 §新增接口).
func (s *ResearchService) CreateOptimization(
	ctx context.Context, collectionID string, req ResearchOptimizationRequest,
) (ResearchOptimizationCreateResult, error) {
	var zero ResearchOptimizationCreateResult
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

	ds, err := s.loadResearchDataset(ctx, collection)
	if err != nil {
		return zero, err
	}

	config := OptimizationConfig{
		WeightStep:        req.WeightStep,
		MaxCandidateCount: req.MaxCandidateCount,
		TopK:              req.TopK,
	}
	config.NormalizeDefaults()

	optReadiness := evaluateOptimizationReadiness(ds, config.WeightStep)
	if !optReadiness.Ready {
		return zero, newErr("research_optimization_not_ready",
			"集合未通过调优准入检查",
			map[string]any{"readiness": optReadiness})
	}

	// Check data dependencies (standard readiness minus weight_sum_invalid)
	stdReadiness := evaluateResearchReadiness(ds, s.now())
	filteredBlocking := make([]ResearchReadinessIssue, 0)
	for _, b := range stdReadiness.BlockingReasons {
		if b.Reason == ResearchReasonWeightSumInvalid {
			continue
		}
		filteredBlocking = append(filteredBlocking, b)
	}
	if len(filteredBlocking) > 0 {
		return zero, newErr("research_optimization_not_ready",
			"集合未通过数据准入检查",
			map[string]any{"blocking_reasons": filteredBlocking})
	}

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

	// Idempotency: reuse succeeded or active optimization with same input hash
	if run, err := s.research.FindSucceededOptimizationByInputHash(ctx, collectionID, inputHash); err == nil {
		view := buildOptimizationView(run)
		return ResearchOptimizationCreateResult{Optimization: view, Reused: true}, nil
	} else if !errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
		return zero, wrapRepo("find succeeded optimization", err)
	}
	if run, err := s.research.FindActiveOptimizationByInputHash(ctx, collectionID, inputHash); err == nil {
		view := buildOptimizationView(run)
		if job, err := s.jobs.GetByID(ctx, run.JobID); err == nil {
			view.Job = &ResearchJobView{
				Status: job.Status, Phase: job.Phase,
				ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
				ErrorCode: job.ErrorCode, ErrorMessage: job.ErrorMessage,
			}
		}
		return ResearchOptimizationCreateResult{Optimization: view, Reused: true}, nil
	} else if !errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
		return zero, wrapRepo("find active optimization", err)
	}

	now := s.now().UnixMilli()
	runID := "ror_" + uuid.New().String()
	jobID := "job_" + uuid.New().String()
	payloadJSON, err := json.Marshal(map[string]string{
		"optimization_run_id": runID, "collection_id": collectionID,
	})
	if err != nil {
		return zero, fmt.Errorf("marshal job payload: %w", err)
	}

	candidateCount := CountCandidates(buildOptimizationAssets(ds), config.WeightStep)

	run := repository.ResearchOptimizationRun{
		ID: runID, CollectionID: collectionID, JobID: jobID,
		Status: repository.ResearchRunStatusQueued, InputHash: inputHash,
		SourceHash: snapshot.SourceHash, EngineVersion: OptimizationEngineVersion,
		BaseCurrency: collection.BaseCurrency, RebalancePolicy: collection.RebalancePolicy,
		WindowStart: stdReadiness.WindowStart, WindowEnd: stdReadiness.WindowEnd,
		ConfigJSON: string(configJSON), InputSnapshotJSON: string(snapshotJSON),
		CandidateCount: candidateCount, EvaluatedCount: 0, ResultJSON: "{}",
		CreatedAt: now,
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, Type: repository.JobTypeResearchOptimization,
			Status: repository.JobStatusQueued, InputHash: inputHash,
			PayloadJSON: string(payloadJSON), CreatedAt: now,
		}); err != nil {
			return err
		}
		return s.research.CreateOptimizationRunTx(ctx, tx, run)
	})
	if err != nil {
		return zero, wrapRepo("create optimization run", err)
	}

	view := buildOptimizationView(run)
	return ResearchOptimizationCreateResult{Optimization: view}, nil
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
		FX: baseSnapshot.FX, LockedWeights: map[string]float64{}, Config: config,
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
	fmt.Fprintf(h, "config:%s|%d|%d\n",
		strconv.FormatFloat(snapshot.Config.WeightStep, 'g', 17, 64),
		snapshot.Config.MaxCandidateCount, snapshot.Config.TopK)
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
	if job, err := s.jobs.GetByID(ctx, run.JobID); err == nil {
		view.Job = &ResearchJobView{
			Status: job.Status, Phase: job.Phase,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ErrorCode: job.ErrorCode, ErrorMessage: job.ErrorMessage,
		}
	}
	return view, nil
}

// GetLatestOptimization returns the most recent optimization run for a
// collection (any status), used for the collection detail page entry point.
func (s *ResearchService) GetLatestOptimization(
	ctx context.Context, collectionID string,
) (*ResearchOptimizationView, error) {
	run, err := s.research.LatestOptimizationByCollection(ctx, collectionID)
	if err != nil {
		if errors.Is(err, repository.ErrResearchOptimizationRunNotFound) {
			return nil, nil
		}
		return nil, wrapRepo("load latest optimization", err)
	}
	view := buildOptimizationView(run)
	if job, err := s.jobs.GetByID(ctx, run.JobID); err == nil {
		view.Job = &ResearchJobView{
			Status: job.Status, Phase: job.Phase,
			ProgressCurrent: job.ProgressCurrent, ProgressTotal: job.ProgressTotal,
			ErrorCode: job.ErrorCode, ErrorMessage: job.ErrorMessage,
		}
	}
	return &view, nil
}

// --- optimization job executor ---

// ExecuteOptimizationJob is the entry point called by the jobs worker
// for one research_optimization_backtest job (td/103 §Worker 执行流程).
func (s *ResearchService) ExecuteOptimizationJob(
	ctx context.Context,
	jobID string,
	cancelCheck func() bool,
	progress func(done, total int, phase string),
) error {
	run, err := s.research.GetOptimizationRunByJobID(ctx, jobID)
	if err != nil {
		return fmt.Errorf("load optimization run for job %s: %w", jobID, err)
	}
	if run.Status == repository.ResearchRunStatusSucceeded {
		return nil
	}
	var snapshot optimizationInputSnapshot
	if err := json.Unmarshal([]byte(run.InputSnapshotJSON), &snapshot); err != nil {
		s.failOptimization(ctx, run.ID, "invalid_snapshot", "failed to decode snapshot")
		return fmt.Errorf("decode optimization snapshot: %w", err)
	}
	if err := s.research.MarkOptimizationRunning(ctx, run.ID); err != nil {
		return err
	}

	if progress != nil {
		progress(0, run.CandidateCount, "loading")
	}

	ds, err := s.loadDatasetFromSnapshot(ctx, researchInputSnapshot{
		EngineVersion: snapshot.EngineVersion, SourceHash: snapshot.SourceHash,
		CommonStart: snapshot.CommonStart, CommonEnd: snapshot.CommonEnd,
		WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
		Collection: snapshot.Collection, Assets: snapshot.Assets, FX: snapshot.FX,
	})
	if err != nil {
		if ctx.Err() != nil {
			return err
		}
		s.failOptimization(ctx, run.ID, "data_load_failed", err.Error())
		return err
	}

	rebuilt := buildResearchSnapshot(ds, ResearchReadiness{
		CommonStart: snapshot.CommonStart, CommonEnd: snapshot.CommonEnd,
		WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
	})
	if rebuilt.SourceHash != snapshot.SourceHash {
		s.failOptimization(ctx, run.ID, "source_changed", "source data changed since run was created")
		return fmt.Errorf("%w (optimization %s)", ErrResearchSourceChanged, run.ID)
	}
	if cancelCheck != nil && cancelCheck() {
		s.cancelOptimization(ctx, run.ID)
		return context.Canceled
	}

	var optAssets []OptimizationAsset
	for _, a := range ds.Enabled {
		lockedWeight, locked := snapshot.LockedWeights[a.Item.ID]
		weight := a.Item.Weight
		if locked {
			weight = lockedWeight
		}
		optAssets = append(optAssets, OptimizationAsset{
			ItemID: a.Item.ID, AssetKey: a.Item.AssetKey,
			Name: a.Asset.Name, Weight: weight, Locked: locked,
		})
	}

	candidates := GenerateCandidates(optAssets, snapshot.Config.WeightStep)
	if progress != nil {
		progress(0, len(candidates), "evaluating")
	}

	topK := snapshot.Config.TopK
	cagrTracker := NewTopKTracker(ObjectiveMaxCAGR, topK)
	ddTracker := NewTopKTracker(ObjectiveMinDrawdown, topK)
	calmarTracker := NewTopKTracker(ObjectiveMaxCalmar, topK)

	evaluated, skipped := 0, 0
	progressInterval := maxInt(1, len(candidates)/100)

	for i, candidate := range candidates {
		if cancelCheck != nil && cancelCheck() {
			s.cancelOptimization(ctx, run.ID)
			return context.Canceled
		}

		input := buildBacktestInputForCandidate(snapshot, ds, candidate)
		result, err := RunResearchBacktest(input)
		if err != nil {
			skipped++
			continue
		}
		evaluated++

		item := OptimizationResultItem{Weights: candidate.Weights, Summary: result.Summary}

		if score, ok := ScoreForObjective(ObjectiveMaxCAGR, result.Summary); ok {
			item.Score = score
			cagrTracker.Push(item)
		}
		if score, ok := ScoreForObjective(ObjectiveMinDrawdown, result.Summary); ok {
			ddItem := item
			ddItem.Score = score
			ddTracker.Push(ddItem)
		}
		if score, ok := ScoreForObjective(ObjectiveMaxCalmar, result.Summary); ok {
			calmarItem := item
			calmarItem.Score = score
			calmarTracker.Push(calmarItem)
		}

		if (i+1)%progressInterval == 0 {
			if progress != nil {
				progress(evaluated+skipped, len(candidates), "evaluating")
			}
			_ = s.research.UpdateOptimizationProgress(ctx, run.ID, evaluated)
		}
	}

	if evaluated == 0 {
		s.failOptimization(ctx, run.ID, "all_candidates_failed", "所有候选组合回测失败")
		return fmt.Errorf("all %d candidates failed", len(candidates))
	}

	optResult := OptimizationResult{
		CandidateCount: len(candidates), EvaluatedCount: evaluated, SkippedCount: skipped,
		BestByCAGR: cagrTracker.Results(), BestByDrawdown: ddTracker.Results(),
		BestByCalmar: calmarTracker.Results(),
	}
	resultJSON, err := json.Marshal(optResult)
	if err != nil {
		s.failOptimization(ctx, run.ID, "result_marshal_failed", err.Error())
		return fmt.Errorf("marshal optimization result: %w", err)
	}

	completedAt := s.now().UnixMilli()
	if err := s.research.CompleteOptimizationRun(ctx, run.ID,
		string(resultJSON), evaluated, completedAt); err != nil {
		if ctx.Err() != nil {
			return err
		}
		s.failOptimization(ctx, run.ID, "persist_failed", err.Error())
		return err
	}

	if progress != nil {
		progress(len(candidates), len(candidates), "done")
	}
	return nil
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
		BaseCurrency: snapshot.Collection.BaseCurrency,
		RebalancePolicy: snapshot.Collection.RebalancePolicy,
		RebalanceThreshold: snapshot.Collection.RebalanceThreshold,
		RiskFreeRate: snapshot.Collection.RiskFreeRate,
		TransactionCostRate: snapshot.Collection.TransactionCostRate,
		WindowStart: snapshot.WindowStart, WindowEnd: snapshot.WindowEnd,
		FX: map[string][]ResearchSeriesPoint{},
	}

	for _, a := range ds.Enabled {
		weight := weightByKey[a.Item.AssetKey]
		if weight <= 0 {
			continue
		}
		input.Assets = append(input.Assets, BacktestAssetInput{
			AssetKey: a.Item.AssetKey, Name: a.Asset.Name,
			Currency: a.Asset.Currency, Weight: weight, IsCash: a.IsCash,
			MaxFillGapDays: ResearchFillGapToleranceDays(a.Asset.InstrumentType),
			Points: assetSeriesPoints(a.Points),
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

