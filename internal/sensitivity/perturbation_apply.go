package sensitivity

import (
	"math"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

func applyInitialAssetsPerturbation(cp *simulation.InputSnapshot, delta float64) {
	scale := 1 + delta
	cp.Parameters.TotalAssetsMinor = int64(math.Round(float64(cp.Parameters.TotalAssetsMinor) * scale))
	for i := range cp.Assets {
		cp.Assets[i].InitialMinor = int64(math.Round(float64(cp.Assets[i].InitialMinor) * scale))
	}
}

func applyAnnualSpendingPerturbation(cp *simulation.InputSnapshot, delta float64) {
	scale := 1 + delta
	cp.Parameters.AnnualSpendingMinor = int64(math.Round(float64(cp.Parameters.AnnualSpendingMinor) * scale))
}

func applyFixedInflationPerturbation(cp *simulation.InputSnapshot, delta float64) {
	cp.Parameters.FixedInflationRate += delta
	if cp.Parameters.FixedInflationRate < -0.02 {
		cp.Parameters.FixedInflationRate = -0.02
	}
	if cp.Parameters.FixedInflationRate > 0.20 {
		cp.Parameters.FixedInflationRate = 0.20
	}
}

func applyNonCashReturnPerturbation(cp *simulation.InputSnapshot, delta float64) {
	for i := range cp.Assets {
		if cp.Assets[i].IsCash || cp.Assets[i].AssetClass == domain.AssetClassCash {
			continue
		}
		cp.Assets[i].ModeledAnnualReturn += delta
		if cp.Assets[i].ModeledAnnualReturn < simulation.ReturnFloor {
			cp.Assets[i].ModeledAnnualReturn = simulation.ReturnFloor
		}
	}
}

func applyRetirementAgePerturbation(cp *simulation.InputSnapshot, delta float64) {
	p := &cp.Parameters
	age := p.RetirementAge + int(delta)
	if age < p.CurrentAge {
		age = p.CurrentAge
	}
	if age >= p.EndAge {
		age = p.EndAge - 1
	}
	p.RetirementAge = age
}

func applyEndAgePerturbation(cp *simulation.InputSnapshot, delta float64) {
	p := &cp.Parameters
	age := p.EndAge + int(delta)
	if age <= p.RetirementAge {
		age = p.RetirementAge + 1
	}
	if age > 120 {
		age = 120
	}
	p.EndAge = age
}
