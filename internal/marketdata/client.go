package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

var (
	errProviderHTTP           = errors.New("market provider http error")
	errProviderTimeout        = errors.New("market provider timeout")
	errProviderRejected       = errors.New("market provider rejected")
	errProviderUnavailable    = errors.New("market provider unavailable")
	errInstrumentNotFound     = errors.New("instrument_not_found")
	errInstrumentTypeMismatch = errors.New("instrument_type_mismatch")
	errProviderInvalidRequest = errors.New("market provider invalid request")
	errSourceDataConflict     = errors.New("source_data_conflict")
)

// classifyProviderError converts a sidecar error envelope body into a typed
// sentinel error keyed solely by error_code. Falls back to HTTP status when the
// body carries no structured error_code (defensive against older sidecars).
func classifyProviderError(status int, body []byte) error {
	var env errorEnvelope
	_ = json.Unmarshal(body, &env)
	message := env.Message
	if message == "" {
		message = string(body)
	}
	code := env.ErrorCode
	if code == "" {
		switch status {
		case http.StatusGatewayTimeout:
			code = "market_provider_timeout"
		case http.StatusServiceUnavailable:
			code = "market_provider_unavailable"
		case http.StatusNotFound:
			code = "instrument_not_found"
		}
	}
	switch code {
	case "market_provider_timeout":
		return fmt.Errorf("market provider error (%s): %w", message, errProviderTimeout)
	case "market_provider_unavailable":
		return fmt.Errorf("market provider error (%s): %w", message, errProviderUnavailable)
	case "instrument_not_found":
		return fmt.Errorf("market provider error (%s): %w", message, errInstrumentNotFound)
	case "instrument_type_mismatch":
		return fmt.Errorf("market provider error (%s): %w", message, errInstrumentTypeMismatch)
	case "invalid_request":
		return fmt.Errorf("market provider error (%s): %w", message, errProviderInvalidRequest)
	case "source_data_conflict":
		return fmt.Errorf("market provider error (%s): %w", message, errSourceDataConflict)
	default:
		return fmt.Errorf("market provider http %d (%s): %w", status, message, errProviderHTTP)
	}
}

// Typed-error predicates used by the service layer to map sidecar failures to
// AppErrors without inspecting message text.

// IsProviderUnavailable reports a non-retryable upstream unavailability.
func IsProviderUnavailable(err error) bool { return errors.Is(err, errProviderUnavailable) }

// IsInstrumentNotFound reports the sidecar confirmed the code does not exist.
func IsInstrumentNotFound(err error) bool { return errors.Is(err, errInstrumentNotFound) }

// IsInstrumentTypeMismatch reports the code belongs to a different instrument type.
func IsInstrumentTypeMismatch(err error) bool { return errors.Is(err, errInstrumentTypeMismatch) }

// IsProviderInvalidRequest reports the sidecar rejected the request as invalid.
func IsProviderInvalidRequest(err error) bool { return errors.Is(err, errProviderInvalidRequest) }

// IsSourceDataConflict reports the fetched data identity conflicted with the request.
func IsSourceDataConflict(err error) bool { return errors.Is(err, errSourceDataConflict) }

func resolveTimeout() time.Duration {
	return envDuration("MARKET_PROVIDER_RESOLVE_TIMEOUT", 90*time.Second)
}

func fetchTimeout() time.Duration {
	return envDuration("MARKET_PROVIDER_FETCH_TIMEOUT", 300*time.Second)
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	if d, err := time.ParseDuration(raw); err == nil && d > 0 {
		return d
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return fallback
	}
	return time.Duration(secs) * time.Second
}

// ProviderClient calls the AKShare sidecar. Each operation applies its own
// timeout (resolve vs fetch); there is no shared default that can silently
// truncate a long fetch.
type ProviderClient struct {
	baseURL     string
	resolveHTTP *http.Client
	fetchHTTP   *http.Client
}

func NewProviderClient(baseURL string) *ProviderClient {
	return &ProviderClient{
		baseURL: baseURL,
		resolveHTTP: &http.Client{
			Timeout: resolveTimeout(),
		},
		fetchHTTP: &http.Client{
			Timeout: fetchTimeout(),
		},
	}
}

// FetchClient is retained for backward compatibility. The client now always
// applies the correct per-operation timeout, so fetches are never capped by a
// short default regardless of how the client was constructed.
func (c *ProviderClient) FetchClient() *ProviderClient { return c }

