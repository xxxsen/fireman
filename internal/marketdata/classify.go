package marketdata

import (
	"errors"
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

var supportedRegions = map[string]struct{}{
	"domestic": {}, "foreign": {},
}

var (
	errClassificationUnsupported = errors.New("instrument_classification_unsupported")
	errMetadataConflict          = errors.New("instrument_metadata_conflict")
)

// ValidateUserAssetClass reports whether the user-selected asset class is supported.
func ValidateUserAssetClass(assetClass string) error {
	if _, ok := supportedAssetClasses[assetClass]; !ok {
		return errClassificationUnsupported
	}
	return nil
}

// ValidateUserRegion reports whether the user-selected region is supported.
func ValidateUserRegion(region string) error {
	if _, ok := supportedRegions[region]; !ok {
		return errMetadataConflict
	}
	return nil
}

// UserClassification builds persisted classification from explicit user asset class and region.
func UserClassification(market, instrumentType, assetClass, region, currency string) (Classification, error) {
	if err := ValidateUserAssetClass(assetClass); err != nil {
		return Classification{}, err
	}
	if err := ValidateUserRegion(region); err != nil {
		return Classification{}, err
	}
	if currency == "" {
		return Classification{}, errMetadataConflict
	}
	_ = market
	_ = instrumentType
	return Classification{
		AssetClass: assetClass,
		Region:     region,
		Currency:   currency,
	}, nil
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
