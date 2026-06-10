package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
)

// Services groups business services.
type Services struct {
	Plans            *service.PlanService
	Allocation       *service.AllocationService
	Holdings         *service.HoldingsService
	Targets          *service.TargetService
	Rebalance        *service.RebalanceService
	Instruments      *service.InstrumentService
	HoldingSnapshots *service.HoldingSnapshotService
	Simulations      *service.SimulationService
	Stress           *service.StressService
	Sensitivity      *service.SensitivityService
	Jobs             *service.JobService
	Dashboard        *service.DashboardService
	System           *service.SystemService
	EventHub         *jobs.EventHub
	Maintenance      *service.MaintenanceGate
}

func NewServices(db *sql.DB, dbPath, marketProviderURL string, maintenance *service.MaintenanceGate) Services {
	if marketProviderURL == "" {
		marketProviderURL = "http://127.0.0.1:18081"
	}
	plans := repository.NewPlanRepo(db)
	params := repository.NewParametersRepo(db)
	alloc := repository.NewAllocationRepo(db)
	scenario := repository.NewScenarioRepo(db)
	holdings := repository.NewHoldingsRepo(db)
	instRepo := repository.NewInstrumentRepo(db)
	marketRepo := repository.NewMarketDataRepo(db)
	annualRepo := repository.NewAnnualReturnsRepo(db)
	snapRepo := repository.NewSnapshotRepo(db)
	hash := service.NewConfigHashService(plans, params, alloc, holdings)
	provider := marketdata.NewProviderClient(marketProviderURL)
	snapSvc := marketdata.NewSnapshotService(snapRepo, instRepo, marketRepo)
	jobRepo := repository.NewJobRepo(db)
	simRepo := repository.NewSimulationRepo(db)
	analysisRepo := repository.NewAnalysisRepo(db)
	eventHub := jobs.NewEventHub()
	targetSvc := service.NewTargetService(plans, params, alloc, holdings, hash)
	rebalanceSvc := service.NewRebalanceService(plans, params, alloc, holdings)
	simSvc := service.NewSimulationService(db, plans, params, alloc, holdings, snapRepo, instRepo, marketRepo, jobRepo, simRepo, hash)
	stressSvc := service.NewStressService(db, plans, jobRepo, analysisRepo, simSvc, hash)
	sensitivitySvc := service.NewSensitivityService(db, plans, jobRepo, analysisRepo, simSvc, hash)
	dashboardSvc := service.NewDashboardService(
		plans, params, alloc, scenario, holdings, instRepo, simRepo, analysisRepo, hash,
		targetSvc, rebalanceSvc, simSvc, stressSvc, sensitivitySvc,
	)

	planSvc := service.NewPlanService(db, plans, params, alloc, scenario, holdings, hash, snapSvc)
	return Services{
		Plans:            planSvc,
		Allocation:       service.NewAllocationService(db, plans, alloc, scenario),
		Holdings:         service.NewHoldingsService(db, plans, holdings, snapSvc),
		Targets:          targetSvc,
		Rebalance:        rebalanceSvc,
		Instruments:      service.NewInstrumentService(db, instRepo, marketRepo, annualRepo, jobRepo, provider),
		HoldingSnapshots: service.NewHoldingSnapshotService(db, plans, holdings, snapRepo, snapSvc),
		Simulations:      simSvc,
		Stress:           stressSvc,
		Sensitivity:      sensitivitySvc,
		Jobs:             service.NewJobService(jobRepo, simRepo, eventHub),
		Dashboard:        dashboardSvc,
		System:           service.NewSystemService(db, dbPath, planSvc, targetSvc, rebalanceSvc, maintenance),
		EventHub:         eventHub,
		Maintenance:      maintenance,
	}
}

func (s Services) registerPlanRoutes(rg *gin.RouterGroup) {
	rg.POST("/plans", s.createPlan)
	rg.POST("/plans/wizard", s.createPlanWizard)
	rg.GET("/plans", s.listPlans)
	rg.GET("/plans/:plan_id", s.getPlan)
	rg.PUT("/plans/:plan_id", s.updatePlan)
	rg.DELETE("/plans/:plan_id", s.deletePlan)

	rg.GET("/plans/:plan_id/parameters", s.getParameters)
	rg.PUT("/plans/:plan_id/parameters", s.updateParameters)
	rg.GET("/plans/:plan_id/allocation", s.getAllocation)
	rg.PUT("/plans/:plan_id/allocation", s.updateAllocation)
	rg.GET("/plans/:plan_id/holdings", s.getHoldings)
	rg.PUT("/plans/:plan_id/holdings", s.updateHoldings)
	rg.GET("/plans/:plan_id/targets", s.getTargets)
	rg.GET("/plans/:plan_id/rebalance", s.getRebalance)
	rg.POST("/plans/:plan_id/portfolio-snapshots", s.createPortfolioSnapshot)
	rg.POST("/plans/:plan_id/apply-scenario", s.applyScenario)
	rg.GET("/plans/:plan_id/dashboard", s.getDashboard)
}

