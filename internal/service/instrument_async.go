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
	"time"

	"github.com/google/uuid"

	fdb "github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

const resolutionTicketTTL = 15 * time.Minute

// InstrumentResolveRequest resolves a symbol before async import.
type InstrumentResolveRequest struct {
	Market         string `json:"market"`
	InstrumentType string `json:"instrument_type"`
	Code           string `json:"code"`
}

// InstrumentImportAsyncRequest creates a placeholder instrument and fetch job.
type InstrumentImportAsyncRequest struct {
	TicketID string `json:"ticket_id"`
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

func validateMarketInstrumentType(market, instrumentType string) error {
	valid := map[string]map[string]bool{
		"CN": {
			"cn_exchange_fund":  true,
			"cn_exchange_stock": true,
			"cn_mutual_fund":    true,
			"fx_rate":           true,
		},
		"HK": {"hk_stock": true, "hk_etf": true},
		"US": {"us_stock": true, "us_etf": true},
	}
	types, ok := valid[strings.ToUpper(market)]
	if !ok || !types[instrumentType] {
		return newErr("invalid_request", "market and instrument_type combination is not supported", nil)
	}
	return nil
}

func (s *InstrumentService) Resolve(ctx context.Context, req InstrumentResolveRequest) (map[string]any, error) {
	req.Code = strings.TrimSpace(req.Code)
	if req.Market == "" || req.InstrumentType == "" || req.Code == "" {
		return nil, newErr("invalid_request", "market, instrument_type and code are required", nil)
	}
	if err := validateMarketInstrumentType(req.Market, req.InstrumentType); err != nil {
		return nil, err
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
		if strings.Contains(msg, "invalid_request") {
			return nil, newErr("invalid_request", msg, nil)
		}
		return nil, newErr("market_provider_unavailable", msg, nil)
	}
	out := map[string]any{"ambiguous": data.Ambiguous}
	if data.Resolved != nil {
		ticketID, err := s.createResolutionTicket(ctx, req.Market, req.InstrumentType, *data.Resolved)
		if err != nil {
			return nil, err
		}
		out["resolved"] = map[string]any{
			"code": data.Resolved.Code, "provider_symbol": data.Resolved.ProviderSymbol,
			"name": data.Resolved.Name, "exchange": data.Resolved.Exchange,
			"instrument_kind": data.Resolved.InstrumentKind, "ticket_id": ticketID,
		}
	}
	if len(data.Candidates) > 0 {
		cands := make([]map[string]any, len(data.Candidates))
		for i, c := range data.Candidates {
			ticketID, err := s.createResolutionTicket(ctx, req.Market, req.InstrumentType, c)
			if err != nil {
				return nil, err
			}
			cands[i] = map[string]any{
				"code": c.Code, "provider_symbol": c.ProviderSymbol,
				"name": c.Name, "exchange": c.Exchange,
				"instrument_kind": c.InstrumentKind, "ticket_id": ticketID,
			}
		}
		out["candidates"] = cands
	}
	return out, nil
}

func (s *InstrumentService) createResolutionTicket(ctx context.Context, market, instrumentType string, c marketdata.ResolveCandidate) (string, error) {
	if s.tickets == nil {
		return "", errors.New("resolution ticket repo not configured")
	}
	id := "tkt_" + uuid.New().String()
	now := time.Now()
	ticket := repository.ResolutionTicket{
		ID:             id,
		Market:         market,
		InstrumentType: instrumentType,
		Code:           c.Code,
		ProviderSymbol: c.ProviderSymbol,
		Name:           c.Name,
		Exchange:       c.Exchange,
		InstrumentKind: c.InstrumentKind,
		CreatedAt:      now.UnixMilli(),
		ExpiresAt:      now.Add(resolutionTicketTTL).UnixMilli(),
	}
	if err := s.tickets.Create(ctx, nil, ticket); err != nil {
		return "", err
	}
	return id, nil
}

func (s *InstrumentService) ImportAsync(ctx context.Context, req InstrumentImportAsyncRequest) (InstrumentImportAsyncResult, error) {
	req.TicketID = strings.TrimSpace(req.TicketID)
	if req.TicketID == "" {
		return InstrumentImportAsyncResult{}, newErr("invalid_request", "ticket_id is required", nil)
	}
	if s.tickets == nil {
		return InstrumentImportAsyncResult{}, errors.New("resolution ticket repo not configured")
	}

	ticketPreview, err := s.tickets.GetByID(ctx, req.TicketID)
	if err != nil {
		return InstrumentImportAsyncResult{}, mapTicketError(err)
	}
	market := ticketPreview.Market
	instrumentType := ticketPreview.InstrumentType
	code := ticketPreview.Code
	adjust := marketdata.DefaultAdjustPolicy(instrumentType)
	inputHash := instrumentFetchInputHash(market, instrumentType, code, adjust)

	if existing, err := s.instRepo.FindByKey(ctx, market, instrumentType, code, adjust); err == nil {
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

	instID := "ins_" + uuid.New().String()
	jobID := "job_" + uuid.New().String()
	var ticket repository.ResolutionTicket
	var providerSymbol string
	inst := repository.InstrumentRecord{}
	payload := InstrumentFetchPayload{}
	payloadJSON := []byte{}
	err = fdb.WithTx(ctx, s.sql, func(tx *sql.Tx) error {
		ticket, err = s.tickets.Consume(ctx, tx, req.TicketID)
		if err != nil {
			return mapTicketError(err)
		}
		providerSymbol = ticket.ProviderSymbol
		inst = repository.InstrumentRecord{
			ID: instID, Code: code, Name: ticket.Name,
			Market: market, InstrumentType: instrumentType,
			AssetClass: "equity", Region: "domestic", Currency: defaultCurrency(market),
			Provider: "akshare", ProviderSymbol: providerSymbol, AdjustPolicy: adjust,
			ExpenseRatioStatus: "unavailable",
			FeeTreatment:       marketdata.FeeTreatmentForType(instrumentType),
			Status:             "pending_fetch",
		}
		payload = InstrumentFetchPayload{
			InstrumentID: instID, Market: market, InstrumentType: instrumentType,
			Code: code, ProviderSymbol: providerSymbol, AdjustPolicy: adjust,
		}
		payloadJSON, err = json.Marshal(payload)
		if err != nil {
			return err
		}
		if _, findErr := s.instRepo.FindByKey(ctx, market, instrumentType, code, adjust); findErr == nil {
			return newErr("instrument_already_exists", "instrument already imported", nil)
		} else if !errors.Is(findErr, repository.ErrInstrumentNotFound) {
			return findErr
		}
		if err := s.instRepo.Create(ctx, tx, inst); err != nil {
			return err
		}
		if err := s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, Type: repository.JobTypeInstrumentFetch, Status: repository.JobStatusQueued,
			InputHash: inputHash, PayloadJSON: string(payloadJSON),
			ProgressTotal: 1, Phase: "queued",
		}); err != nil {
			if repository.IsJobUniqueConstraint(err) {
				return s.returnFetchInProgress(ctx, inputHash)
			}
			return err
		}
		return nil
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

func mapTicketError(err error) error {
	switch {
	case errors.Is(err, repository.ErrResolutionTicketNotFound):
		return newErr("invalid_request", "resolution ticket not found", nil)
	case errors.Is(err, repository.ErrResolutionTicketExpired):
		return newErr("invalid_request", "resolution ticket expired", nil)
	case errors.Is(err, repository.ErrResolutionTicketConsumed):
		return newErr("invalid_request", "resolution ticket already consumed", nil)
	default:
		return err
	}
}

func (s *InstrumentService) returnFetchInProgress(ctx context.Context, inputHash string) error {
	job, err := s.jobs.FindInProgressByInputHash(ctx, repository.JobTypeInstrumentFetch, inputHash)
	if err != nil {
		return err
	}
	var payload InstrumentFetchPayload
	_ = json.Unmarshal([]byte(job.PayloadJSON), &payload)
	details := map[string]any{"job_id": job.ID}
	if payload.InstrumentID != "" {
		details["instrument_id"] = payload.InstrumentID
	}
	return newErr("instrument_fetch_in_progress", "instrument fetch already in progress", details)
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
		if err := s.jobs.Create(ctx, tx, repository.Job{
			ID: jobID, Type: repository.JobTypeInstrumentFetch, Status: repository.JobStatusQueued,
			InputHash: inputHash, PayloadJSON: string(payloadJSON),
			ProgressTotal: 1, Phase: "queued",
		}); err != nil {
			if repository.IsJobUniqueConstraint(err) {
				return s.returnFetchInProgress(ctx, inputHash)
			}
			return err
		}
		return nil
	})
	if err != nil {
		var ae *AppError
		if errors.As(err, &ae) {
			return InstrumentImportAsyncResult{}, ae
		}
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

// EnsureInstrumentReadyForPlan rejects instruments that are not active with available library quality.
func EnsureInstrumentReadyForPlan(inst repository.Instrument, qualityStatus string) error {
	if inst.Status != "active" {
		return newErr("instrument_not_ready", fmt.Sprintf("instrument status is %s", inst.Status), map[string]any{
			"instrument_id": inst.ID, "status": inst.Status,
		})
	}
	if qualityStatus != "available" {
		return newErr("instrument_insufficient_history", "instrument does not have enough complete years for simulation", map[string]any{
			"instrument_id": inst.ID, "quality_status": qualityStatus,
		})
	}
	return nil
}

// EnsureInstrumentRecordReadyForPlan checks an enriched instrument record.
func EnsureInstrumentRecordReadyForPlan(inst repository.InstrumentRecord) error {
	return EnsureInstrumentReadyForPlan(repository.Instrument{
		ID: inst.ID, Code: inst.Code, Name: inst.Name, Market: inst.Market,
		AssetClass: inst.AssetClass, Region: inst.Region, Currency: inst.Currency,
		Status: inst.Status, IsSystem: inst.IsSystem,
	}, inst.QualityStatus)
}

// LibraryQuality returns the current library quality status for an instrument.
func (s *InstrumentService) LibraryQuality(ctx context.Context, instrumentID string) string {
	return s.libraryQuality(ctx, instrumentID)
}
