package domain

const (
	WeightTolerance = 0.0001

	AssetClassEquity = "equity"
	AssetClassBond   = "bond"
	AssetClassCash   = "cash"

	RegionDomestic = "domestic"
	RegionForeign  = "foreign"

	RebalanceActionDisabled = "disabled"
	RebalanceActionHold     = "hold"
	RebalanceActionIncrease = "increase"
	RebalanceActionDecrease = "decrease"

	RebalanceModeFull    = "full"
	RebalanceModeNewCash = "new_cash"
)

// AssetClasses is the ordered list of asset classes used in allocation.
var AssetClasses = []string{AssetClassEquity, AssetClassBond, AssetClassCash}

// Regions is the ordered list of regions within an asset class.
var Regions = []string{RegionDomestic, RegionForeign}
