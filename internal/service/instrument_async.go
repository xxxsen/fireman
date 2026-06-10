package service

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

// InstrumentResolveRequest resolves a symbol before async import.
type InstrumentResolveRequest struct {
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	Code           string `json:"code"`
}

// InstrumentImportAsyncRequest creates a placeholder instrument and fetch job.
type InstrumentImportAsyncRequest struct {
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	Code           string `json:"code"`
	ProviderSymbol string `json:"provider_symbol"`
}

// InstrumentImportAsyncResult is returned immediately after enqueue.
type InstrumentImportAsyncResult struct {
	InstrumentID string `json:"instrument_id"`
	JobID        string `json:"job_id"`
	Status       string `json:"status"`
}

// InstrumentFetchStatus aggregates instrument and job progress.
type InstrumentFetchStatus struct {
	InstrumentID     string `json:"instrument_id"`
	InstrumentStatus string `json:"instrument_status"`
	JobID            string `json:"job_id,omitempty"`
	JobStatus        string `json:"job_status,omitempty"`
	Phase            string `json:"phase,omitempty"`
	ProgressCurrent  int    `json:"progress_current"`
	ProgressTotal    int    `json:"progress_total"`
	ErrorCode        string `json:"error_code"`
	ErrorMessage     string `json:"error_message"`
}

// InstrumentFetchPayload is stored in jobs.payload_json.
type InstrumentFetchPayload = repository.InstrumentFetchPayload

