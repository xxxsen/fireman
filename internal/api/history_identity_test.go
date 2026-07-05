package api

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/fireman/fireman/internal/service"
)

// The history sync payload must carry the directory identity
// (region_code/exchange/symbol/instrument_kind) read from market_assets, and
// a CN on-exchange asset without any exchange identity must fail with a
// definite error instead of creating a task that would guess the exchange.

func TestSyncHistoryPayloadCarriesDirectoryIdentity(t *testing.T) {
	st := newInternalStack(t)
	ctx := context.Background()

	seed := cnETFAssetSeed()
	seed.Points = nil
	seedMarketAssetWithHistory(t, st.db, seed)

	created, err := st.assets.SyncHistory(ctx, service.HistorySyncRequest{
		AssetKey: seed.AssetKey, Mode: "default_refresh",
	})
	if err != nil {
		t.Fatal(err)
	}

	var payloadJSON string
	if err := st.db.QueryRow(
		`SELECT payload_json FROM worker_tasks WHERE id=?`, created.Task.ID,
	).Scan(&payloadJSON); err != nil {
		t.Fatal(err)
	}
	var payload service.AssetHistorySyncPayload
	if err := json.Unmarshal([]byte(payloadJSON), &payload); err != nil {
		t.Fatal(err)
	}
	if payload.RegionCode != "sh" || payload.Symbol != "510300" {
		t.Fatalf("payload identity = (%q, %q), want (sh, 510300)",
			payload.RegionCode, payload.Symbol)
	}
	if payload.InstrumentKind != "etf" {
		t.Fatalf("payload instrument_kind = %q, want etf", payload.InstrumentKind)
	}
	// Exchange mirrors the directory row (empty here because the seed row has
	// no exchange; region_code alone already satisfies the identity gate).
	if payload.Exchange != "" {
		t.Fatalf("payload exchange = %q, want empty (directory value)", payload.Exchange)
	}
}

func TestSyncHistoryRejectsMissingDirectoryIdentity(t *testing.T) {
	st := newInternalStack(t)
	ctx := context.Background()

	seed := cnETFAssetSeed()
	seed.AssetKey = "cn:cn_exchange_fund::159915"
	seed.RegionCode = ""
	seed.Symbol = "159915"
	seed.Points = nil
	seedMarketAssetWithHistory(t, st.db, seed)

	_, err := st.assets.SyncHistory(ctx, service.HistorySyncRequest{
		AssetKey: seed.AssetKey, Mode: "default_refresh",
	})
	if err == nil {
		t.Fatal("expected asset_identity_incomplete error, got nil")
	}
	var appErr *service.AppError
	if !errors.As(err, &appErr) || appErr.Code != "asset_identity_incomplete" {
		t.Fatalf("error = %v, want AppError{asset_identity_incomplete}", err)
	}
	if n := countRows(t, st.db,
		`SELECT COUNT(*) FROM worker_tasks WHERE type='asset_history_sync'`); n != 0 {
		t.Fatalf("history task was created despite incomplete identity: %d", n)
	}
}