func (c *ProviderClient) Resolve(ctx context.Context, req ResolveRequest) (*ResolveData, error) {
	start := time.Now()
	deadline := resolveTimeout()
	resolveCtx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal resolve request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		resolveCtx, http.MethodPost, c.baseURL+"/v1/instruments/resolve", bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("build resolve request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.resolveHTTP.Do(httpReq)
	if err != nil {
		elapsedMs := time.Since(start).Milliseconds()
		remainingMs := max(int64(0), deadline.Milliseconds()-elapsedMs)
		if errors.Is(err, context.DeadlineExceeded) || resolveCtx.Err() != nil {
			slog.Warn("market provider resolve timeout",
				"operation", "resolve",
				"symbol", req.Code,
				"elapsed_ms", elapsedMs,
				"remaining_ms", remainingMs,
				"layer", "go",
			)
		}
		return nil, fmt.Errorf("market provider resolve request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, fmt.Errorf("read resolve response: %w", err)
	}
	if resp.StatusCode >= 400 {
		return nil, classifyProviderError(resp.StatusCode, raw)
	}

	var envelope ResolveResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode resolve response: %w", err)
	}
	if envelope.Code != 0 {
		return nil, classifyProviderError(http.StatusServiceUnavailable, raw)
	}
	return &envelope.Data, nil
}

func (c *ProviderClient) fetchHTTPRaw(
	ctx context.Context,
	req FetchRequest,
) ([]byte, error) {
	start := time.Now()
	deadlineMs := fetchTimeout().Milliseconds()
	if d, ok := ctx.Deadline(); ok {
		if until := time.Until(d); until > 0 {
			deadlineMs = until.Milliseconds()
		} else {
			deadlineMs = 0
		}
	}
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("marshal fetch request: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(
		ctx, http.MethodPost, c.baseURL+"/v1/instruments/fetch", bytes.NewReader(body),
	)
	if err != nil {
		return nil, fmt.Errorf("build fetch request: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.fetchHTTP.Do(httpReq)
	if err != nil {
		elapsedMs := time.Since(start).Milliseconds()
		remainingMs := max(int64(0), deadlineMs-elapsedMs)
		if errors.Is(err, context.DeadlineExceeded) || ctx.Err() != nil {
			slog.WarnContext(
				ctx, "market provider fetch timeout",
				"operation", "fetch",
				"symbol", req.SourceCode,
				"elapsed_ms", elapsedMs,
				"remaining_ms", remainingMs,
				"layer", "go",
			)
		}
		slog.ErrorContext(
			ctx, "market provider fetch request failed",
			"source_code", req.SourceCode,
			"instrument_type", req.InstrumentType,
			"error", err,
		)
		return nil, fmt.Errorf("market provider request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return nil, fmt.Errorf("read fetch response: %w", err)
	}
	if resp.StatusCode == http.StatusGatewayTimeout {
		elapsedMs := time.Since(start).Milliseconds()
		remainingMs := max(int64(0), deadlineMs-elapsedMs)
		slog.WarnContext(
			ctx, "market provider fetch timeout",
			"operation", "fetch",
			"symbol", req.SourceCode,
			"elapsed_ms", elapsedMs,
			"remaining_ms", remainingMs,
			"layer", "go",
		)
		return nil, fmt.Errorf("market provider timeout: %w", errProviderTimeout)
	}
	if resp.StatusCode >= 400 {
		slog.ErrorContext(
			ctx, "market provider fetch http error",
			"source_code", req.SourceCode,
			"instrument_type", req.InstrumentType,
			"status", resp.StatusCode,
			"body", string(raw),
		)
		return nil, classifyProviderError(resp.StatusCode, raw)
	}
	return raw, nil
}

func (c *ProviderClient) Fetch(ctx context.Context, req FetchRequest) (*FetchData, error) {
	slog.InfoContext(
		ctx, "market provider fetch start",
		"market", req.Market,
		"instrument_type", req.InstrumentType,
		"source_code", req.SourceCode,
		"start_date", req.StartDate,
		"end_date", req.EndDate,
	)
	raw, err := c.fetchHTTPRaw(ctx, req)
	if err != nil {
		return nil, err
	}

	var envelope FetchResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, fmt.Errorf("decode fetch response: %w", err)
	}
	if envelope.Code != 0 {
		slog.ErrorContext(
			ctx, "market provider fetch rejected",
			"source_code", req.SourceCode,
			"instrument_type", req.InstrumentType,
			"message", envelope.Message,
		)
		return nil, fmt.Errorf("market provider error: %s: %w", envelope.Message, errProviderRejected)
	}
	slog.InfoContext(
		ctx, "market provider fetch ok",
		"source_code", req.SourceCode,
		"instrument_type", req.InstrumentType,
		"points", len(envelope.Data.Points),
		"source_name", envelope.Data.SourceName,
		"source_quality", envelope.Data.SourceQuality,
	)
	return &envelope.Data, nil
}

// IsProviderTimeout reports whether err indicates a sidecar upstream timeout.
func IsProviderTimeout(err error) bool {
	return errors.Is(err, errProviderTimeout)
}
