package stress

import (
	"github.com/fireman/fireman/internal/domain"
	"github.com/fireman/fireman/internal/simulation"
)

// ScenarioID constants for the seven built-in stress scenarios.
const (
	ScenarioHistoricalMaxDrawdown = "historical_max_drawdown"
	ScenarioEarlyRetirementCrash  = "early_retirement_crash"
	ScenarioThreeYearStagflation  = "three_year_stagflation"
	ScenarioLostDecade            = "lost_decade"
	ScenarioRateShock             = "rate_shock"
	ScenarioFXHeadwind            = "fx_headwind"
	ScenarioMedicalExpenseShock   = "medical_expense_shock"
)

// Scenario describes one user-initiated stress overlay.
type Scenario struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	RiskHint    string `json:"risk_hint"`
	ShockEnd    int    `json:"shock_end_month"`
}

// BuiltinScenarios returns the seven fixed stress scenarios.
func BuiltinScenarios() []Scenario {
	return []Scenario{
		{
			ID: ScenarioHistoricalMaxDrawdown, Name: "历史最大回撤重现",
			Description: "退休后前 12 个月，每只基金分别承受其快照最大回撤对应的复合跌幅；系统现金不受影响",
			RiskHint:    "该场景使用各基金自身历史最大回撤，不代表会在同一年发生",
		},
		{
			ID: ScenarioEarlyRetirementCrash, Name: "退休初期股灾",
			Description: "退休后前 12 个月权益额外 -35%、债券额外 -5%；随后 24 个月权益年化漂移 +5 个百分点",
			RiskHint:    "顺序风险：退休初期大跌会显著拉低 FIRE 成功率",
		},
		{
			ID: ScenarioThreeYearStagflation, Name: "三年滞涨",
			Description: "退休起前 36 个月权益年化漂移 -8 个百分点、债券 -6 个百分点，年通胀固定 8%",
			RiskHint:    "高通胀叠加低回报会侵蚀实际购买力",
		},
		{
			ID: ScenarioLostDecade, Name: "失落十年",
			Description: "退休起前 10 年权益期望收益 -5 个百分点、债券 -1 个百分点",
			RiskHint:    "长期低回报会压缩安全提取空间",
		},
		{
			ID: ScenarioRateShock, Name: "利率冲击",
			Description: "第 1 年债券额外 -15%、权益额外 -10%；第 2 至 5 年债券期望收益 +2 个百分点",
			RiskHint:    "利率上行初期股债双杀，随后债券收益回升",
		},
		{
			ID: ScenarioFXHeadwind, Name: "汇率逆风",
			Description: "退休起前 12 个月人民币相对外币升值 15%，只影响直接外币资产",
			RiskHint:    "本币升值会降低外币资产的本币回报",
		},
		{
			ID: ScenarioMedicalExpenseShock, Name: "医疗支出冲击",
			Description: "退休第 3 年一次性增加 2 倍基准年支出，随后 5 年年支出增加 20%",
			RiskHint:    "突发大额医疗支出会加速资产耗尽",
		},
	}
}

// CompileSchedule builds the monthly shock overlay for a scenario.
func CompileSchedule(scenarioID string, in *simulation.InputSnapshot) simulation.ShockSchedule {
	start := shockStartMonth(in)
	switch scenarioID {
	case ScenarioHistoricalMaxDrawdown:
		return compileHistoricalMaxDrawdown(in, start)
	case ScenarioEarlyRetirementCrash:
		return compileEarlyRetirementCrash(in, start)
	case ScenarioThreeYearStagflation:
		return compileThreeYearStagflation(in, start)
	case ScenarioLostDecade:
		return compileLostDecade(in, start)
	case ScenarioRateShock:
		return compileRateShock(in, start)
	case ScenarioFXHeadwind:
		return compileFXHeadwind(in, start)
	case ScenarioMedicalExpenseShock:
		return compileMedicalExpenseShock(in, in.RetirementMonth())
	default:
		return nil
	}
}

func shockStartMonth(in *simulation.InputSnapshot) int {
	retire := in.RetirementMonth()
	if in.Parameters.CurrentAge >= in.Parameters.RetirementAge {
		return 0
	}
	return retire
}

func compileHistoricalMaxDrawdown(in *simulation.InputSnapshot, start int) simulation.ShockSchedule {
	sched := simulation.ShockSchedule{}
	for m := start; m < start+12 && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{Assets: map[int]simulation.AssetShock{}}
		for i, a := range in.Assets {
			if a.IsCash || a.AssetClass == domain.AssetClassCash {
				continue
			}
			ms.Assets[i] = simulation.AssetShock{
				ReturnMul: simulation.DrawdownToMonthlyShock(a.MaxDrawdown),
			}
		}
		sched[m] = ms
	}
	return sched
}