func (s Services) getDashboard(c *gin.Context) {
	out, err := s.Dashboard.Get(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) registerScenarioRoutes(rg *gin.RouterGroup) {
	rg.GET("/allocation-scenarios", s.listScenarios)
	rg.POST("/allocation-scenarios", s.createScenario)
	rg.PUT("/allocation-scenarios/:scenario_id", s.updateScenario)
	rg.DELETE("/allocation-scenarios/:scenario_id", s.deleteScenario)
}

func (s Services) createPlan(c *gin.Context) {
	var req service.CreatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Plans.Create(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) createPlanWizard(c *gin.Context) {
	var req service.PlanWizardRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Plans.CreateWizard(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listPlans(c *gin.Context) {
	out, err := s.Plans.List(c.Request.Context())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getPlan(c *gin.Context) {
	out, err := s.Plans.Get(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) updatePlan(c *gin.Context) {
	var req service.UpdatePlanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Plans.Update(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) deletePlan(c *gin.Context) {
	if err := s.Plans.Delete(c.Request.Context(), c.Param("plan_id")); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"deleted": true})
}

func (s Services) getParameters(c *gin.Context) {
	params, flows, err := s.Plans.GetParameters(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"parameters": service.ParametersToAPI(params), "cash_flows": flows})
}

func (s Services) updateParameters(c *gin.Context) {
	var req service.ParametersUpdateAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	params, err := service.ParametersFromAPI(req.Parameters)
	if err != nil {
		Fail(c, http.StatusBadRequest, "parameters_invalid", err.Error(), nil)
		return
	}
	updated, flows, err := s.Plans.UpdateParameters(c.Request.Context(), c.Param("plan_id"), service.ParametersUpdateRequest{
		ConfigVersion: req.ConfigVersion, Parameters: params,
		CashFlows: req.CashFlows, ApplyUnallocatedToCash: req.ApplyUnallocatedToCash,
	})
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"parameters": service.ParametersToAPI(updated), "cash_flows": flows})
}

func (s Services) getAllocation(c *gin.Context) {
	out, err := s.Allocation.GetAllocation(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) updateAllocation(c *gin.Context) {
	var req service.AllocationUpdateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Allocation.UpdateAllocation(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getHoldings(c *gin.Context) {
	out, err := s.Holdings.GetHoldings(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"holdings": out})
}

func (s Services) updateHoldings(c *gin.Context) {
	body, err := c.GetRawData()
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if roErr := checkHoldingReadOnlyFields(body); roErr != nil {
		FailErr(c, roErr)
		return
	}
	var req service.HoldingsUpdateRequest
	if err := json.Unmarshal(body, &req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Holdings.UpdateHoldings(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"holdings": out})
}

func checkHoldingReadOnlyFields(body []byte) error {
	var raw struct {
		Holdings []map[string]json.RawMessage `json:"holdings"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil
	}
	readOnly := []string{
		"name", "code", "instrument_name", "instrument_code",
		"market", "asset_class", "region", "currency",
		"historical_cagr", "modeled_annual_return", "annual_volatility", "max_drawdown",
		"expense_ratio", "simulation_snapshot_id",
	}
	for _, h := range raw.Holdings {
		for _, f := range readOnly {
			if _, ok := h[f]; ok {
				return &service.AppError{
					Code:    "holding_fields_read_only",
					Message: "instrument metadata, risk/return metrics and simulation_snapshot_id are read-only",
					Details: map[string]any{"field": f},
				}
			}
		}
	}
	return nil
}

func (s Services) getTargets(c *gin.Context) {
	out, err := s.Targets.GetTargets(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getRebalance(c *gin.Context) {
	mode := c.Query("mode")
	var newCash int64
	if v := c.Query("new_cash_minor"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", "new_cash_minor must be an integer", nil)
			return
		}
		newCash = n
	}
	out, err := s.Rebalance.GetRebalance(c.Request.Context(), c.Param("plan_id"), mode, newCash)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) createPortfolioSnapshot(c *gin.Context) {
	var req service.CreatePortfolioSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Plans.CreatePortfolioSnapshot(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listScenarios(c *gin.Context) {
	out, err := s.Allocation.ListScenarios(c.Request.Context())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"scenarios": out})
}

func (s Services) createScenario(c *gin.Context) {
	var req service.ScenarioCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Allocation.CreateScenario(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) updateScenario(c *gin.Context) {
	var req service.ScenarioCreateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Allocation.UpdateScenario(c.Request.Context(), c.Param("scenario_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) deleteScenario(c *gin.Context) {
	if err := s.Allocation.DeleteScenario(c.Request.Context(), c.Param("scenario_id")); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"deleted": true})
}

func (s Services) applyScenario(c *gin.Context) {
	var req service.ApplyScenarioRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Allocation.ApplyScenario(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}
