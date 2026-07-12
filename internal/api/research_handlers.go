package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/service"
)

// registerResearchRoutes mounts the portfolio research module under
// /api/v1/research (td/099 §5.3).
func (s Services) registerResearchRoutes(rg *gin.RouterGroup) {
	research := rg.Group("/research")

	research.GET("/assets", s.listResearchAssets)

	research.GET("/collections", s.listResearchCollections)
	research.POST("/collections", s.createResearchCollection)
	research.GET("/collections/:collection_id", s.getResearchCollection)
	research.PATCH("/collections/:collection_id", s.updateResearchCollection)
	research.DELETE("/collections/:collection_id", s.deleteResearchCollection)

	research.POST("/collections/:collection_id/items", s.addResearchItem)
	research.PATCH("/collections/:collection_id/items/:item_id", s.updateResearchItem)
	research.DELETE("/collections/:collection_id/items/:item_id", s.deleteResearchItem)
	research.POST("/collections/:collection_id/normalize-weights", s.normalizeResearchWeights)

	research.GET("/collections/:collection_id/readiness", s.getResearchReadiness)
	research.POST("/collections/:collection_id/sync-history", s.syncResearchHistory)

	research.POST("/collections/:collection_id/backtests", s.createResearchBacktest)
	research.GET("/collections/:collection_id/runs", s.listResearchRuns)
	research.GET("/runs", s.listRecentResearchRuns)
	research.GET("/runs/:run_id", s.getResearchRun)
	research.GET("/runs/:run_id/points", s.getResearchRunPoints)
	research.GET("/runs/:run_id/export.csv", s.exportResearchRunCSV)

	research.POST("/collections/:collection_id/plan-preview", s.previewResearchPlanReplacement)
	research.POST("/collections/:collection_id/apply-to-plan", s.applyResearchPlanReplacement)
	research.POST("/collections/:collection_id/copy-to-plan", s.deprecatedCopyResearchToPlan)

	research.GET("/collections/:collection_id/optimization-readiness", s.getOptimizationReadiness)
	research.POST("/collections/:collection_id/optimizations", s.createOptimization)
	research.GET("/collections/:collection_id/optimizations/latest", s.getLatestOptimization)
	research.GET("/optimizations/:optimization_id", s.getOptimization)
	research.POST("/optimizations/:optimization_id/apply", s.applyOptimization)
}

// --- screener ---

func csvQueryList(raw string) []string {
	var out []string
	for _, part := range strings.Split(raw, ",") {
		if trimmed := strings.TrimSpace(part); trimmed != "" {
			out = append(out, trimmed)
		}
	}
	return out
}

// floatQueryPtr parses an optional float query parameter; ok=false means the
// value was present but invalid.
func floatQueryPtr(c *gin.Context, name string) (*float64, bool) {
	raw := strings.TrimSpace(c.Query(name))
	if raw == "" {
		return nil, true
	}
	v, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", name+" must be a number", nil)
		return nil, false
	}
	return &v, true
}

func (s Services) listResearchAssets(c *gin.Context) {
	params := service.ResearchAssetListParams{
		Market:          strings.TrimSpace(c.Query("market")),
		InstrumentTypes: csvQueryList(c.Query("instrument_types")),
		Query:           c.Query("q"),
		Currencies:      csvQueryList(c.Query("currencies")),
		IncludeInactive: c.Query("include_inactive") == "true",
		HistoryStatus:   strings.TrimSpace(c.Query("history_status")),
		DataAsOfMin:     strings.TrimSpace(c.Query("data_as_of_min")),
		BacktestReady:   c.Query("backtest_ready") == "true",
		SortBy:          strings.TrimSpace(c.Query("sort_by")),
		SortDesc:        c.Query("sort_desc") == "true",
		Limit:           atoiDefault(c.Query("limit"), 50),
		Offset:          atoiDefault(c.Query("offset"), 0),
	}
	if v, ok := floatQueryPtr(c, "min_history_years"); !ok {
		return
	} else if v != nil {
		params.MinHistoryYears = *v
	}
	bounds := []struct {
		name string
		dst  **float64
	}{
		{"min_cagr", &params.MinCAGR},
		{"min_return_1y", &params.MinReturn1Y},
		{"min_return_3y", &params.MinReturn3Y},
		{"min_return_5y", &params.MinReturn5Y},
		{"max_volatility", &params.MaxVolatility},
		{"min_max_drawdown", &params.MinMaxDrawdown},
		{"min_sharpe", &params.MinSharpe},
		{"min_calmar", &params.MinCalmar},
		{"max_downside_volatility", &params.MaxDownsideVolatility},
		{"min_return_drawdown", &params.MinReturnDrawdownRatio},
	}
	for _, b := range bounds {
		v, ok := floatQueryPtr(c, b.name)
		if !ok {
			return
		}
		*b.dst = v
	}
	out, err := s.Research.ListResearchAssets(c.Request.Context(), params)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

// --- collections ---

func (s Services) listResearchCollections(c *gin.Context) {
	out, err := s.Research.ListCollections(c.Request.Context(), strings.TrimSpace(c.Query("status")))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"collections": out})
}

