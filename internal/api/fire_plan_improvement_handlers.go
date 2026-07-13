package api

import (
	"net/http"

	"github.com/fireman/fireman/internal/service"
	"github.com/gin-gonic/gin"
)

func (s Services) registerImprovementRoutes(rg *gin.RouterGroup) {
	rg.GET("/plans/:plan_id/improvement-readiness", s.getImprovementReadiness)
	rg.POST("/plans/:plan_id/improvement-runs", s.createImprovementRun)
	rg.GET("/plans/:plan_id/improvement-runs", s.listImprovementRuns)
	rg.GET("/improvement-runs/:run_id", s.getImprovementRun)
	rg.POST("/improvement-runs/:run_id/proposals/:proposal_id/preview", s.previewImprovementProposal)
	rg.POST("/improvement-runs/:run_id/proposals/:proposal_id/apply", s.applyImprovementProposal)
}

func (s Services) getImprovementReadiness(c *gin.Context) {
	out, err := s.Improvements.Readiness(c.Request.Context(), c.Param("plan_id"), c.Query("simulation_run_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) createImprovementRun(c *gin.Context) {
	var req service.CreateImprovementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	req.IdempotencyKey = c.GetHeader("Idempotency-Key")
	out, err := s.Improvements.Create(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listImprovementRuns(c *gin.Context) {
	limit, offset := atoiDefault(c.Query("limit"), 20), atoiDefault(c.Query("offset"), 0)
	items, total, err := s.Improvements.List(c.Request.Context(), c.Param("plan_id"), limit, offset)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"runs": items, "total": total, "limit": limit, "offset": offset})
}

func (s Services) getImprovementRun(c *gin.Context) {
	out, err := s.Improvements.Get(c.Request.Context(), c.Param("run_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) previewImprovementProposal(c *gin.Context) {
	var req service.PreviewImprovementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Improvements.Preview(c.Request.Context(), c.Param("run_id"), c.Param("proposal_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) applyImprovementProposal(c *gin.Context) {
	var req service.ApplyImprovementRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Improvements.Apply(c.Request.Context(), c.Param("run_id"), c.Param("proposal_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}
