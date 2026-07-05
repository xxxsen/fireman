package service

import (
	"testing"

	"github.com/fireman/fireman/internal/repository"
)

func TestNormalizeWizardRegionTargetsFillsCash(t *testing.T) {
	in := []repository.RegionTarget{
		{AssetClass: "equity", Region: "domestic", WeightWithinClass: 1.0},
		{AssetClass: "equity", Region: "foreign", WeightWithinClass: 0.0},
		{AssetClass: "bond", Region: "domestic", WeightWithinClass: 1.0},
		{AssetClass: "bond", Region: "foreign", WeightWithinClass: 0.0},
	}
	out := normalizeWizardRegionTargets(in)
	if len(out) != 6 {
		t.Fatalf("len=%d want 6", len(out))
	}
	var cashDomestic *repository.RegionTarget
	for i := range out {
		if out[i].AssetClass == "cash" && out[i].Region == "domestic" {
			cashDomestic = &out[i]
		}
	}
	if cashDomestic == nil || cashDomestic.WeightWithinClass != 1.0 {
		t.Fatalf("cash domestic=%+v", cashDomestic)
	}
	if err := validateRegionTargets(out); err != nil {
		t.Fatalf("validate: %v", err)
	}
}

func TestValidateWizardRequestAcceptsPartialRegionTargets(t *testing.T) {
	req := PlanWizardRequest{
		Name:               "p",
		ValuationDate:      "2026-06-14",
		SelectedScenarioID: "scn_a",
		Holdings:           []WizardHoldingItem{{AssetKey: "CN|test|sh|X001"}},
		RegionTargets: []repository.RegionTarget{
			{AssetClass: "equity", Region: "domestic", WeightWithinClass: 1.0},
			{AssetClass: "equity", Region: "foreign", WeightWithinClass: 0.0},
			{AssetClass: "bond", Region: "domestic", WeightWithinClass: 1.0},
			{AssetClass: "bond", Region: "foreign", WeightWithinClass: 0.0},
		},
	}
	if err := validateWizardRequest(req); err != nil {
		t.Fatalf("expected validation to pass after cash normalization: %v", err)
	}
}
