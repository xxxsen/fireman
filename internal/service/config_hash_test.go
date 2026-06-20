package service

import (
	"testing"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/repository"
)

// td/061 §6.1.6 / §6.2.4: the return-assumption selection is part of the plan
// config hash, so switching scenario, mode, profile or version must change the
// hash (which is what marks existing runs stale). This is a regression test for
// a bug where parametersToMap omitted the assumption fields and assumption-only
// edits left the config hash unchanged.
func TestConfigHashChangesWithAssumptionSelection(t *testing.T) {
	base := repository.PlanParameters{
		PlanID: "plan_1", CurrentAge: 35, RetirementAge: 35, EndAge: 85,
		ReturnAssumptionMode:       repository.ModeBlendedPrior,
		AssumptionSelectionMode:    repository.DefaultAssumptionSelectionMode,
		ReturnAssumptionSetID:      "system_cma_v1",
		ReturnAssumptionSetVersion: 1,
		ReturnAssumptionScenario:   "baseline",
	}

	hashOf := func(p repository.PlanParameters) string {
		h, err := domain.ComputeConfigHash(domain.ConfigHashInput{
			PlanID:     p.PlanID,
			Parameters: parametersToMap(p),
		})
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		return h
	}

	baseHash := hashOf(base)

	cases := map[string]func(p *repository.PlanParameters){
		"scenario":  func(p *repository.PlanParameters) { p.ReturnAssumptionScenario = "conservative" },
		"mode":      func(p *repository.PlanParameters) { p.ReturnAssumptionMode = repository.ModeHistoricalCAGR },
		"version":   func(p *repository.PlanParameters) { p.ReturnAssumptionSetVersion = 2 },
		"profile":   func(p *repository.PlanParameters) { p.ReturnAssumptionSetID = "user_custom_v1" },
		"selection": func(p *repository.PlanParameters) { p.AssumptionSelectionMode = "pinned_profile" },
		"custom_json": func(p *repository.PlanParameters) {
			p.ReturnAssumptionMode = repository.ModeCustom
			p.CustomReturnAssumptionsJSON = `{"i1":0.05}`
		},
	}

	for name, mutate := range cases {
		p := base
		mutate(&p)
		if got := hashOf(p); got == baseHash {
			t.Fatalf("changing %s must change config hash, but it stayed %s", name, baseHash)
		}
	}
}

// td/064 N6: student_t_df is a legacy 2.x-only field. Forward (blended_prior/
// custom) runs freeze the global profile's df, so changing the plan df must not
// change a forward run's config hash; historical_cagr replay still depends on it.
func TestConfigHashStudentTDfLegacySemantics(t *testing.T) {
	hashOf := func(p repository.PlanParameters) string {
		h, err := domain.ComputeConfigHash(domain.ConfigHashInput{
			PlanID:     p.PlanID,
			Parameters: parametersToMap(p),
		})
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		return h
	}

	forward := repository.PlanParameters{
		PlanID: "plan_1", ReturnAssumptionMode: repository.ModeBlendedPrior, StudentTDf: 7,
	}
	forwardOther := forward
	forwardOther.StudentTDf = 25
	if hashOf(forward) != hashOf(forwardOther) {
		t.Fatal("changing student_t_df must NOT change a forward config hash")
	}

	hist := repository.PlanParameters{
		PlanID: "plan_1", ReturnAssumptionMode: repository.ModeHistoricalCAGR, StudentTDf: 7,
	}
	histOther := hist
	histOther.StudentTDf = 25
	if hashOf(hist) == hashOf(histOther) {
		t.Fatal("changing student_t_df must change a historical_cagr config hash")
	}
}

// td/061 §4.1.5: an asset-level override is part of the plan config, so adding or
// editing one (for a held instrument) must change the config hash that marks
// existing runs stale.
func TestConfigHashChangesWithReturnOverride(t *testing.T) {
	holds := []repository.PlanHolding{{InstrumentID: "ins_1", Enabled: true, WeightWithinGroup: 1}}
	hashOf := func(overrides []repository.PlanReturnOverride) string {
		h, err := domain.ComputeConfigHash(domain.ConfigHashInput{
			PlanID:   "plan_1",
			Holdings: holdingsToMaps(holds, overrides),
		})
		if err != nil {
			t.Fatalf("hash: %v", err)
		}
		return h
	}

	r := 0.2
	base := hashOf(nil)
	added := hashOf([]repository.PlanReturnOverride{
		{InstrumentID: "ins_1", ForwardReturn: &r, Reason: "x", ExpiresAt: "2099-12-31"},
	})
	if base == added {
		t.Fatal("adding an override must change the config hash")
	}

	r2 := 0.3
	edited := hashOf([]repository.PlanReturnOverride{
		{InstrumentID: "ins_1", ForwardReturn: &r2, Reason: "x", ExpiresAt: "2099-12-31"},
	})
	if edited == added {
		t.Fatal("editing an override's value must change the config hash")
	}

	// An override for an instrument the plan does not hold has no simulation
	// effect, so it must not change the hash.
	unrelated := hashOf([]repository.PlanReturnOverride{
		{InstrumentID: "ins_other", ForwardReturn: &r, Reason: "x", ExpiresAt: "2099-12-31"},
	})
	if unrelated != base {
		t.Fatal("override for an unheld instrument must not change the config hash")
	}
}
