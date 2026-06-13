package domain

import (
	"fmt"
	"strings"
)

// AssetClassCoverage describes how much of an asset-class target is covered by holdings.
type AssetClassCoverage struct {
	AssetClass string  `json:"asset_class"`
	Target     float64 `json:"target"`
	Covered    float64 `json:"covered"`
	Gap        float64 `json:"gap"`
}

// PortfolioCoverageByClass sums portfolio target weights per asset class and finds shortfalls.
func PortfolioCoverageByClass(alloc AllocationWeights, holdings []HoldingWeightInput) []AssetClassCoverage {
	covered := map[string]float64{}
	for _, ac := range AssetClasses {
		covered[ac] = 0
	}
	for _, h := range holdings {
		if !h.Enabled {
			continue
		}
		covered[h.AssetClass] += PortfolioTargetWeight(alloc, h)
	}

	var out []AssetClassCoverage
	for _, ac := range AssetClasses {
		target := alloc.AssetClass[ac]
		if target <= WeightTolerance {
			continue
		}
		cov := covered[ac]
		gap := target - cov
		if gap > WeightTolerance {
			out = append(out, AssetClassCoverage{
				AssetClass: ac, Target: target, Covered: cov, Gap: gap,
			})
		}
	}
	return out
}

func formatPortfolioWeightMessage(actual, target float64, missing []AssetClassCoverage, holdings []HoldingWeightInput,
	alloc AllocationWeights,
) string {
	gap := target - actual
	msg := fmt.Sprintf("%s当前为 %s，还差 %s。",
		"已启用标的全组合目标权重", formatPercent(actual), formatPercent(gap))

	if len(missing) > 0 {
		parts := make([]string, 0, len(missing))
		for _, m := range missing {
			parts = append(parts, fmt.Sprintf("%s（目标 %s）", assetClassLabel(m.AssetClass), formatPercent(m.Target)))
		}
		msg += "还缺少：" + strings.Join(parts, "、") + "。"
	}

	configured := configuredAssetClassSummary(alloc, holdings)
	if configured != "" {
		msg += "已配置：" + configured + "。"
	}
	msg += "请补充对应方向标的或调整场景配置。"
	return msg
}

func configuredAssetClassSummary(alloc AllocationWeights, holdings []HoldingWeightInput) string {
	covered := map[string]float64{}
	for _, h := range holdings {
		if !h.Enabled {
			continue
		}
		covered[h.AssetClass] += PortfolioTargetWeight(alloc, h)
	}
	var parts []string
	for _, ac := range AssetClasses {
		if covered[ac] > WeightTolerance {
			parts = append(parts, fmt.Sprintf("%s %s", assetClassLabel(ac), formatPercent(covered[ac])))
		}
	}
	return strings.Join(parts, "、")
}
