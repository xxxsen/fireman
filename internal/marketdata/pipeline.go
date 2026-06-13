package marketdata

import (
	"fmt"
	"time"
)

// ProcessFetchResult cleans provider data and derives annual returns.
type ProcessFetchResult struct {
	Points     []DataPoint
	Annual     []AnnualReturnRow
	HasAnomaly bool
	Quality    string
	PointType  string
	SourceName string
}

// ProcessProviderData applies cleaning and library quality checks.
func ProcessProviderData(data *FetchData, asOfDate string) ProcessFetchResult {
	points := CleanPoints(data.Points)
	for i := range points {
		points[i].PointType = data.PointType
		points[i].SourceName = data.SourceName
		points[i].FetchedAt = time.Now().UnixMilli()
	}
	hasAnomaly := DetectDailyAnomaly(points)
	annual := ComputeAnnualReturns(points)
	quality := DetermineLibraryQuality(points, annual, asOfDate, hasAnomaly)
	return ProcessFetchResult{
		Points: points, Annual: annual, HasAnomaly: hasAnomaly,
		Quality: quality, PointType: data.PointType, SourceName: data.SourceName,
	}
}

// BuildSnapshotMetrics computes plan snapshot from stored points.
func BuildSnapshotMetrics(points []DataPoint, inclusionDate, pointType, sourceName string) SnapshotMetrics {
	annual := ComputeAnnualReturns(points)
	years := SelectSimulationYears(points, annual, inclusionDate)
	return ComputeMetrics(points, years, pointType, sourceName)
}

// MergeRefreshedPoints replaces overlapping refresh data while preserving earlier history.
func MergeRefreshedPoints(existing, incoming []DataPoint) []DataPoint {
	byDate := make(map[string]DataPoint, len(existing)+len(incoming))
	dates := make([]string, 0, len(existing)+len(incoming))
	add := func(p DataPoint) {
		if _, ok := byDate[p.TradeDate]; !ok {
			dates = append(dates, p.TradeDate)
		}
		byDate[p.TradeDate] = p
	}
	for _, p := range existing {
		add(p)
	}
	for _, p := range incoming {
		add(p)
	}
	sortStrings(dates)
	out := make([]DataPoint, 0, len(dates))
	for _, d := range dates {
		out = append(out, byDate[d])
	}
	return out
}

func sortStrings(ss []string) {
	for i := 0; i < len(ss); i++ {
		for j := i + 1; j < len(ss); j++ {
			if ss[j] < ss[i] {
				ss[i], ss[j] = ss[j], ss[i]
			}
		}
	}
}

// RefreshStartDate returns overlap start (last date - 10 days).
func RefreshStartDate(lastDate string) (string, error) {
	t, err := time.Parse("2006-01-02", lastDate)
	if err != nil {
		return "", fmt.Errorf("parse last trade date: %w", err)
	}
	return t.AddDate(0, 0, -10).Format("2006-01-02"), nil
}

// DominantSourceName returns the most frequent source_name in stored points.
func DominantSourceName(points []DataPoint) string {
	counts := make(map[string]int)
	for _, p := range points {
		if p.SourceName == "" {
			continue
		}
		counts[p.SourceName]++
	}
	best := ""
	maxCount := 0
	for name, n := range counts {
		if n > maxCount {
			maxCount = n
			best = name
		}
	}
	return best
}

// ShouldFullReplaceOnRefresh reports when refresh should replace all stored history.
func ShouldFullReplaceOnRefresh(force bool, existing []DataPoint, incomingSource string) bool {
	if force {
		return true
	}
	if len(existing) == 0 {
		return false
	}
	dominant := DominantSourceName(existing)
	if dominant == "" {
		return false
	}
	if incomingSource != "" {
		return dominant != incomingSource
	}
	// Pre-fetch: unadjusted sina ETF history must be replaced for adjusted pipelines.
	return dominant == "ak.fund_etf_hist_sina"
}