func compileEarlyRetirementCrash(in *simulation.InputSnapshot, start int) simulation.ShockSchedule {
	sched := simulation.ShockSchedule{}
	equityShock := simulation.AnnualToMonthlyCompound(-0.35)
	bondShock := simulation.AnnualToMonthlyCompound(-0.05)
	for m := start; m < start+12 && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{Assets: map[int]simulation.AssetShock{}}
		for i, a := range in.Assets {
			if a.IsCash {
				continue
			}
			switch a.AssetClass {
			case domain.AssetClassEquity:
				ms.Assets[i] = simulation.AssetShock{ReturnMul: equityShock}
			case domain.AssetClassBond:
				ms.Assets[i] = simulation.AssetShock{ReturnMul: bondShock}
			}
		}
		sched[m] = ms
	}
	for m := start + 12; m < start+36 && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{Assets: map[int]simulation.AssetShock{}}
		for i, a := range in.Assets {
			if a.IsCash || a.AssetClass != domain.AssetClassEquity {
				continue
			}
			ms.Assets[i] = simulation.AssetShock{DriftDelta: 0.05}
		}
		if len(ms.Assets) > 0 {
			sched[m] = ms
		}
	}
	return sched
}

func compileThreeYearStagflation(in *simulation.InputSnapshot, start int) simulation.ShockSchedule {
	sched := simulation.ShockSchedule{}
	infl := 0.08
	for m := start; m < start+36 && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{
			Assets:          map[int]simulation.AssetShock{},
			InflationAnnual: &infl,
		}
		for i, a := range in.Assets {
			if a.IsCash {
				continue
			}
			switch a.AssetClass {
			case domain.AssetClassEquity:
				ms.Assets[i] = simulation.AssetShock{DriftDelta: -0.08}
			case domain.AssetClassBond:
				ms.Assets[i] = simulation.AssetShock{DriftDelta: -0.06}
			}
		}
		sched[m] = ms
	}
	return sched
}

func compileLostDecade(in *simulation.InputSnapshot, start int) simulation.ShockSchedule {
	sched := simulation.ShockSchedule{}
	end := start + 10*12
	for m := start; m < end && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{Assets: map[int]simulation.AssetShock{}}
		for i, a := range in.Assets {
			if a.IsCash {
				continue
			}
			switch a.AssetClass {
			case domain.AssetClassEquity:
				ms.Assets[i] = simulation.AssetShock{DriftDelta: -0.05}
			case domain.AssetClassBond:
				ms.Assets[i] = simulation.AssetShock{DriftDelta: -0.01}
			}
		}
		sched[m] = ms
	}
	return sched
}

func compileRateShock(in *simulation.InputSnapshot, start int) simulation.ShockSchedule {
	sched := simulation.ShockSchedule{}
	bondShock := simulation.AnnualToMonthlyCompound(-0.15)
	equityShock := simulation.AnnualToMonthlyCompound(-0.10)
	for m := start; m < start+12 && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{Assets: map[int]simulation.AssetShock{}}
		for i, a := range in.Assets {
			if a.IsCash {
				continue
			}
			switch a.AssetClass {
			case domain.AssetClassEquity:
				ms.Assets[i] = simulation.AssetShock{ReturnMul: equityShock}
			case domain.AssetClassBond:
				ms.Assets[i] = simulation.AssetShock{ReturnMul: bondShock}
			}
		}
		sched[m] = ms
	}
	for m := start + 12; m < start+5*12 && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{Assets: map[int]simulation.AssetShock{}}
		for i, a := range in.Assets {
			if a.IsCash || a.AssetClass != domain.AssetClassBond {
				continue
			}
			ms.Assets[i] = simulation.AssetShock{DriftDelta: 0.02}
		}
		if len(ms.Assets) > 0 {
			sched[m] = ms
		}
	}
	return sched
}

func compileFXHeadwind(in *simulation.InputSnapshot, start int) simulation.ShockSchedule {
	sched := simulation.ShockSchedule{}
	// CNY appreciation 15% annualized hurts foreign assets: FX leg gets negative shock.
	fxShock := simulation.AnnualToMonthlyCompound(-0.15)
	for m := start; m < start+12 && m < in.HorizonMonths(); m++ {
		ms := simulation.MonthShock{Assets: map[int]simulation.AssetShock{}}
		for i, a := range in.Assets {
			if a.IsCash || a.Currency == in.BaseCurrency || a.FXSnapshotID == "" {
				continue
			}
			ms.Assets[i] = simulation.AssetShock{FXReturnMul: fxShock}
		}
		if len(ms.Assets) > 0 {
			sched[m] = ms
		}
	}
	return sched
}

func compileMedicalExpenseShock(in *simulation.InputSnapshot, retire int) simulation.ShockSchedule {
	sched := simulation.ShockSchedule{}
	year3 := retire + 24
	if year3 < in.HorizonMonths() {
		extra := in.Parameters.AnnualSpendingMinor * 2
		sched[year3] = simulation.MonthShock{ExtraSpendingMinor: extra}
	}
	for m := year3 + 1; m < year3+5*12 && m < in.HorizonMonths(); m++ {
		sched[m] = simulation.MonthShock{SpendingMultiplier: 1.20}
	}
	return sched
}

// ShockEndMonth returns the last month index affected by a scenario schedule.
func ShockEndMonth(sched simulation.ShockSchedule) int {
	end := -1
	for m := range sched {
		if m > end {
			end = m
		}
	}
	return end
}
