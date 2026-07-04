package service

import (
	"context"
	"fmt"
	"strings"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
)

func validateUserAssetClass(assetClass string) error {
	if err := marketdata.ValidateUserAssetClass(assetClass); err != nil {
		return newErr("invalid_request", "asset_class must be equity, bond, or cash", nil)
	}
	return nil
}

func validateUserRegion(region string) error {
	if err := marketdata.ValidateUserRegion(region); err != nil {
		return newErr("invalid_request", "region must be domestic or foreign", nil)
	}
	return nil
}

func defaultImportRegion(market string) string {
	if strings.EqualFold(market, "HK") || strings.EqualFold(market, "US") {
		return "foreign"
	}
	return "domestic"
}

func defaultImportAssetClass(instrumentType string) string {
	if instrumentType == "cn_mutual_fund" {
		return "bond"
	}
	return "equity"
}

func defaultCurrency(market string) string {
	switch strings.ToUpper(market) {
	case "HK":
		return "HKD"
	case "US":
		return "USD"
	default:
		return "CNY"
	}
}

// EnsureInstrumentReadyForPlan rejects instruments that are not active with available library quality.
func EnsureInstrumentReadyForPlan(inst repository.Instrument, qualityStatus string) error {
	if inst.ID == repository.SystemCashInstrumentID && inst.Status == "active" && qualityStatus == "available" {
		return nil
	}
	if inst.IsSystem {
		return newErr("instrument_not_ready", "system instrument cannot be used as a plan holding", map[string]any{
			"instrument_id": inst.ID,
		})
	}
	if inst.Status != "active" {
		return newErr("instrument_not_ready", fmt.Sprintf("instrument status is %s", inst.Status), map[string]any{
			"instrument_id": inst.ID, "status": inst.Status,
		})
	}
	if qualityStatus != "available" {
		return newErr("instrument_insufficient_history", "instrument does not have enough complete years for simulation",
			map[string]any{
				"instrument_id": inst.ID, "quality_status": qualityStatus,
			})
	}
	return nil
}

// EnsureInstrumentRecordReadyForPlan checks an enriched instrument record.
func EnsureInstrumentRecordReadyForPlan(inst repository.InstrumentRecord) error {
	return EnsureInstrumentReadyForPlan(repository.Instrument{
		ID: inst.ID, Code: inst.Code, Name: inst.Name, Market: inst.Market,
		AssetClass: inst.AssetClass, Region: inst.Region, Currency: inst.Currency,
		Status: inst.Status, IsSystem: inst.IsSystem,
	}, inst.QualityStatus)
}

// LibraryQuality returns the current library quality status for an instrument.
func (s *InstrumentService) LibraryQuality(ctx context.Context, instrumentID string) string {
	return s.libraryQuality(ctx, instrumentID)
}
