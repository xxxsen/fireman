package marketdata

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"sort"
)

type hashPoint struct {
	TradeDate  string  `json:"trade_date"`
	Value      float64 `json:"value"`
	PointType  string  `json:"point_type"`
	SourceName string  `json:"source_name"`
}

type metricsHashInput struct {
	MetricsVersion string      `json:"metrics_version"`
	PointType      string      `json:"point_type"`
	SourceName     string      `json:"source_name"`
	SelectedYears  []int       `json:"selected_years"`
	Points         []hashPoint `json:"points"`
}

// ComputeSourceHash returns the SHA-256 digest of canonical market-data JSON.
func ComputeSourceHash(points []DataPoint, pointType, sourceName string) string {
	return ComputeMetricsSourceHash(points, pointType, sourceName, nil, MetricsVersionMonthlyLogReturnV1)
}

// ComputeMetricsSourceHash hashes metrics version, selected years and point set.
func ComputeMetricsSourceHash(
	points []DataPoint,
	pointType, sourceName string,
	years []SimulationYear,
	metricsVersion string,
) string {
	rows := make([]hashPoint, len(points))
	for i, p := range points {
		rows[i] = hashPoint{
			TradeDate: p.TradeDate, Value: p.Value,
			PointType: pointType, SourceName: sourceName,
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TradeDate < rows[j].TradeDate })
	selected := make([]int, len(years))
	for i, y := range years {
		selected[i] = y.Year
	}
	input := metricsHashInput{
		MetricsVersion: metricsVersion,
		PointType:      pointType,
		SourceName:     sourceName,
		SelectedYears:  selected,
		Points:         rows,
	}
	raw, _ := json.Marshal(input)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
