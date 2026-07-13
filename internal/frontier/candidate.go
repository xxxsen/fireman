package frontier

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"math/big"
	"reflect"
	"sort"
	"strconv"

	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

var ErrCandidateInvalid = errors.New("frontier candidate invalid")

type Candidate struct {
	RetirementAge int
	ValueMinor    int64
}

// ValidateSourceAssets proves the frozen amount invariant. When target weights
// are required (the asset frontiers with a zero source total), building a
// one-minor candidate also validates the exact fallback allocation contract.
func ValidateSourceAssets(snapshot simulation.InputSnapshot, requireTargetWeights bool) error {
	var sum int64
	keys := make(map[string]struct{}, len(snapshot.Assets))
	for _, asset := range snapshot.Assets {
		if asset.AssetKey == "" || asset.InitialMinor < 0 || asset.InitialMinor > math.MaxInt64-sum {
			return fmt.Errorf("%w: invalid frozen asset amounts", ErrCandidateInvalid)
		}
		if _, exists := keys[asset.AssetKey]; exists {
			return fmt.Errorf("%w: duplicate frozen asset key %q", ErrCandidateInvalid, asset.AssetKey)
		}
		keys[asset.AssetKey] = struct{}{}
		sum += asset.InitialMinor
	}
	if sum != snapshot.Parameters.TotalAssetsMinor || sum < 0 {
		return fmt.Errorf("%w: frozen assets do not equal total assets", ErrCandidateInvalid)
	}
	if requireTargetWeights && sum == 0 {
		_, err := BuildCandidate(snapshot, TypeRequiredCurrentAssets, Candidate{ValueMinor: 1})
		return err
	}
	return nil
}

// ValidateConfigAssets proves that the source snapshot's enabled asset amounts
// are the same values covered by the frozen plan config hash. Disabled plan
// holdings remain hash-frozen but are not simulation slots and are not scaled.
func ValidateConfigAssets(snapshot simulation.InputSnapshot, config domain.ConfigHashInput) error {
	amounts := make(map[string]int64, len(config.Holdings))
	for _, holding := range config.Holdings {
		if enabled, exists := holding["enabled"].(bool); exists && !enabled {
			continue
		}
		key, _ := holding["asset_key"].(string)
		amount, ok := configMinor(holding["current_amount_minor"])
		if key == "" || !ok {
			return fmt.Errorf("%w: enabled config holding is invalid", ErrCandidateInvalid)
		}
		if _, duplicate := amounts[key]; duplicate {
			return fmt.Errorf("%w: duplicate enabled config asset %q", ErrCandidateInvalid, key)
		}
		amounts[key] = amount
	}
	if len(amounts) != len(snapshot.Assets) {
		return fmt.Errorf("%w: source assets differ from enabled config holdings", ErrCandidateInvalid)
	}
	for _, asset := range snapshot.Assets {
		if amount, exists := amounts[asset.AssetKey]; !exists || amount != asset.InitialMinor {
			return fmt.Errorf("%w: source asset %q differs from frozen config", ErrCandidateInvalid, asset.AssetKey)
		}
	}
	return nil
}

func BuildCandidate(base simulation.InputSnapshot, frontierType string, candidate Candidate) (simulation.InputSnapshot, error) {
	out, err := cloneSnapshot(base)
	if err != nil {
		return simulation.InputSnapshot{}, err
	}
	switch frontierType {
	case TypeRetirementAgeMaxSpending:
		out.Parameters.RetirementAge = candidate.RetirementAge
		out.Parameters.AnnualSpendingMinor = candidate.ValueMinor
	case TypeRetirementAgeMinSavings:
		out.Parameters.RetirementAge = candidate.RetirementAge
		out.Parameters.AnnualSavingsMinor = candidate.ValueMinor
	case TypeRequiredCurrentAssets:
		if err := scaleAssets(&out, candidate.ValueMinor); err != nil {
			return simulation.InputSnapshot{}, err
		}
	case TypeCoastRequiredAssets:
		out.Parameters.AnnualSavingsMinor = 0
		if err := scaleAssets(&out, candidate.ValueMinor); err != nil {
			return simulation.InputSnapshot{}, err
		}
	default:
		return simulation.InputSnapshot{}, ErrCandidateInvalid
	}
	if err := validateCandidateValues(out); err != nil {
		return simulation.InputSnapshot{}, err
	}
	if err := ValidateCandidateDiff(base, out, frontierType); err != nil {
		return simulation.InputSnapshot{}, err
	}
	return out, nil
}

