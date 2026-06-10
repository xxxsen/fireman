package service

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/testutil"
)

func TestIsImportableCandidate(t *testing.T) {
	if !IsImportableCandidate("cn_exchange_fund", "etf") {
		t.Fatal("etf should be importable for cn_exchange_fund")
	}
	if IsImportableCandidate("cn_exchange_fund", "stock") {
		t.Fatal("stock must not be importable for cn_exchange_fund")
	}
	if !IsImportableCandidate("cn_exchange_stock", "stock") {
		t.Fatal("stock should be importable for cn_exchange_stock")
	}
}

func TestEvaluateInstrumentForPlan_SystemCash(t *testing.T) {
	db := testutil.OpenTestDB(t)
	marketRepo := repository.NewMarketDataRepo(db)
	inst, err := repository.NewInstrumentRepo(db).GetByID(context.Background(), repository.SystemCashInstrumentID)
	if err != nil {
		t.Fatal(err)
	}
	eval, err := EvaluateInstrumentForPlan(context.Background(), inst, marketRepo, "2020-01-01")
	if err != nil {
		t.Fatalf("system cash should be available: %v", err)
	}
	if !eval.Available || eval.QualityStatus != "available" {
		t.Fatalf("eval=%+v", eval)
	}
}

func TestEvaluateInstrumentForPlan_RejectsOtherSystemInstrument(t *testing.T) {
	db := testutil.OpenTestDB(t)
	marketRepo := repository.NewMarketDataRepo(db)
	instRepo := repository.NewInstrumentRepo(db)
	inst, err := instRepo.GetByID(context.Background(), "system_fx_usdcny")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := EvaluateInstrumentForPlan(context.Background(), inst, marketRepo, "2026-06-09"); err == nil {
		t.Fatal("expected system FX instrument to be rejected")
	}
}
