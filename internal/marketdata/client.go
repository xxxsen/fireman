package marketdata

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

const defaultTimeout = 30 * time.Second

// ProviderClient calls the AKShare sidecar.
type ProviderClient struct {
	baseURL    string
	httpClient *http.Client
}

func NewProviderClient(baseURL string) *ProviderClient {
	return &ProviderClient{
		baseURL: baseURL,
		httpClient: &http.Client{
			Timeout: defaultTimeout,
		},
	}
}

func (c *ProviderClient) Fetch(ctx context.Context, req FetchRequest) (*FetchData, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, err
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/v1/instruments/fetch", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("market provider request: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 100<<20))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode == http.StatusGatewayTimeout {
		return nil, fmt.Errorf("market provider timeout")
	}
	if resp.StatusCode >= 400 {
		return nil, fmt.Errorf("market provider http %d: %s", resp.StatusCode, string(raw))
	}

	var envelope FetchResponse
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return nil, err
	}
	if envelope.Code != 0 {
		return nil, fmt.Errorf("market provider error: %s", envelope.Message)
	}
	return &envelope.Data, nil
}
