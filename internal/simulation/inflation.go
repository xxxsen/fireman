package simulation

import "math"

// InflationState tracks cumulative inflation for spending.
type InflationState struct {
	Mode           string
	FixedMonthly   float64
	Mu, Phi, Sigma float64
	AnnualPi       float64
	Cumulative     float64
	lastYear       int
	rng            *RNG
	overrideAnnual *float64
}

// SetOverrideAnnual pins a fixed annual inflation rate for the current month.
func (s *InflationState) SetOverrideAnnual(annual *float64) {
	s.overrideAnnual = annual
}

// ClearOverrideAnnual removes a monthly inflation override.
func (s *InflationState) ClearOverrideAnnual() {
	s.overrideAnnual = nil
}

func NewInflationState(engineVersion, mode string, fixedAnnual, mu, phi, sigma float64, rng *RNG) InflationState {
	st := InflationState{
		Mode: mode, Mu: mu, Phi: phi, Sigma: sigma, Cumulative: 1, lastYear: -1, rng: rng,
	}
	if mode == "random_ar1" && UsesStationaryInflationInitialState(engineVersion) {
		st.AnnualPi = mu
	}
	if mode == "fixed" || mode == "fixed_real" {
		st.FixedMonthly = math.Pow(1+fixedAnnual, 1.0/12) - 1
	}
	return st
}

func (s *InflationState) MonthlyRate(month int) float64 {
	if s.overrideAnnual != nil {
		return math.Pow(1+*s.overrideAnnual, 1.0/12) - 1
	}
	year := month / 12
	switch s.Mode {
	case "random_ar1":
		if year != s.lastYear {
			eps := s.rng.NormFloat64()
			pi := s.Mu + s.Phi*(s.AnnualPi-s.Mu) + s.Sigma*eps
			if pi < -0.02 {
				pi = -0.02
			}
			if pi > 0.20 {
				pi = 0.20
			}
			s.AnnualPi = pi
			s.lastYear = year
		}
		return math.Pow(1+s.AnnualPi, 1.0/12) - 1
	default:
		return s.FixedMonthly
	}
}

func (s *InflationState) Advance(month int) {
	r := s.MonthlyRate(month)
	s.Cumulative *= (1 + r)
}
