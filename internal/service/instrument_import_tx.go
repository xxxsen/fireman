package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

type pendingInstrumentImport struct {
	instID, jobID string
}

func (s *InstrumentService) createPendingInstrumentImport(
	ctx context.Context,
	req InstrumentImportAsyncRequest,
	market, instrumentType, code, adjust, inputHash string,
) (pendingInstrumentImport, error) {
	instID := "ins_" + uuid.New().String()
	jobID := "job_" + uuid.New().String()
	err := fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		ticket, err := s.tickets.Consume(ctx, tx, req.TicketID)
		if err != nil {
			return mapTicketError(err)
		}
		if !IsImportableCandidate(ticket.InstrumentType, ticket.InstrumentKind) {
			return newErr("invalid_request", "instrument kind is not compatible with instrument type", map[string]any{
				"instrument_type": ticket.InstrumentType, "instrument_kind": ticket.InstrumentKind,
			})
		}
		inst := repository.InstrumentRecord{
			ID: instID, Code: code, Name: ticket.Name,
			Market: market, InstrumentType: instrumentType,
			AssetClass: req.AssetClass, Region: req.Region, Currency: defaultCurrency(market),
			Provider: "akshare", ProviderSymbol: ticket.ProviderSymbol, AdjustPolicy: adjust,
			ExpenseRatioStatus: "unavailable",
			FeeTreatment:       marketdata.FeeTreatmentForType(instrumentType),
			Status:             "pending_fetch",
		}
		payload := InstrumentFetchPayload{
			InstrumentID: instID, Market: market, InstrumentType: instrumentType,
			Code: code, ProviderSymbol: ticket.ProviderSymbol, AdjustPolicy: adjust,
			ResolvedName: ticket.Name, InstrumentKind: ticket.InstrumentKind,
			UserAssetClass: req.AssetClass, UserRegion: req.Region,
		}
		payloadJSON, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, findErr := s.instRepo.FindByKey(ctx, market, instrumentType, code, adjust); findErr == nil {
			return newErr("instrument_already_exists", "instrument already imported", nil)
		} else if !errors.Is(findErr, repository.ErrInstrumentNotFound) {
			return wrapRepo("find instrument by key in tx", findErr)
		}
		if err := s.instRepo.Create(ctx, tx, inst); err != nil {
			return wrapRepo("create instrument", err)
		}
		if err := s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, Type: repository.JobTypeInstrumentFetch, Status: repository.JobStatusQueued,
			InputHash: inputHash, PayloadJSON: string(payloadJSON),
			ProgressTotal: 1, Phase: "queued",
		}); err != nil {
			if repository.IsJobUniqueConstraint(err) {
				return s.returnFetchInProgress(ctx, inputHash)
			}
			return wrapRepo("create fetch job", err)
		}
		return nil
	})
	if err != nil {
		return pendingInstrumentImport{}, wrapRepo("create pending instrument import", err)
	}
	return pendingInstrumentImport{instID: instID, jobID: jobID}, nil
}

func (s *InstrumentService) loadRefreshableInstrument(
	ctx context.Context,
	instrumentID string,
	opts InstrumentRefreshOptions,
) (repository.InstrumentRecord, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return repository.InstrumentRecord{}, newErr("instrument_not_found", "instrument not found", nil)
		}
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	if inst.IsSystem || inst.Provider != "akshare" {
		return repository.InstrumentRecord{}, newErr(
			"instrument_not_refreshable",
			"only AKShare instruments can be refreshed",
			nil,
		)
	}
	if err := s.ensureNoFetchInProgress(ctx, inst); err != nil {
		return repository.InstrumentRecord{}, wrapRepo("load instrument", err)
	}
	lastFetched, _ := s.marketRepo.LastFetchedAt(ctx, instrumentID)
	namePlaceholder := inst.Name == "" || inst.Name == inst.Code
	throttled := !opts.Force && lastFetched > 0 &&
		time.Now().UnixMilli()-lastFetched < 24*time.Hour.Milliseconds() &&
		!namePlaceholder
	if throttled {
		return inst, newErr("instrument_refresh_throttled", "instrument refreshed within last 24 hours", nil)
	}
	return inst, nil
}

type commitDraftState struct {
	plan             repository.Plan
	detail           RebalanceDraftDetail
	existing         []repository.PlanHolding
	plannedByHolding map[string]int64
	net              int64
}

func (s *RebalanceDraftService) loadCommitDraftState(
	ctx context.Context,
	planID, _ string,
	req CommitRebalanceDraftRequest,
	draft repository.RebalanceDraft,
) (commitDraftState, error) {
	plan, err := s.plans.GetByID(ctx, planID)
	if err != nil {
		return commitDraftState{}, wrapRepo("get plan for commit", err)
	}
	if err := validateCommitPlanVersions(req, plan, draft); err != nil {
		return commitDraftState{}, err
	}
	detail, err := s.buildDetail(ctx, draft)
	if err != nil {
		return commitDraftState{}, wrapRepo("build draft detail for commit", err)
	}
	for _, line := range detail.Lines {
		if line.PlannedCurrentMinor < 0 {
			return commitDraftState{}, newErr("validation_failed", "planned amount cannot be negative", nil)
		}
	}
	existing, err := s.holdings.ListByPlan(ctx, planID)
	if err != nil {
		return commitDraftState{}, wrapRepo("list holdings for commit", err)
	}
	plannedByHolding := make(map[string]int64, len(detail.Lines))
	for _, line := range detail.Lines {
		plannedByHolding[line.HoldingID] = line.PlannedCurrentMinor
	}
	net := detail.FundPool.NetMinor
	net, err = resolveCommitFundPool(net, req, existing, plannedByHolding)
	if err != nil {
		return commitDraftState{}, err
	}
	return commitDraftState{
		plan: plan, detail: detail, existing: existing,
		plannedByHolding: plannedByHolding, net: net,
	}, nil
}