func ApplyConfigCandidate(base domain.ConfigHashInput, frontierType string, candidate Candidate,
	assetAmounts map[string]int64,
) (domain.ConfigHashInput, error) {
	out := cloneConfigHashInput(base)
	switch frontierType {
	case TypeRetirementAgeMaxSpending:
		out.Parameters["retirement_age"] = candidate.RetirementAge
		out.Parameters["annual_spending_minor"] = candidate.ValueMinor
	case TypeRetirementAgeMinSavings:
		out.Parameters["retirement_age"] = candidate.RetirementAge
		out.Parameters["annual_savings_minor"] = candidate.ValueMinor
	case TypeRequiredCurrentAssets, TypeCoastRequiredAssets:
		out.Parameters["total_assets_minor"] = candidate.ValueMinor
		if frontierType == TypeCoastRequiredAssets {
			out.Parameters["annual_savings_minor"] = int64(0)
		}
		for i := range out.Holdings {
			if enabled, exists := out.Holdings[i]["enabled"].(bool); exists && !enabled {
				continue
			}
			key, _ := out.Holdings[i]["asset_key"].(string)
			amount, ok := assetAmounts[key]
			if !ok {
				return domain.ConfigHashInput{}, fmt.Errorf("%w: holding %q missing from frozen assets", ErrCandidateInvalid, key)
			}
			out.Holdings[i]["current_amount_minor"] = amount
		}
	default:
		return domain.ConfigHashInput{}, ErrCandidateInvalid
	}
	return out, nil
}

