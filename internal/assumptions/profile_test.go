package assumptions

import (
	"math"
	"testing"
)

func TestSystemDefaultProfileValidates(t *testing.T) {
	p := SystemDefaultProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("system default profile invalid: %v", err)
	}
	if p.Ref() != "system_cma_v3@1" {
		t.Fatalf("unexpected ref %q", p.Ref())
	}
}

func TestContentHashStableAcrossOrdering(t *testing.T) {
	a := SystemDefaultProfile()
	b := SystemDefaultProfile()
	// Reverse the slice orderings; canonicalisation must yield the same hash.
	for i, j := 0, len(b.ReturnPriors)-1; i < j; i, j = i+1, j-1 {
		b.ReturnPriors[i], b.ReturnPriors[j] = b.ReturnPriors[j], b.ReturnPriors[i]
	}
	for i := range b.CorrelationPriors {
		b.CorrelationPriors[i].FactorA, b.CorrelationPriors[i].FactorB = b.CorrelationPriors[i].FactorB, b.CorrelationPriors[i].FactorA
	}
	ha, err := a.ContentHash()
	if err != nil {
		t.Fatal(err)
	}
	hb, err := b.ContentHash()
	if err != nil {
		t.Fatal(err)
	}
	if ha != hb {
		t.Fatalf("content hash not order-independent: %s != %s", ha, hb)
	}
}

func TestContentHashChangesWithValues(t *testing.T) {
	a := SystemDefaultProfile()
	b := SystemDefaultProfile()
	b.ReturnPriors[0].AnnualGeometricReturn += 0.001
	ha, _ := a.ContentHash()
	hb, _ := b.ContentHash()
	if ha == hb {
		t.Fatal("content hash unchanged after editing a prior")
	}
}

func TestProfileValidateFailures(t *testing.T) {
	cases := map[string]func(p *Profile){
		"missing scenario": func(p *Profile) { delete(p.Scenarios, ScenarioOptimistic) },
		"bad vol mult": func(p *Profile) {
			s := p.Scenarios[ScenarioBaseline]
			s.VolatilityMultiplier = 0
			p.Scenarios[ScenarioBaseline] = s
		},
		"missing audit": func(p *Profile) { p.ReturnPriors[0].SourceURL = "" },
		"bad vol bounds": func(p *Profile) {
			p.ReturnPriors[0].AnnualVolatilityCeiling = 0.01
			p.ReturnPriors[0].AnnualVolatilityFloor = 0.2
		},
		"return below -100%":      func(p *Profile) { p.ReturnPriors[0].AnnualGeometricReturn = -1.5 },
		"low student t":           func(p *Profile) { p.StudentTDf = 2 },
		"return floor not loss":   func(p *Profile) { p.ReturnFloor = 0 },
		"return floor below -1":   func(p *Profile) { p.ReturnFloor = -1.2 },
		"return ceil not gain":    func(p *Profile) { p.ReturnCeil = 0 },
		"return floor above ceil": func(p *Profile) { p.ReturnFloor, p.ReturnCeil = -0.1, -0.2 },
		"bad rho":                 func(p *Profile) { p.CorrelationPriors[0].Rho = 1.5 },
		"zero prior years":        func(p *Profile) { p.PriorStrengthYears = 0 },
		"duplicate prior": func(p *Profile) {
			p.ReturnPriors = append(p.ReturnPriors, p.ReturnPriors[0])
		},
		// Non-finite numerics must be rejected even when range checks pass.
		"inf vol multiplier": func(p *Profile) {
			s := p.Scenarios[ScenarioBaseline]
			s.VolatilityMultiplier = math.Inf(1)
			p.Scenarios[ScenarioBaseline] = s
		},
		"nan return shift": func(p *Profile) {
			s := p.Scenarios[ScenarioOptimistic]
			s.ReturnShiftLog = math.NaN()
			p.Scenarios[ScenarioOptimistic] = s
		},
		"inf vol ceiling": func(p *Profile) { p.ReturnPriors[0].AnnualVolatilityCeiling = math.Inf(1) },
		"duplicate fx prior": func(p *Profile) {
			p.FXPriors = append(p.FXPriors, p.FXPriors[0])
		},
		// Provenance must be an https URL and ISO dates.
		"non-https source":   func(p *Profile) { p.ReturnPriors[0].SourceURL = "http://x.test" },
		"bad published date": func(p *Profile) { p.ReturnPriors[0].PublishedAt = "2026/06/20" },
		"empty reviewed at":  func(p *Profile) { p.FXPriors[0].ReviewedAt = "" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			p := SystemDefaultProfile()
			mutate(&p)
			if err := p.Validate(); err == nil {
				t.Fatalf("expected validation error for %s", name)
			}
		})
	}
}
