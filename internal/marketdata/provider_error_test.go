package marketdata

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"
)

// Regression guard for the removed defaultHTTP=30s trap: a client built via the
// plain constructor (without FetchClient) must apply the long fetch timeout, so
// fetches are never silently truncated at 30s.
func TestFetchNotCappedByShortDefault(t *testing.T) {
	c := NewProviderClient("http://example.invalid")
	want := fetchTimeout()
	if want <= 30*time.Second {
		t.Fatalf("fetch timeout should be much larger than the old 30s default, got %v", want)
	}
	if c.fetchHTTP.Timeout != want {
		t.Fatalf("base client fetch timeout = %v, want %v", c.fetchHTTP.Timeout, want)
	}
	if c.FetchClient().fetchHTTP.Timeout != want {
		t.Fatalf("FetchClient fetch timeout = %v, want %v", c.FetchClient().fetchHTTP.Timeout, want)
	}
	if c.resolveHTTP.Timeout != resolveTimeout() {
		t.Fatalf("resolve timeout = %v, want %v", c.resolveHTTP.Timeout, resolveTimeout())
	}
}

// Error classification must depend ONLY on the structured error_code, never on
// the free-text message. Each case uses an intentionally misleading message.
func TestClassifyProviderErrorByCodeIgnoresMessage(t *testing.T) {
	cases := []struct {
		errorCode string
		status    int
		predicate func(error) bool
		name      string
	}{
		{"market_provider_timeout", http.StatusGatewayTimeout, IsProviderTimeout, "timeout"},
		{"market_provider_unavailable", http.StatusServiceUnavailable, IsProviderUnavailable, "unavailable"},
		{"instrument_not_found", http.StatusNotFound, IsInstrumentNotFound, "not_found"},
		{"instrument_type_mismatch", http.StatusBadRequest, IsInstrumentTypeMismatch, "type_mismatch"},
		{"invalid_request", http.StatusBadRequest, IsProviderInvalidRequest, "invalid_request"},
		{"source_data_conflict", http.StatusUnprocessableEntity, IsSourceDataConflict, "source_data_conflict"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			body := []byte(`{"code":1,"error_code":"` + tc.errorCode +
				`","message":"完全不相关的文案 not_found timeout mismatch","data":null}`)
			err := classifyProviderError(tc.status, body)
			if err == nil {
				t.Fatalf("expected error for %s", tc.errorCode)
			}
			if !tc.predicate(err) {
				t.Fatalf("error_code %s did not classify correctly: %v", tc.errorCode, err)
			}
			// A different code's predicate must NOT match.
			if tc.errorCode != "market_provider_timeout" && IsProviderTimeout(err) {
				t.Fatalf("error_code %s wrongly classified as timeout", tc.errorCode)
			}
		})
	}
}

// When the sidecar omits error_code (defensive for older versions), fall back to
// HTTP status semantics.
func TestClassifyProviderErrorFallsBackToStatus(t *testing.T) {
	if !IsProviderTimeout(classifyProviderError(http.StatusGatewayTimeout, []byte(`{}`))) {
		t.Fatalf("504 without error_code should classify as timeout")
	}
	if !IsProviderUnavailable(classifyProviderError(http.StatusServiceUnavailable, []byte(`{}`))) {
		t.Fatalf("503 without error_code should classify as unavailable")
	}
	if !IsInstrumentNotFound(classifyProviderError(http.StatusNotFound, []byte(`{}`))) {
		t.Fatalf("404 without error_code should classify as not_found")
	}
}

// candidate_id emitted by the sidecar must round-trip into the Go struct (no
// silent field drop).
func TestResolveCandidateIDRoundTrip(t *testing.T) {
	raw := `{"code":0,"message":"success","data":{"ambiguous":false,"resolved":{` +
		`"code":"sh510300","provider_symbol":"sh510300","name":"沪深300ETF",` +
		`"exchange":"SH","instrument_kind":"index_etf","candidate_id":"sh510300|sh510300|index_etf|SH"}}}`
	var env ResolveResponse
	if err := json.Unmarshal([]byte(raw), &env); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if env.Data.Resolved == nil {
		t.Fatal("resolved is nil")
	}
	if env.Data.Resolved.CandidateID != "sh510300|sh510300|index_etf|SH" {
		t.Fatalf("candidate_id dropped: %q", env.Data.Resolved.CandidateID)
	}
}
