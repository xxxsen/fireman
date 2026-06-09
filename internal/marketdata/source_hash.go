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

// ComputeSourceHash returns the SHA-256 digest of canonical market-data JSON.
func ComputeSourceHash(points []DataPoint, pointType, sourceName string) string {
	rows := make([]hashPoint, len(points))
	for i, p := range points {
		rows[i] = hashPoint{
			TradeDate: p.TradeDate, Value: p.Value,
			PointType: pointType, SourceName: sourceName,
		}
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].TradeDate < rows[j].TradeDate })
	raw, _ := json.Marshal(rows)
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])
}