func (s Services) createResearchCollection(c *gin.Context) {
	var req service.ResearchCollectionInput
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Research.CreateCollection(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getResearchCollection(c *gin.Context) {
	out, err := s.Research.GetCollection(c.Request.Context(), c.Param("collection_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) updateResearchCollection(c *gin.Context) {
	var req service.ResearchCollectionUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Research.UpdateCollection(c.Request.Context(), c.Param("collection_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) deleteResearchCollection(c *gin.Context) {
	hard := c.Query("hard") == "true"
	if err := s.Research.DeleteCollection(c.Request.Context(), c.Param("collection_id"), hard); err != nil {
		FailErr(c, err)
		return
	}
	if hard {
		OK(c, gin.H{"deleted": true})
		return
	}
	OK(c, gin.H{"archived": true})
}

// --- items ---

func (s Services) addResearchItem(c *gin.Context) {
	var req service.ResearchCollectionItemInput
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Research.AddItem(c.Request.Context(), c.Param("collection_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) updateResearchItem(c *gin.Context) {
	var req service.ResearchItemUpdate
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Research.UpdateItem(
		c.Request.Context(), c.Param("collection_id"), c.Param("item_id"), req,
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) deleteResearchItem(c *gin.Context) {
	out, err := s.Research.DeleteItem(
		c.Request.Context(), c.Param("collection_id"), c.Param("item_id"),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) normalizeResearchWeights(c *gin.Context) {
	out, err := s.Research.NormalizeWeights(c.Request.Context(), c.Param("collection_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

// --- readiness & sync ---

func (s Services) getResearchReadiness(c *gin.Context) {
	out, err := s.Research.GetReadiness(c.Request.Context(), c.Param("collection_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) syncResearchHistory(c *gin.Context) {
	var req service.ResearchSyncRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
	}
	out, err := s.Research.SyncCollectionHistory(c.Request.Context(), c.Param("collection_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

// --- backtests & runs ---

func (s Services) createResearchBacktest(c *gin.Context) {
	out, err := s.Research.CreateBacktest(c.Request.Context(), c.Param("collection_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listResearchRuns(c *gin.Context) {
	out, err := s.Research.ListRuns(
		c.Request.Context(), c.Param("collection_id"), atoiDefault(c.Query("limit"), 20),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"runs": out})
}

func (s Services) listRecentResearchRuns(c *gin.Context) {
	out, err := s.Research.ListRecentRuns(c.Request.Context(), atoiDefault(c.Query("limit"), 10))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"runs": out})
}

func (s Services) getResearchRun(c *gin.Context) {
	out, err := s.Research.GetRun(c.Request.Context(), c.Param("run_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getResearchRunPoints(c *gin.Context) {
	params := service.ResearchPointsParams{
		From:           strings.TrimSpace(c.Query("from")),
		To:             strings.TrimSpace(c.Query("to")),
		Limit:          atoiDefault(c.Query("limit"), 0),
		Offset:         atoiDefault(c.Query("offset"), 0),
		IncludeWeights: c.Query("include_weights") == "true",
	}
	out, err := s.Research.GetRunPoints(c.Request.Context(), c.Param("run_id"), params)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) exportResearchRunCSV(c *gin.Context) {
	csv, filename, err := s.Research.ExportRunCSV(c.Request.Context(), c.Param("run_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	c.Header("Content-Disposition", `attachment; filename="`+filename+`"`)
	c.Data(http.StatusOK, "text/csv; charset=utf-8", []byte(csv))
}

// --- plan interop ---

// --- optimization ---

func (s Services) getOptimizationReadiness(c *gin.Context) {
	weightStep := 0.05
	if raw := strings.TrimSpace(c.Query("weight_step")); raw != "" {
		v, err := strconv.ParseFloat(raw, 64)
		if err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", "weight_step must be a number", nil)
			return
		}
		weightStep = v
	}
	confidence, ok := floatQueryPtr(c, "cvar_confidence")
	if !ok {
		return
	}
	var horizonDays *int
	if raw := strings.TrimSpace(c.Query("cvar_horizon_days")); raw != "" {
		v, err := strconv.Atoi(raw)
		if err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", "cvar_horizon_days must be an integer", nil)
			return
		}
		horizonDays = &v
	}
	out, err := s.Research.GetOptimizationReadiness(
		c.Request.Context(), c.Param("collection_id"), service.OptimizationReadinessRequest{
			WeightStep: weightStep, Confidence: confidence, HorizonDays: horizonDays,
		},
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) createOptimization(c *gin.Context) {
	var req service.ResearchOptimizationRequest
	if c.Request.ContentLength > 0 {
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
	}
	out, err := s.Research.CreateOptimization(
		c.Request.Context(), c.Param("collection_id"), req,
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getLatestOptimization(c *gin.Context) {
	out, found, err := s.Research.GetLatestOptimization(
		c.Request.Context(), c.Param("collection_id"),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	if !found {
		OK(c, nil)
		return
	}
	OK(c, &out)
}

func (s Services) getOptimization(c *gin.Context) {
	out, err := s.Research.GetOptimization(
		c.Request.Context(), c.Param("optimization_id"),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) applyOptimization(c *gin.Context) {
	var req service.ResearchOptimizationApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Research.ApplyOptimization(c.Request.Context(), c.Param("optimization_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

// --- plan interop ---

func (s Services) previewResearchPlanReplacement(c *gin.Context) {
	var req service.ResearchPlanPreviewRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Research.PreviewPlanReplacement(c.Request.Context(), c.Param("collection_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) applyResearchPlanReplacement(c *gin.Context) {
	var req service.ResearchPlanApplyRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Research.ApplyPlanReplacement(c.Request.Context(), c.Param("collection_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) deprecatedCopyResearchToPlan(c *gin.Context) {
	Fail(c, http.StatusGone, "copy_to_plan_deprecated",
		"use plan-preview followed by apply-to-plan", nil)
}
