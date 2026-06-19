package repository

import (
	"context"
	"testing"

	"github.com/fireman/fireman/internal/testutil"
)

func seedInstrument(t *testing.T, repo *InstrumentRepo, id, code, name, assetClass, region string, createdAt int64) {
	t.Helper()
	if err := repo.Create(context.Background(), nil, InstrumentRecord{
		ID: id, Code: code, Name: name, Market: "CN", InstrumentType: "fund",
		AssetClass: assetClass, Region: region, Currency: "CNY",
		Provider: "akshare", ProviderSymbol: code, AdjustPolicy: "qfq",
		ExpenseRatioStatus: "unknown", FeeTreatment: "net", Status: "active",
		CreatedAt: createdAt,
	}); err != nil {
		t.Fatalf("seed %s: %v", id, err)
	}
}

func TestInstrumentRepoSearchPagination(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewInstrumentRepo(db)
	ctx := context.Background()

	// Three equity-domestic created at increasing times (newest = ins_c).
	seedInstrument(t, repo, "ins_a", "EQA", "权益甲", "equity", "domestic", 1000)
	seedInstrument(t, repo, "ins_b", "EQB", "权益乙", "equity", "domestic", 2000)
	seedInstrument(t, repo, "ins_c", "EQC", "权益丙", "equity", "domestic", 3000)
	seedInstrument(t, repo, "ins_bond", "BD1", "债券甲", "bond", "domestic", 4000)

	page1, err := repo.Search(ctx, InstrumentSearchOptions{
		AssetClass: "equity", Region: "domestic", Status: "active",
		ExcludeSystem: true, Limit: 2, Offset: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 3 {
		t.Fatalf("total = %d, want 3", page1.Total)
	}
	if len(page1.Instruments) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1.Instruments))
	}
	if page1.Instruments[0].ID != "ins_c" || page1.Instruments[1].ID != "ins_b" {
		t.Fatalf("order = %s,%s want ins_c,ins_b (newest first)",
			page1.Instruments[0].ID, page1.Instruments[1].ID)
	}

	page2, err := repo.Search(ctx, InstrumentSearchOptions{
		AssetClass: "equity", Region: "domestic", Status: "active",
		ExcludeSystem: true, Limit: 2, Offset: 2,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(page2.Instruments) != 1 || page2.Instruments[0].ID != "ins_a" {
		t.Fatalf("page2 = %+v, want [ins_a]", page2.Instruments)
	}
}

func TestInstrumentRepoSearchQueryAndExclude(t *testing.T) {
	db := testutil.OpenTestDB(t)
	repo := NewInstrumentRepo(db)
	ctx := context.Background()

	seedInstrument(t, repo, "ins_a", "EQA", "沪深300", "equity", "domestic", 1000)
	seedInstrument(t, repo, "ins_b", "EQB", "纳指100", "equity", "foreign", 2000)

	byName, err := repo.Search(ctx, InstrumentSearchOptions{Query: "沪深", ExcludeSystem: true, Limit: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(byName.Instruments) != 1 || byName.Instruments[0].ID != "ins_a" {
		t.Fatalf("query 沪深 = %+v, want [ins_a]", byName.Instruments)
	}

	excluded, err := repo.Search(ctx, InstrumentSearchOptions{
		ExcludeSystem: true, ExcludeIDs: []string{"ins_a"}, Limit: 10,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, inst := range excluded.Instruments {
		if inst.ID == "ins_a" {
			t.Fatal("ins_a should be excluded")
		}
	}
}
