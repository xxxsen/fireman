package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestInstrumentResolveAndImportAsync(t *testing.T) {
	provider := mockProviderServer(t)
	defer provider.Close()
	srv := testRouterWithProvider(t, provider.URL)
	defer srv.Close()
	client := srv.Client()

	resolvePayload, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund", "code": "510300",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/resolve", "application/json", bytes.NewReader(resolvePayload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	resolve := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	if resolve["ambiguous"] != false {
		t.Fatalf("expected unambiguous resolve")
	}

	importPayload, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund",
		"code": "sh510300", "provider_symbol": "sh510300",
	})
	resp, err = client.Post(srv.URL+"/api/v1/instruments/import-async", "application/json", bytes.NewReader(importPayload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import-async status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	out := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	instID := out["instrument_id"].(string)
	jobID := out["job_id"].(string)
	if out["status"] != "pending_fetch" {
		t.Fatalf("status=%v want pending_fetch", out["status"])
	}

	resp, err = client.Post(srv.URL+"/api/v1/instruments/import-async", "application/json", bytes.NewReader(importPayload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("duplicate import-async status=%d", resp.StatusCode)
	}
	dup := decodeEnvelope(t, readBody(t, resp))
	if dup["code"] != "instrument_fetch_in_progress" && dup["code"] != "instrument_already_exists" {
		t.Fatalf("duplicate code=%v", dup["code"])
	}

	resp, err = client.Get(srv.URL + "/api/v1/instruments/" + instID + "/fetch-status")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("fetch-status status=%d", resp.StatusCode)
	}
	status := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
	if status["job_id"] != jobID {
		t.Fatalf("job_id=%v want %s", status["job_id"], jobID)
	}
}

func TestInstrumentFetchWorkerActivates(t *testing.T) {
	provider := mockProviderServer(t)
	defer provider.Close()
	db := testutil.OpenTestDB(t)
	services := buildServices(db, provider.URL)
	fetchProvider := marketdata.NewProviderClient(provider.URL).FetchClient()
	fetchRunner := jobs.NewInstrumentFetchRunner(
		db,
		repository.NewInstrumentRepo(db),
		repository.NewMarketDataRepo(db),
		repository.NewAnnualReturnsRepo(db),
		fetchProvider,
	)
	worker := jobs.NewWorker(
		db, repository.NewJobRepo(db), repository.NewSimulationRepo(db),
		jobs.NewSimulationRunner(db, repository.NewSimulationRepo(db)),
		nil, fetchRunner, services.EventHub, nil, nil,
	)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go worker.Start(ctx, 1)

	srv := httptest.NewServer(NewRouter(Deps{DB: db, Services: services}))
	defer srv.Close()
	client := srv.Client()

	payload, _ := json.Marshal(map[string]any{
		"market": "CN", "instrument_type": "cn_exchange_fund",
		"code": "sh510300", "provider_symbol": "sh510300",
	})
	resp, err := client.Post(srv.URL+"/api/v1/instruments/import-async", "application/json", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("import-async status=%d body=%s", resp.StatusCode, readBody(t, resp))
	}
	instID := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)["instrument_id"].(string)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		resp, err = client.Get(srv.URL + "/api/v1/instruments/" + instID)
		if err != nil {
			t.Fatal(err)
		}
		inst := decodeEnvelope(t, readBody(t, resp))["data"].(map[string]any)
		if inst["status"] == "active" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	t.Fatal("instrument did not become active after worker fetch")
}
