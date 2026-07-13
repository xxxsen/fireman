package api

import (
	"net/http"

	"github.com/fireman/fireman/internal/service"
	"github.com/gin-gonic/gin"
)

func (s Services) registerFrontierRoutes(rg *gin.RouterGroup) {
	rg.POST("/plans/:plan_id/fire-frontier-readiness", s.fireFrontierReadiness)
	rg.POST("/plans/:plan_id/fire-frontier-runs", s.createFireFrontierRun)
	rg.GET("/plans/:plan_id/fire-frontier-runs", s.listFireFrontierRuns)
	rg.GET("/fire-frontier-runs/:run_id", s.getFireFrontierRun)
	rg.POST("/fire-frontier-runs/:run_id/points/:point_id/preview", s.previewFireFrontierPoint)
	rg.POST("/fire-frontier-runs/:run_id/points/:point_id/apply", s.applyFireFrontierPoint)
}

func (s Services) fireFrontierReadiness(c *gin.Context) {
	var req service.FireFrontierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Frontiers.Readiness(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) createFireFrontierRun(c *gin.Context) {
	var req service.FireFrontierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	req.IdempotencyKey = c.GetHeader("Idempotency-Key")
	req.RequestID = requestID(c)
	out, err := s.Frontiers.Create(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	if out.Reused {
		OK(c, out)
		return
	}
	Accepted(c, out)
}

func (s Services) listFireFrontierRuns(c *gin.Context) {
	limit, offset := atoiDefault(c.Query("limit"), 20), atoiDefault(c.Query("offset"), 0)
	items, total, err := s.Frontiers.List(c.Request.Context(), c.Param("plan_id"), limit, offset)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"runs": items, "total": total, "limit": limit, "offset": offset})
}

func (s Services) getFireFrontierRun(c *gin.Context) {
	out, err := s.Frontiers.Get(c.Request.Context(), c.Param("run_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) previewFireFrontierPoint(c *gin.Context) {
	var req service.PreviewFrontierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Frontiers.Preview(c.Request.Context(), c.Param("run_id"), c.Param("point_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) applyFireFrontierPoint(c *gin.Context) {
	var req service.ApplyFrontierRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Frontiers.Apply(c.Request.Context(), c.Param("run_id"), c.Param("point_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}
