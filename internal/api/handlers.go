package api

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/marketdata"
	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/service"
	taskcore "github.com/fireman/fireman/internal/task"
)

// Services groups business services.
type Services struct {
	Plans               *service.PlanService
	Allocation          *service.AllocationService
	Holdings            *service.HoldingsService
	Targets             *service.TargetService
	Rebalance           *service.RebalanceService
	RebalanceExecutions *service.RebalanceExecutionService
	AssetRefresh        *service.AssetRefreshService
	MarketAssets        *service.MarketAssetService
	HoldingSnapshots    *service.HoldingSnapshotService
	Simulations         *service.SimulationService
	SimulationReadiness *service.SimulationReadinessService
	Assumptions         *service.AssumptionService
	Stress              *service.StressService
	Sensitivity         *service.SensitivityService
	Improvements        *service.FirePlanImprovementService
	Tasks               *service.TaskService
	TaskCoordinator     *taskcore.Coordinator
	Research            *service.ResearchService
	Dashboard           *service.DashboardService
	System              *service.SystemService
	Admin               *service.AdminService
	AutoUpdates         *service.AutoUpdateService
	EventHub            *taskcore.EventHub
	Maintenance         *service.MaintenanceGate
}

// NewServices wires the business service graph. resources may be nil (tests,
// router fallback); the admin overview then reports zero resource storage.
//
//nolint:funlen,lll // Service wiring stays centralized so every API shares one dependency graph.
func NewServices(
	db *sql.DB, dbPath string, maintenance *service.MaintenanceGate, resources *resourcedb.DB, loc *time.Location,
) Services {
	plans := repository.NewPlanRepo(db)
	params := repository.NewParametersRepo(db)
	alloc := repository.NewAllocationRepo(db)
	scenario := repository.NewScenarioRepo(db)
	holdings := repository.NewHoldingsRepo(db)
	instRepo := repository.NewInstrumentRepo(db)
	marketRepo := repository.NewMarketDataRepo(db)
	snapRepo := repository.NewSnapshotRepo(db)
	workerTaskRepo := repository.NewWorkerTaskRepo(db)
	taskRegistry := taskcore.DefaultRegistry()
	taskEventHub := taskcore.NewEventHub()
	taskCoordinator := taskcore.NewCoordinator(db, workerTaskRepo, taskRegistry, taskEventHub)
	marketAssetRepo := repository.NewMarketAssetRepo(db)
	assumptionRepo := repository.NewAssumptionProfileRepo(db)
	hash := service.NewConfigHashService(
		plans, params, alloc, holdings, repository.NewReturnOverrideRepo(db), assumptionRepo,
	)
	snapSvc := marketdata.NewSnapshotService(snapRepo, marketAssetRepo)
	simRepo := repository.NewSimulationRepo(db)
	analysisRepo := repository.NewAnalysisRepo(db)
	improvementRepo := repository.NewFirePlanImprovementRepo(db)
	researchRepo := repository.NewResearchRepo(db)
	targetSvc := service.NewTargetService(plans, params, alloc, holdings, hash)
	rebalanceSvc := service.NewRebalanceService(plans, params, alloc, holdings)
	holdingsSvc := service.NewHoldingsService(db, plans, holdings, snapSvc, marketAssetRepo)
	executionRepo := repository.NewRebalanceExecutionRepo(db)
	rebalanceExecutionSvc := service.NewRebalanceExecutionService(
		db, plans, executionRepo, holdings, holdingsSvc, rebalanceSvc,
	)
	marketAssetSvc := service.NewMarketAssetService(
		db, workerTaskRepo, marketAssetRepo, taskCoordinator,
	)
	readinessSvc := service.NewSimulationReadinessService(
		db, plans, holdings, marketAssetRepo, workerTaskRepo, snapSvc, marketAssetSvc,
	)
	simSvc := service.NewSimulationService(
		db, plans, params, alloc, holdings, snapRepo, marketAssetRepo, instRepo, marketRepo,
		workerTaskRepo, taskCoordinator, simRepo, analysisRepo, hash, readinessSvc,
	)
	stressSvc := service.NewStressService(db, plans, workerTaskRepo, taskCoordinator, analysisRepo, simSvc, hash)
	sensitivitySvc := service.NewSensitivityService(db, plans, workerTaskRepo, taskCoordinator, analysisRepo, simSvc, hash)
	improvementSvc := service.NewFirePlanImprovementService(
		db, plans, params, simRepo, workerTaskRepo, taskCoordinator,
		improvementRepo, hash, simSvc,
	)
	dashboardSvc := service.NewDashboardService(
		plans, params, alloc, scenario, holdings, simRepo, analysisRepo, hash,
		targetSvc, rebalanceSvc, simSvc, stressSvc, sensitivitySvc, executionRepo,
	)

	planSvc := service.NewPlanService(db, plans, params, alloc, scenario, holdings, marketAssetRepo, hash, snapSvc)
	planSvc.SetTaskCoordinator(taskCoordinator)
	researchSvc := service.NewResearchService(
		db, researchRepo, marketAssetRepo, workerTaskRepo, taskCoordinator,
		instRepo, marketRepo, plans, holdings, marketAssetSvc,
	)
	taskCancellationSvc := service.NewTaskCancellationService(
		db, taskCoordinator, researchRepo, improvementRepo,
	)
	adminSvc := service.NewAdminService(
		workerTaskRepo, repository.NewWorkerTaskFinalizeRecordRepo(db),
		marketAssetRepo, marketAssetSvc, resources, dbPath,
	)
	autoUpdates := service.NewAutoUpdateService(
		repository.NewMarketDataAutoUpdateRepo(db), marketAssetRepo, marketAssetSvc, loc,
	)
	return Services{
		Plans:               planSvc,
		Allocation:          service.NewAllocationService(db, plans, params, alloc, scenario),
		Holdings:            holdingsSvc,
		Targets:             targetSvc,
		Rebalance:           rebalanceSvc,
		RebalanceExecutions: rebalanceExecutionSvc,
		AssetRefresh: service.NewAssetRefreshService(
			db, plans, params, alloc, scenario, holdingsSvc, repository.NewAssetRefreshEventRepo(db), executionRepo,
		),
		MarketAssets:        marketAssetSvc,
		HoldingSnapshots:    service.NewHoldingSnapshotService(db, plans, holdings, snapRepo, snapSvc),
		Simulations:         simSvc,
		SimulationReadiness: readinessSvc,
		Assumptions:         service.NewAssumptionService(db),
		Stress:              stressSvc,
		Sensitivity:         sensitivitySvc,
		Improvements:        improvementSvc,
		Tasks:               service.NewTaskService(taskCoordinator, taskCancellationSvc),
		TaskCoordinator:     taskCoordinator,
		Research:            researchSvc,
		Dashboard:           dashboardSvc,
		System:              service.NewSystemService(db, dbPath, planSvc, targetSvc, rebalanceSvc, maintenance),
		Admin:               adminSvc,
		AutoUpdates:         autoUpdates,
		EventHub:            taskEventHub,
		Maintenance:         maintenance,
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
	rg.PUT("/plans/:plan_id/settings", s.updatePlanSettings)
	rg.GET("/plans/:plan_id/allocation", s.getAllocation)
	rg.PUT("/plans/:plan_id/allocation", s.updateAllocation)
	rg.GET("/plans/:plan_id/holdings", s.getHoldings)
	rg.PUT("/plans/:plan_id/holdings", s.updateHoldings)
	rg.POST("/plans/:plan_id/holding-region-changes/preview", s.previewHoldingRegionChange)
	rg.POST("/plans/:plan_id/holding-region-changes/apply", s.applyHoldingRegionChange)
	rg.GET("/plans/:plan_id/targets", s.getTargets)
	rg.GET("/plans/:plan_id/rebalance", s.getRebalance)
	rg.POST("/plans/:plan_id/portfolio-snapshots", s.createPortfolioSnapshot)
	rg.POST("/plans/:plan_id/apply-scenario", s.applyScenario)
	rg.GET("/plans/:plan_id/dashboard", s.getDashboard)
	rg.POST("/plans/:plan_id/asset-refresh", s.submitAssetRefresh)
	s.registerRebalanceExecutionRoutes(rg)
}

func (s Services) submitAssetRefresh(c *gin.Context) {
	var req service.AssetRefreshRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.AssetRefresh.Submit(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
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
	type planListItem struct {
		service.PlanDetail
		RebalanceActionableCount int   `json:"rebalance_actionable_count"`
		HoldingsGapMinor         int64 `json:"holdings_gap_minor"`
	}
	items := make([]planListItem, 0, len(out))
	for _, plan := range out {
		item := planListItem{PlanDetail: plan}
		if summary, summaryErr := s.Dashboard.GetPlanSummary(c.Request.Context(), plan.ID); summaryErr == nil {
			item.RebalanceActionableCount = summary.RebalanceActionableCount
			item.HoldingsGapMinor = summary.HoldingsGapMinor
		}
		items = append(items, item)
	}
	OK(c, items)
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
	params, err := s.Plans.GetParameters(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	identity, err := s.Assumptions.ResolveEffectiveIdentity(c.Request.Context(), params)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{
		"parameters": service.ParametersToAPI(params), "effective_assumption": identity,
	})
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
	updated, err := s.Plans.UpdateParameters(c.Request.Context(), c.Param("plan_id"),
		service.ParametersUpdateRequest{
			ConfigVersion: req.ConfigVersion, Parameters: params,
			ApplyUnallocatedToCash: req.ApplyUnallocatedToCash,
		})
	if err != nil {
		FailErr(c, err)
		return
	}
	identity, err := s.Assumptions.ResolveEffectiveIdentity(c.Request.Context(), updated)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{
		"parameters": service.ParametersToAPI(updated), "effective_assumption": identity,
	})
}

// planSettingsUpdateAPIRequest is the JSON body of PUT /plans/:plan_id/settings.
// Parameters use the API DTO (string seed) and are converted before the
// service call.
type planSettingsUpdateAPIRequest struct {
	ConfigVersion          int                                  `json:"config_version"`
	Plan                   *service.PlanSettingsPlanPatch       `json:"plan,omitempty"`
	Allocation             *service.PlanSettingsAllocationPatch `json:"allocation,omitempty"`
	Parameters             service.PlanParametersAPI            `json:"parameters"`
	ApplyUnallocatedToCash bool                                 `json:"apply_unallocated_to_cash,omitempty"`
}

func (s Services) updatePlanSettings(c *gin.Context) {
	var req planSettingsUpdateAPIRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	params, err := service.ParametersFromAPI(req.Parameters)
	if err != nil {
		Fail(c, http.StatusBadRequest, "parameters_invalid", err.Error(), nil)
		return
	}
	out, err := s.Plans.UpdateSettings(c.Request.Context(), c.Param("plan_id"),
		service.PlanSettingsUpdateRequest{
			ConfigVersion:          req.ConfigVersion,
			Plan:                   req.Plan,
			Allocation:             req.Allocation,
			Parameters:             params,
			ApplyUnallocatedToCash: req.ApplyUnallocatedToCash,
		})
	if err != nil {
		FailErr(c, err)
		return
	}
	identity, err := s.Assumptions.ResolveEffectiveIdentity(c.Request.Context(), out.Parameters)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{
		"plan":                 out.Plan,
		"parameters":           service.ParametersToAPI(out.Parameters),
		"allocation":           out.Allocation,
		"effective_assumption": identity,
	})
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

func (s Services) previewHoldingRegionChange(c *gin.Context) {
	var req service.HoldingRegionChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Holdings.PreviewRegionChange(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) applyHoldingRegionChange(c *gin.Context) {
	var req service.HoldingRegionChangeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Holdings.ApplyRegionChange(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func checkHoldingReadOnlyFields(body []byte) error {
	var raw struct {
		Holdings []map[string]json.RawMessage `json:"holdings"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return &service.AppError{Code: "invalid_request", Message: err.Error()}
	}
	// asset_class/region are user-chosen and writable; asset metadata and
	// snapshot-derived metrics stay read-only.
	readOnly := []string{
		"name", "code", "instrument_name", "instrument_code",
		"market", "currency",
		"historical_cagr", "modeled_annual_return", "annual_volatility", "max_drawdown",
		"expense_ratio", "simulation_snapshot_id",
	}
	for _, h := range raw.Holdings {
		for _, f := range readOnly {
			if _, ok := h[f]; ok {
				return &service.AppError{
					Code:    "holding_fields_read_only",
					Message: "asset metadata, risk/return metrics and simulation_snapshot_id are read-only",
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
