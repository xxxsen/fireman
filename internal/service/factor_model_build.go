package service

import (
	"errors"
	"fmt"

	"github.com/fireman/fireman/internal/assumptions"
	"github.com/fireman/fireman/internal/simulation"
)

var (
	// errFactorCorrelationMissing is returned when a cross-type factor pair has no
	// correlation prior in the resolved profile. The forward engine must block
	// rather than silently assume ρ=0 (td/063 R4).
	errFactorCorrelationMissing = errors.New("no correlation prior for factor pair")
	// errFactorModelNotPSD is returned when the frozen covariance cannot be
	// Cholesky-decomposed, so the joint sampler cannot be built (td/063 N3).
	errFactorModelNotPSD = errors.New("factor model covariance is not positive semi-definite")
)

// factorBuild collects the per-factor inputs while walking the plan's assets.
type factorBuild struct {
	names    []string
	typeKeys []string
	mu       []float64
	sigma    []float64
	months   []map[string]float64
}

// buildFrozenFactorModel assembles the joint risk model frozen into a run's input
// snapshot (td/061 §3.5). Factors are per non-cash asset (so each asset keeps its
// own forward drift and volatility) plus one shared FX factor per foreign
// currency. Cross-type correlations blend the frozen monthly history toward the
// profile prior (shrinkage), falling back to the prior when fewer than 24 common
// months exist; two holdings of the same (asset_class, region) are forced to ρ=1
// so identical exposures get no fake diversification (td/061 §3.5.1/§3.5.2).
//
// It returns (nil, nil, nil) when there is no risk factor (an all-cash plan), in
// which case the caller keeps the legacy independent path. It returns an error
// when a required correlation prior is missing or the covariance is not PSD, so
// the forward engine blocks instead of silently degrading (td/063 R4/N3).
func buildFrozenFactorModel(
	assets []simulation.SnapshotAsset, baseCurrency string, profile assumptions.Profile,
) (*simulation.FactorModel, []simulation.FactorRef, error) {
	fb := factorBuild{}
	refs := make([]simulation.FactorRef, len(assets))
	fxIndexByCurrency := map[string]int{}

	for i, a := range assets {
		refs[i] = simulation.FactorRef{AssetFactorIndex: -1, FXFactorIndex: -1}
		if a.IsCash {
			continue
		}
		params := simulation.ParamsFromAnnual(a.ModeledAnnualReturn, a.AnnualVolatility)
		refs[i].AssetFactorIndex = len(fb.names)
		fb.add(
			assumptions.AssetFactorKey(a.AssetClass, a.Region)+"#"+a.HoldingID,
			assumptions.AssetFactorKey(a.AssetClass, a.Region),
			params.MonthlyMu, params.MonthlySigma, a.Months,
		)

		if a.FXSnapshotID != "" && a.Currency != baseCurrency {
			fxIdx, ok := fxIndexByCurrency[a.Currency]
			if !ok {
				fxIdx = len(fb.names)
				fxParams := simulation.ParamsFromAnnual(a.FXModeledReturn, a.FXAnnualVolatility)
				fxKey := assumptions.FXFactorKey(a.Currency, baseCurrency)
				fb.add(fxKey, fxKey, fxParams.MonthlyMu, fxParams.MonthlySigma, a.FXMonths)
				fxIndexByCurrency[a.Currency] = fxIdx
			}
			refs[i].FXFactorIndex = fxIdx
		}
	}
	if len(fb.names) == 0 {
		return nil, nil, nil
	}

	rRaw, pairMonths, lambda, priorOnly, err := fb.correlations(profile)
	if err != nil {
		return nil, nil, err
	}
	model, ok := simulation.AssembleFactorModelDetailed(
		fb.names, fb.mu, fb.sigma, rRaw, pairMonths, lambda, priorOnly,
	)
	if !ok {
		return nil, nil, errFactorModelNotPSD
	}
	return &model, refs, nil
}

func (fb *factorBuild) add(name, typeKey string, mu, sigma float64, months map[string]float64) {
	fb.names = append(fb.names, name)
	fb.typeKeys = append(fb.typeKeys, typeKey)
	fb.mu = append(fb.mu, mu)
	fb.sigma = append(fb.sigma, sigma)
	fb.months = append(fb.months, months)
}

// correlations builds the raw correlation matrix and per-pair audit. Same-type
// pairs are forced to ρ=1; cross-type pairs use the shrunk historical estimate
// when at least MinCommonMonths overlap, otherwise the profile prior.
func (fb *factorBuild) correlations(
	profile assumptions.Profile,
) ([][]float64, map[string]int, map[string]float64, []string, error) {
	priorLookup := correlationPriorLookup(profile)
	strength := profile.CorrelationStrengthMonths
	n := len(fb.names)
	r := make([][]float64, n)
	for i := range r {
		r[i] = make([]float64, n)
		r[i][i] = 1
	}
	pairMonths := map[string]int{}
	lambda := map[string]float64{}
	var priorOnly []string
	for i := 0; i < n; i++ {
		for j := i + 1; j < n; j++ {
			pk := simulation.PairKey(fb.names[i], fb.names[j])
			rho, err := fb.pairCorrelation(i, j, priorLookup, strength, pk, pairMonths, lambda, &priorOnly)
			if err != nil {
				return nil, nil, nil, nil, err
			}
			r[i][j] = rho
			r[j][i] = rho
		}
	}
	return r, pairMonths, lambda, priorOnly, nil
}

func (fb *factorBuild) pairCorrelation(
	i, j int, priorLookup func(a, b string) (float64, bool), strength int,
	pk string, pairMonths map[string]int, lambda map[string]float64, priorOnly *[]string,
) (float64, error) {
	if fb.typeKeys[i] == fb.typeKeys[j] {
		// Identical exposures are the same risk factor: perfectly correlated.
		lambda[pk] = 0
		return 1, nil
	}
	prior, hasPrior := priorLookup(fb.typeKeys[i], fb.typeKeys[j])
	if !hasPrior {
		// A missing cross-type correlation prior must block the run, never silently
		// become ρ=0 (td/063 R4).
		return 0, fmt.Errorf("%w: %s|%s", errFactorCorrelationMissing, fb.typeKeys[i], fb.typeKeys[j])
	}
	rhoHist, m, histOK := simulation.PairwiseCorrelation(
		simulation.FactorSpec{Months: fb.months[i]},
		simulation.FactorSpec{Months: fb.months[j]},
	)
	rho, lam, isPriorOnly := simulation.ShrinkCorrelation(rhoHist, m, histOK, prior, strength)
	pairMonths[pk] = m
	lambda[pk] = lam
	if isPriorOnly {
		*priorOnly = append(*priorOnly, pk)
	}
	return rho, nil
}

func correlationPriorLookup(profile assumptions.Profile) func(a, b string) (float64, bool) {
	type pair struct{ a, b string }
	m := make(map[pair]float64, len(profile.CorrelationPriors))
	for _, c := range profile.CorrelationPriors {
		m[pair{c.FactorA, c.FactorB}] = c.Rho
		m[pair{c.FactorB, c.FactorA}] = c.Rho
	}
	return func(a, b string) (float64, bool) {
		v, ok := m[pair{a, b}]
		return v, ok
	}
}