func instrumentFetchInputHash(market, instrumentType, code, adjustPolicy string) string {
	raw := strings.ToLower(market) + "|" + instrumentType + "|" + code + "|" + adjustPolicy
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

func normalizeAsyncImport(req *InstrumentImportAsyncRequest) {
	req.Code = strings.TrimSpace(req.Code)
	req.ProviderSymbol = strings.TrimSpace(req.ProviderSymbol)
	if strings.EqualFold(req.Market, "HK") {
		req.Code = marketdata.NormalizeHKCode(req.Code)
		if req.ProviderSymbol != "" {
			req.ProviderSymbol = marketdata.NormalizeHKCode(req.ProviderSymbol)
		}
	}
	if strings.EqualFold(req.Market, "CN") && marketdata.HasCNExchangePrefix(req.Code) {
		req.Code = marketdata.NormalizeCNExchangeCode(req.Code)
	}
	if strings.EqualFold(req.Market, "CN") && marketdata.HasCNExchangePrefix(req.ProviderSymbol) {
		req.ProviderSymbol = marketdata.NormalizeCNExchangeCode(req.ProviderSymbol)
	}
	if req.ProviderSymbol == "" {
		req.ProviderSymbol = req.Code
	}
}

func (s *InstrumentService) Resolve(ctx context.Context, req InstrumentResolveRequest) (map[string]any, error) {
	req.Code = strings.TrimSpace(req.Code)
	if req.Market == "" || req.InstrumentType == "" || req.Code == "" {
		return nil, newErr("invalid_request", "market, instrument_type and code are required", nil)
	}
	if strings.EqualFold(req.Market, "HK") {
		req.Code = marketdata.NormalizeHKCode(req.Code)
	}
	data, err := s.provider.Resolve(ctx, marketdata.ResolveRequest{
		Market: req.Market, InstrumentType: req.InstrumentType, Code: req.Code,
	})
	if err != nil {
		msg := err.Error()
		if strings.Contains(msg, "instrument_not_found") {
			return nil, newErr("instrument_not_found", "instrument not found", nil)
		}
		return nil, newErr("market_provider_unavailable", msg, nil)
	}
	out := map[string]any{"ambiguous": data.Ambiguous}
	if data.Resolved != nil {
		out["resolved"] = map[string]any{
			"code": data.Resolved.Code, "provider_symbol": data.Resolved.ProviderSymbol,
			"name": data.Resolved.Name, "exchange": data.Resolved.Exchange,
			"instrument_kind": data.Resolved.InstrumentKind,
		}
	}
	if len(data.Candidates) > 0 {
		cands := make([]map[string]any, len(data.Candidates))
		for i, c := range data.Candidates {
			cands[i] = map[string]any{
				"code": c.Code, "provider_symbol": c.ProviderSymbol,
				"name": c.Name, "exchange": c.Exchange,
				"instrument_kind": c.InstrumentKind,
			}
		}
		out["candidates"] = cands
	}
	return out, nil
}

func (s *InstrumentService) ImportAsync(ctx context.Context, req InstrumentImportAsyncRequest) (InstrumentImportAsyncResult, error) {
	normalizeAsyncImport(&req)
	if req.Market == "" || req.InstrumentType == "" || req.Code == "" {
		return InstrumentImportAsyncResult{}, newErr("invalid_request", "market, instrument_type and code are required", nil)
	}
	if marketdata.RequiresCNResolve(req.Market, req.InstrumentType, req.Code) {
		return InstrumentImportAsyncResult{}, newErr("instrument_resolve_required", "resolve and select a prefixed code before import", nil)
	}
	if req.Code != req.ProviderSymbol {
		return InstrumentImportAsyncResult{}, newErr("instrument_resolve_required", "code and provider_symbol must match after resolve", nil)
	}

	adjust := marketdata.DefaultAdjustPolicy(req.InstrumentType)
	inputHash := instrumentFetchInputHash(req.Market, req.InstrumentType, req.Code, adjust)

	if existing, err := s.instRepo.FindByKey(ctx, req.Market, req.InstrumentType, req.Code, adjust); err == nil {
		if existing.Status == "active" || existing.Status == "fetch_failed" {
			return InstrumentImportAsyncResult{}, newErr("instrument_already_exists", "instrument already imported", map[string]any{"instrument_id": existing.ID})
		}
		if existing.Status == "pending_fetch" {
			if job, err := s.jobs.FindInProgressByInputHash(ctx, repository.JobTypeInstrumentFetch, inputHash); err == nil {
				return InstrumentImportAsyncResult{}, newErr("instrument_fetch_in_progress", "instrument fetch already in progress", map[string]any{
					"instrument_id": existing.ID, "job_id": job.ID,
				})
			}
		}
	} else if !errors.Is(err, repository.ErrInstrumentNotFound) {
		return InstrumentImportAsyncResult{}, err
	}

	if job, err := s.jobs.FindInProgressByInputHash(ctx, repository.JobTypeInstrumentFetch, inputHash); err == nil {
		var payload InstrumentFetchPayload
		_ = json.Unmarshal([]byte(job.PayloadJSON), &payload)
		details := map[string]any{"job_id": job.ID}
		if payload.InstrumentID != "" {
			details["instrument_id"] = payload.InstrumentID
		}
		return InstrumentImportAsyncResult{}, newErr("instrument_fetch_in_progress", "instrument fetch already in progress", details)
	} else if !errors.Is(err, repository.ErrJobNotFound) {
		return InstrumentImportAsyncResult{}, err
	}

	resolveData, err := s.provider.Resolve(ctx, marketdata.ResolveRequest{
		Market: req.Market, InstrumentType: req.InstrumentType, Code: req.Code,
	})
	if err == nil && resolveData.Ambiguous {
		return InstrumentImportAsyncResult{}, newErr("instrument_ambiguous", "code is still ambiguous; resolve and select a candidate", nil)
	}

	instID := "ins_" + uuid.New().String()
	jobID := "job_" + uuid.New().String()
	name := req.Code
	if resolveData != nil && resolveData.Resolved != nil && resolveData.Resolved.Name != "" {
		name = resolveData.Resolved.Name
	}
	inst := repository.InstrumentRecord{
		ID: instID, Code: req.Code, Name: name,
		Market: req.Market, InstrumentType: req.InstrumentType,
		AssetClass: "equity", Region: "domestic", Currency: defaultCurrency(req.Market),
		Provider: "akshare", ProviderSymbol: req.ProviderSymbol, AdjustPolicy: adjust,
		ExpenseRatioStatus: "unavailable",
		FeeTreatment:       marketdata.FeeTreatmentForType(req.InstrumentType),
		Status:             "pending_fetch",
	}
	payload := InstrumentFetchPayload{
		InstrumentID: instID, Market: req.Market, InstrumentType: req.InstrumentType,
		Code: req.Code, ProviderSymbol: req.ProviderSymbol, AdjustPolicy: adjust,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return InstrumentImportAsyncResult{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if _, findErr := s.instRepo.FindByKey(ctx, req.Market, req.InstrumentType, req.Code, adjust); findErr == nil {
			return newErr("instrument_already_exists", "instrument already imported", nil)
		} else if !errors.Is(findErr, repository.ErrInstrumentNotFound) {
			return findErr
		}
		if err := s.instRepo.Create(ctx, tx, inst); err != nil {
			return err
		}
		return s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, Type: repository.JobTypeInstrumentFetch, Status: repository.JobStatusQueued,
			InputHash: inputHash, PayloadJSON: string(payloadJSON),
			ProgressTotal: 1, Phase: "queued",
		})
	})
	if err != nil {
		var ae *AppError
		if errors.As(err, &ae) {
			return InstrumentImportAsyncResult{}, ae
		}
		return InstrumentImportAsyncResult{}, err
	}
	return InstrumentImportAsyncResult{InstrumentID: instID, JobID: jobID, Status: "pending_fetch"}, nil
}

