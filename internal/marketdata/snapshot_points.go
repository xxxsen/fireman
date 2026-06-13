package marketdata

import "sort"

type snapshotPointKey struct {
	TradeDate  string
	PointType  string
	SourceName string
}

// BuildSnapshotPointSet builds the canonical point set used for snapshot metrics.
func BuildSnapshotPointSet(points []DataPoint, years []SimulationYear, pointType, sourceName string) []DataPoint {
	if len(years) == 0 {
		return nil
	}
	segments := ConsecutiveYearSegments(years)
	seen := map[snapshotPointKey]DataPoint{}
	add := func(p DataPoint) {
		pt := p.PointType
		if pt == "" {
			pt = pointType
		}
		src := p.SourceName
		if src == "" {
			src = sourceName
		}
		key := snapshotPointKey{TradeDate: p.TradeDate, PointType: pt, SourceName: src}
		p.PointType = pt
		p.SourceName = src
		seen[key] = p
	}
	for _, seg := range segments {
		if len(seg) == 0 {
			continue
		}
		if anchor, ok := anchorBefore(points, seg[0].Year); ok {
			add(anchor)
		}
		yearSet := map[int]struct{}{}
		for _, y := range seg {
			yearSet[y.Year] = struct{}{}
		}
		for _, p := range points {
			if _, ok := yearSet[yearOf(p.TradeDate)]; ok {
				add(p)
			}
		}
	}
	out := make([]DataPoint, 0, len(seen))
	for _, p := range seen {
		out = append(out, p)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].TradeDate != out[j].TradeDate {
			return out[i].TradeDate < out[j].TradeDate
		}
		if out[i].PointType != out[j].PointType {
			return out[i].PointType < out[j].PointType
		}
		return out[i].SourceName < out[j].SourceName
	})
	return out
}

// WindowBoundsFromPoints returns inclusive window bounds from a point set.
func WindowBoundsFromPoints(points []DataPoint) (string, string) {
	if len(points) == 0 {
		return "", ""
	}
	return points[0].TradeDate, points[len(points)-1].TradeDate
}
