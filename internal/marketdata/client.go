package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strconv"
	"time"
)

const defaultTimeout = 30 * time.Second

func resolveTimeout() time.Duration {
	return envDuration("MARKET_PROVIDER_RESOLVE_TIMEOUT", 20*time.Second)
}

func fetchTimeout() time.Duration {
	return envDuration("MARKET_PROVIDER_FETCH_TIMEOUT", 300*time.Second)
}

func envDuration(key string, fallback time.Duration) time.Duration {
	raw := os.Getenv(key)
	if raw == "" {
		return fallback
	}
	secs, err := strconv.Atoi(raw)
	if err != nil || secs <= 0 {
		return fallback
	}
	return time.Duration(secs) * time.Second
}

// ProviderClient calls the AKShare sidecar.
type ProviderClient struct {
	baseURL     string
	resolveHTTP *http.Client
	fetchHTTP   *http.Client
	defaultHTTP *http.Client
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
		defaultHTTP: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

// FetchClient returns the long-timeout client used by Worker fetches.
func (c *ProviderClient) FetchClient() *ProviderClient {
	return &ProviderClient{
		baseURL:     c.baseURL,
		resolveHTTP: c.resolveHTTP,
		fetchHTTP:   c.fetchHTTP,
		defaultHTTP: c.fetchHTTP,
	}
}

func (c *ProviderClient) Resolve(ctx context.Context, req ResolveRequest) (*ResolveData, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/instruments/resolve", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.resolveHTTP.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("market provider resolve request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("market provider resolve http %d: %s", resp.StatusCode, string(raw))
	}

	var envelope ResolveResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("market provider resolve error: %s", envelope.Message)
	}
	return &envelope.Data, nil
}

func (c *ProviderClient) Fetch(ctx context.Context, req FetchRequest) (*FetchData, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	slog.InfoContext(ctx, "market provider fetch start",
		"market", req.Market,
		"instrument_type", req.InstrumentType,
		"source_code", req.SourceCode,
		"start_date", req.StartDate,
		"end_date", req.EndDate,
	)
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/instruments/fetch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	client := c.defaultHTTP
	if client == nil {
		client = c.fetchHTTP
	}
	resp, err := client.Do(httpReq)
	if err != nil {
		slog.ErrorContext(ctx, "market provider fetch request failed",
			"source_code", req.SourceCode,
			"instrument_type", req.InstrumentType,
			"error", err,
		)
		return nil, fmt.Errorf("market provider request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusGatewayTimeout {
		slog.ErrorContext(ctx, "market provider fetch timeout",
			"source_code", req.SourceCode,
			"instrument_type", req.InstrumentType,
		)
		return nil, fmt.Errorf("market provider timeout")
	}
	if resp.StatusCode >= 400 {
		slog.ErrorContext(ctx, "market provider fetch http error",
			"source_code", req.SourceCode,
			"instrument_type", req.InstrumentType,
			"status", resp.StatusCode,
			"body", string(raw),
		)
		return nil, fmt.Errorf("market provider http %d: %s", resp.StatusCode, string(raw))
	}

	var envelope FetchResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		slog.ErrorContext(ctx, "market provider fetch rejected",
			"source_code", req.SourceCode,
			"instrument_type", req.InstrumentType,
			"message", envelope.Message,
		)
		return nil, fmt.Errorf("market provider error: %s", envelope.Message)
	}
	slog.InfoContext(ctx, "market provider fetch ok",
		"source_code", req.SourceCode,
		"instrument_type", req.InstrumentType,
		"points", len(envelope.Data.Points),
		"source_name", envelope.Data.SourceName,
		"source_quality", envelope.Data.SourceQuality,
	)
	return &envelope.Data, nil
}
