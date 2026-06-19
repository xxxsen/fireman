package api

import (
	"errors"
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/service"
)

func (s Services) registerAnalysisRoutes(rg *gin.RouterGroup) {
	rg.POST("/plans/:plan_id/stress-tests", s.createStressTest)
	rg.GET("/plans/:plan_id/stress-tests", s.listStressTests)
	rg.GET("/stress-tests/:job_id", s.getStressTest)

	rg.POST("/plans/:plan_id/sensitivity-tests", s.createSensitivityTest)
	rg.GET("/plans/:plan_id/sensitivity-tests", s.listSensitivityTests)
	rg.GET("/sensitivity-tests/:job_id", s.getSensitivityTest)
}

func (s Services) createStressTest(c *gin.Context) {
	var req service.CreateStressTestRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	req.PlanID = c.Param("plan_id")
	req.IdempotencyKey = c.GetHeader("Idempotency-Key")
	out, err := s.Stress.Create(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listStressTests(c *gin.Context) {
	planID := c.Param("plan_id")
	if runID := c.Query("simulation_run_id"); runID != "" {
		out, err := s.Stress.ListByRun(c.Request.Context(), planID, runID)
		if err != nil {
			FailErr(c, err)
			return
		}
		OK(c, gin.H{"stress_tests": out})
		return
	}
	out, err := s.Stress.ListByPlan(c.Request.Context(), planID)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"stress_tests": out})
}

func (s Services) getStressTest(c *gin.Context) {
	out, err := s.Stress.GetByJobID(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) createSensitivityTest(c *gin.Context) {
	var req service.CreateSensitivityTestRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	req.PlanID = c.Param("plan_id")
	req.IdempotencyKey = c.GetHeader("Idempotency-Key")
	out, err := s.Sensitivity.Create(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listSensitivityTests(c *gin.Context) {
	planID := c.Param("plan_id")
	if runID := c.Query("simulation_run_id"); runID != "" {
		out, err := s.Sensitivity.ListByRun(c.Request.Context(), planID, runID)
		if err != nil {
			FailErr(c, err)
			return
		}
		OK(c, gin.H{"sensitivity_tests": out})
		return
	}
	out, err := s.Sensitivity.ListByPlan(c.Request.Context(), planID)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"sensitivity_tests": out})
}

func (s Services) getSensitivityTest(c *gin.Context) {
	out, err := s.Sensitivity.GetByJobID(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}