func configMinor(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case json.Number:
		parsed, err := typed.Int64()
		return parsed, err == nil
	case float64:
		if typed < math.MinInt64 || typed > math.MaxInt64 || typed != math.Trunc(typed) {
			return 0, false
		}
		return int64(typed), true
	case string:
		parsed, err := strconv.ParseInt(typed, 10, 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

// ValidateCandidateDiff makes the field whitelist executable. It restores the
// permitted fields from the source and then requires byte-for-byte structural
// equality with the source snapshot.
func ValidateCandidateDiff(base, candidate simulation.InputSnapshot, frontierType string) error {
	restored, err := cloneSnapshot(candidate)
	if err != nil {
		return err
	}
	switch frontierType {
	case TypeRetirementAgeMaxSpending:
		restored.Parameters.RetirementAge = base.Parameters.RetirementAge
		restored.Parameters.AnnualSpendingMinor = base.Parameters.AnnualSpendingMinor
	case TypeRetirementAgeMinSavings:
		restored.Parameters.RetirementAge = base.Parameters.RetirementAge
		restored.Parameters.AnnualSavingsMinor = base.Parameters.AnnualSavingsMinor
	case TypeRequiredCurrentAssets:
		restored.Parameters.TotalAssetsMinor = base.Parameters.TotalAssetsMinor
		if len(restored.Assets) != len(base.Assets) {
			return ErrCandidateInvalid
		}
		for i := range restored.Assets {
			restored.Assets[i].InitialMinor = base.Assets[i].InitialMinor
		}
	case TypeCoastRequiredAssets:
		restored.Parameters.AnnualSavingsMinor = base.Parameters.AnnualSavingsMinor
		restored.Parameters.TotalAssetsMinor = base.Parameters.TotalAssetsMinor
		if len(restored.Assets) != len(base.Assets) {
			return ErrCandidateInvalid
		}
		for i := range restored.Assets {
			restored.Assets[i].InitialMinor = base.Assets[i].InitialMinor
		}
	default:
		return ErrCandidateInvalid
	}
	if !reflect.DeepEqual(base, restored) {
		return fmt.Errorf("%w: snapshot contains a change outside the frontier whitelist", ErrCandidateInvalid)
	}
	return nil
}

func scaleAssets(snapshot *simulation.InputSnapshot, total int64) error {
	if total < 1 {
		return fmt.Errorf("%w: candidate assets must be positive", ErrCandidateInvalid)
	}
	originalTotal := int64(0)
	for _, asset := range snapshot.Assets {
		if asset.InitialMinor < 0 || asset.InitialMinor > math.MaxInt64-originalTotal {
			return fmt.Errorf("%w: invalid original asset amounts", ErrCandidateInvalid)
		}
		originalTotal += asset.InitialMinor
	}
	if originalTotal != snapshot.Parameters.TotalAssetsMinor {
		return fmt.Errorf("%w: frozen asset amounts do not equal total assets", ErrCandidateInvalid)
	}
	var weights []*big.Rat
	eligible := make([]bool, len(snapshot.Assets))
	if originalTotal > 0 {
		weights = make([]*big.Rat, len(snapshot.Assets))
		for i := range snapshot.Assets {
			weights[i] = new(big.Rat).SetFrac(big.NewInt(snapshot.Assets[i].InitialMinor), big.NewInt(originalTotal))
			eligible[i] = snapshot.Assets[i].InitialMinor > 0
		}
	} else {
		weights = make([]*big.Rat, len(snapshot.Assets))
		sum := new(big.Rat)
		for i := range snapshot.Assets {
			weight := snapshot.Assets[i].TargetWeight
			if math.IsNaN(weight) || math.IsInf(weight, 0) || weight < 0 {
				return fmt.Errorf("%w: invalid target weights for zero-total fallback", ErrCandidateInvalid)
			}
			weights[i] = new(big.Rat).SetFloat64(weight)
			sum.Add(sum, weights[i])
			eligible[i] = weight > 0
		}
		one := new(big.Rat).SetInt64(1)
		delta := new(big.Rat).Sub(sum, one)
		abs, _ := new(big.Float).Abs(new(big.Float).SetRat(delta)).Float64()
		if sum.Sign() <= 0 || abs > 1e-9 {
			return fmt.Errorf("%w: target weights must sum to 1 for zero-total fallback", ErrCandidateInvalid)
		}
		for i := range weights {
			weights[i] = new(big.Rat).Quo(weights[i], sum)
		}
	}
	amounts, err := largestRemainder(total, snapshot.Assets, weights, eligible)
	if err != nil {
		return err
	}
	for i := range snapshot.Assets {
		snapshot.Assets[i].InitialMinor = amounts[i]
	}
	snapshot.Parameters.TotalAssetsMinor = total
	return nil
}

type remainderItem struct {
	index     int
	assetKey  string
	remainder *big.Rat
}

func largestRemainder(total int64, assets []simulation.SnapshotAsset, weights []*big.Rat,
	eligible []bool,
) ([]int64, error) {
	if len(assets) == 0 || len(weights) != len(assets) || len(eligible) != len(assets) {
		return nil, fmt.Errorf("%w: no allocatable frozen assets", ErrCandidateInvalid)
	}
	amounts := make([]int64, len(assets))
	items := make([]remainderItem, 0, len(assets))
	allocated := int64(0)
	for i, weight := range weights {
		if weight == nil || weight.Sign() < 0 {
			return nil, ErrCandidateInvalid
		}
		raw := new(big.Rat).Mul(new(big.Rat).SetInt64(total), weight)
		base := new(big.Int).Quo(raw.Num(), raw.Denom())
		if !base.IsInt64() {
			return nil, ErrCandidateInvalid
		}
		amounts[i] = base.Int64()
		allocated += amounts[i]
		if eligible[i] {
			items = append(items, remainderItem{index: i, assetKey: assets[i].AssetKey,
				remainder: new(big.Rat).Sub(raw, new(big.Rat).SetInt(base))})
		}
	}
	remaining := total - allocated
	if remaining < 0 || remaining > int64(len(items)) {
		return nil, fmt.Errorf("%w: asset allocation does not conserve total", ErrCandidateInvalid)
	}
	sort.Slice(items, func(i, j int) bool {
		if cmp := items[i].remainder.Cmp(items[j].remainder); cmp != 0 {
			return cmp > 0
		}
		return items[i].assetKey < items[j].assetKey
	})
	for i := int64(0); i < remaining; i++ {
		amounts[items[i].index]++
	}
	check := int64(0)
	for _, amount := range amounts {
		if amount < 0 || amount > math.MaxInt64-check {
			return nil, ErrCandidateInvalid
		}
		check += amount
	}
	if check != total {
		return nil, ErrCandidateInvalid
	}
	return amounts, nil
}

func assetAmountMap(snapshot simulation.InputSnapshot) map[string]int64 {
	out := make(map[string]int64, len(snapshot.Assets))
	for _, asset := range snapshot.Assets {
		out[asset.AssetKey] = asset.InitialMinor
	}
	return out
}

func cloneSnapshot(base simulation.InputSnapshot) (simulation.InputSnapshot, error) {
	raw, err := json.Marshal(base)
	if err != nil {
		return simulation.InputSnapshot{}, fmt.Errorf("clone frontier snapshot: %w", err)
	}
	var out simulation.InputSnapshot
	if err := json.Unmarshal(raw, &out); err != nil {
		return simulation.InputSnapshot{}, fmt.Errorf("clone frontier snapshot: %w", err)
	}
	return out, nil
}

func cloneConfigHashInput(in domain.ConfigHashInput) domain.ConfigHashInput {
	out := in
	out.Parameters = cloneAnyMap(in.Parameters)
	out.AssetClass = cloneMapSlice(in.AssetClass)
	out.RegionTargets = cloneMapSlice(in.RegionTargets)
	out.Holdings = cloneMapSlice(in.Holdings)
	return out
}

func cloneMapSlice(in []map[string]any) []map[string]any {
	if in == nil {
		return nil
	}
	out := make([]map[string]any, len(in))
	for i := range in {
		out[i] = cloneAnyMap(in[i])
	}
	return out
}

func cloneAnyMap(in map[string]any) map[string]any {
	if in == nil {
		return nil
	}
	out := make(map[string]any, len(in))
	for key, value := range in {
		switch typed := value.(type) {
		case map[string]any:
			out[key] = cloneAnyMap(typed)
		case []map[string]any:
			out[key] = cloneMapSlice(typed)
		case []any:
			out[key] = append([]any(nil), typed...)
		default:
			out[key] = value
		}
	}
	return out
}

func DecodeConfigHashInput(raw json.RawMessage) (domain.ConfigHashInput, error) {
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	var out domain.ConfigHashInput
	if err := decoder.Decode(&out); err != nil {
		return domain.ConfigHashInput{}, err
	}
	return out, nil
}

func validateCandidateValues(snapshot simulation.InputSnapshot) error {
	p := snapshot.Parameters
	if p.CurrentAge < 0 || p.RetirementAge < p.CurrentAge || p.RetirementAge >= p.EndAge ||
		p.AnnualSavingsMinor < 0 || p.AnnualSpendingMinor < 1 || p.TotalAssetsMinor < 0 {
		return fmt.Errorf("%w: candidate plan parameters are invalid", ErrCandidateInvalid)
	}
	return nil
}
