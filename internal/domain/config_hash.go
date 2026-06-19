package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

// ConfigHashInput is the canonical configuration snapshot used for change detection.
type ConfigHashInput struct {
	PlanID        string           `json:"plan_id"`
	Name          string           `json:"name"`
	BaseCurrency  string           `json:"base_currency"`
	ValuationDate string           `json:"valuation_date"`
	Parameters    map[string]any   `json:"parameters"`
	AssetClass    []map[string]any `json:"asset_class_targets"`
	RegionTargets []map[string]any `json:"region_targets"`
	Holdings      []map[string]any `json:"holdings"`
}

// ComputeConfigHash returns the SHA-256 hex digest of canonical plan JSON.
func ComputeConfigHash(in ConfigHashInput) (string, error) {
	sortConfigForHash(&in)
	b, err := json.Marshal(in)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

func sortConfigForHash(in *ConfigHashInput) {
	sort.Slice(in.AssetClass, func(i, j int) bool {
		return stringVal(in.AssetClass[i]["asset_class"]) < stringVal(in.AssetClass[j]["asset_class"])
	})
	sort.Slice(in.RegionTargets, func(i, j int) bool {
		a := stringVal(in.RegionTargets[i]["asset_class"]) + "/" + stringVal(in.RegionTargets[i]["region"])
		b := stringVal(in.RegionTargets[j]["asset_class"]) + "/" + stringVal(in.RegionTargets[j]["region"])
		return a < b
	})
	sort.Slice(in.Holdings, func(i, j int) bool {
		return stringVal(in.Holdings[i]["instrument_id"]) < stringVal(in.Holdings[j]["instrument_id"])
	})
}

func stringVal(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
