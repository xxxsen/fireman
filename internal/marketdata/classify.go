package marketdata

import (
	"fmt"
	"strings"
)

// Classification holds resolved instrument metadata.
type Classification struct {
	AssetClass string
	Region     string
	Currency   string
}

var supportedAssetClasses = map[string]struct{}{
	"equity": {}, "bond": {}, "cash": {},
}

// ResolveClassification maps provider metadata to persisted classification fields.
func ResolveClassification(market, instrumentType string, data *FetchData) (Classification, error) {
	if data.AssetClass == "fx" {
		return Classification{}, fmt.Errorf("instrument_classification_unsupported")
	}
	if _, ok := supportedAssetClasses[data.AssetClass]; !ok {
		return Classification{}, fmt.Errorf("instrument_classification_unsupported")
	}

	region := regionFromComponents(data.ExpenseRatioComponents)
	if region == "" {
		region = defaultRegion(market, instrumentType)
	}
	if region != "domestic" && region != "foreign" {
		return Classification{}, fmt.Errorf("instrument_metadata_conflict")
	}
	if data.Currency == "" {
		return Classification{}, fmt.Errorf("instrument_metadata_conflict")
	}
	return Classification{
		AssetClass: data.AssetClass,
		Region:     region,
		Currency:   data.Currency,
	}, nil
}

func regionFromComponents(components map[string]any) string {
	if components == nil {
		return ""
	}
	if v, ok := components["region"]; ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func defaultRegion(market, instrumentType string) string {
	switch strings.ToUpper(market) {
	case "US":
		return "foreign"
	case "HK":
		return "foreign"
	default:
		return "domestic"
	}
}

// FeeTreatmentForType reports whether historical values already include holding fees.
func FeeTreatmentForType(instrumentType string) string {
	switch instrumentType {
	case "cn_exchange_stock", "us_stock", "hk_stock":
		return "none"
	default:
		return "embedded"
	}
}

// DefaultAdjustPolicy picks adjust policy for import.
func DefaultAdjustPolicy(instrumentType string) string {
	switch instrumentType {
	case "cn_exchange_stock", "cn_exchange_fund", "us_stock", "us_etf", "hk_stock", "hk_etf":
		return "qfq"
	default:
		return "none"
	}
}

// ExpenseRatioFromComponents extracts validated expense ratio if present.
func ExpenseRatioFromComponents(components map[string]any) *float64 {
	if components == nil {
		return nil
	}
	v, ok := components["expense_ratio"]
	if !ok {
		return nil
	}
	switch n := v.(type) {
	case float64:
		if n >= 0 && n <= 0.10 {
			return &n
		}
	case int:
		f := float64(n)
		if f >= 0 && f <= 0.10 {
			return &f
		}
	}
	return nil
}