func defaultCurrency(market string) string {
	switch strings.ToUpper(market) {
	case "HK":
		return "HKD"
	case "US":
		return "USD"
	default:
		return "CNY"
	}
}

func (s *InstrumentService) GetFetchStatus(ctx context.Context, instrumentID string) (InstrumentFetchStatus, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return InstrumentFetchStatus{}, newErr("instrument_not_found", "instrument not found", nil)
		}
		return InstrumentFetchStatus{}, err
	}
	out := InstrumentFetchStatus{
		InstrumentID: instrumentID, InstrumentStatus: inst.Status,
	}
	job, err := s.jobs.FindLatestInstrumentFetch(ctx, instrumentID)
	if errors.Is(err, repository.ErrJobNotFound) {
		return out, nil
	}
	if err != nil {
		return InstrumentFetchStatus{}, err
	}
	out.JobID = job.ID
	out.JobStatus = job.Status
	out.Phase = job.Phase
	out.ProgressCurrent = job.ProgressCurrent
	out.ProgressTotal = job.ProgressTotal
	out.ErrorCode = job.ErrorCode
	out.ErrorMessage = job.ErrorMessage
	return out, nil
}

func (s *InstrumentService) RetryFetch(ctx context.Context, instrumentID string) (InstrumentImportAsyncResult, error) {
	inst, err := s.instRepo.GetByID(ctx, instrumentID)
	if err != nil {
		if errors.Is(err, repository.ErrInstrumentNotFound) {
			return InstrumentImportAsyncResult{}, newErr("instrument_not_found", "instrument not found", nil)
		}
		return InstrumentImportAsyncResult{}, err
	}
	if inst.Status != "fetch_failed" {
		return InstrumentImportAsyncResult{}, newErr("invalid_request", "retry is only allowed for fetch_failed instruments", nil)
	}
	inputHash := instrumentFetchInputHash(inst.Market, inst.InstrumentType, inst.Code, inst.AdjustPolicy)
	if job, err := s.jobs.FindInProgressByInputHash(ctx, repository.JobTypeInstrumentFetch, inputHash); err == nil {
		return InstrumentImportAsyncResult{}, newErr("instrument_fetch_in_progress", "instrument fetch already in progress", map[string]any{
			"instrument_id": instrumentID, "job_id": job.ID,
		})
	} else if !errors.Is(err, repository.ErrJobNotFound) {
		return InstrumentImportAsyncResult{}, err
	}

	jobID := "job_" + uuid.New().String()
	payload := InstrumentFetchPayload{
		InstrumentID: inst.ID, Market: inst.Market, InstrumentType: inst.InstrumentType,
		Code: inst.Code, ProviderSymbol: inst.ProviderSymbol, AdjustPolicy: inst.AdjustPolicy,
	}
	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return InstrumentImportAsyncResult{}, err
	}

	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		if err := s.instRepo.UpdateStatusTx(ctx, tx, instrumentID, "pending_fetch"); err != nil {
			return err
		}
		return s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, Type: repository.JobTypeInstrumentFetch, Status: repository.JobStatusQueued,
			InputHash: inputHash, PayloadJSON: string(payloadJSON),
			ProgressTotal: 1, Phase: "queued",
		})
	})
	if err != nil {
		return InstrumentImportAsyncResult{}, err
	}
	return InstrumentImportAsyncResult{InstrumentID: instrumentID, JobID: jobID, Status: "pending_fetch"}, nil
}

func (s *InstrumentService) ensureNoFetchInProgress(ctx context.Context, inst repository.InstrumentRecord) error {
	if inst.Status == "pending_fetch" {
		return newErr("instrument_fetch_in_progress", "instrument fetch in progress", map[string]any{"instrument_id": inst.ID})
	}
	inputHash := instrumentFetchInputHash(inst.Market, inst.InstrumentType, inst.Code, inst.AdjustPolicy)
	if job, err := s.jobs.FindInProgressByInputHash(ctx, repository.JobTypeInstrumentFetch, inputHash); err == nil {
		return newErr("instrument_fetch_in_progress", "instrument fetch in progress", map[string]any{
			"instrument_id": inst.ID, "job_id": job.ID,
		})
	} else if !errors.Is(err, repository.ErrJobNotFound) {
		return err
	}
	return nil
}

// EnsureInstrumentReadyForPlan rejects non-active instruments in plan holdings.
func EnsureInstrumentReadyForPlan(inst repository.Instrument) error {
	if inst.Status != "active" {
		return newErr("instrument_not_ready", fmt.Sprintf("instrument status is %s", inst.Status), map[string]any{
			"instrument_id": inst.ID, "status": inst.Status,
		})
	}
	return nil
}
