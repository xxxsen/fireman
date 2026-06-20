package assumptions

import "testing"

func TestSystemDefaultProfileValidates(t *testing.T) {
	p := SystemDefaultProfile()
	if err := p.Validate(); err != nil {
		t.Fatalf("system default profile invalid: %v", err)
	}
	if p.Ref() != "system_cma_v1@1" {
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
		"return below -100%": func(p *Profile) { p.ReturnPriors[0].AnnualGeometricReturn = -1.5 },
		"low student t":      func(p *Profile) { p.StudentTDf = 2 },
		"bad rho":            func(p *Profile) { p.CorrelationPriors[0].Rho = 1.5 },
		"zero prior years":   func(p *Profile) { p.PriorStrengthYears = 0 },
		"duplicate prior": func(p *Profile) {
			p.ReturnPriors = append(p.ReturnPriors, p.ReturnPriors[0])
		},
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
