package marketdata

import "math"

const CNAdjustedPriceMaxDailyMove = 0.25

// PriceDiscontinuity identifies an implausible jump in a continuous price series.
type PriceDiscontinuity struct {
	PreviousDate string
	Date         string
	Return       float64
}

// FindPriceDiscontinuity returns the first adjacent move above the supplied limit.
func FindPriceDiscontinuity(points []DataPoint, maxAbsReturn float64) (PriceDiscontinuity, bool) {
	for i := 1; i < len(points); i++ {
		previous := points[i-1]
		current := points[i]
		if previous.Value <= 0 || current.Value <= 0 {
			continue
		}
		ret := current.Value/previous.Value - 1
		if math.Abs(ret) > maxAbsReturn {
			return PriceDiscontinuity{
				PreviousDate: previous.TradeDate,
				Date:         current.TradeDate,
				Return:       ret,
			}, true
		}
	}
	return PriceDiscontinuity{}, false
}
