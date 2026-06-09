package api

import (
	"io"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/service"
)

func (s Services) registerSimulationRoutes(rg *gin.RouterGroup) {
	rg.POST("/plans/:plan_id/simulations", s.createSimulation)
	rg.GET("/plans/:plan_id/simulations", s.listSimulations)
	rg.GET("/simulations/:run_id", s.getSimulation)
	rg.GET("/simulations/:run_id/paths", s.listSimulationPaths)
	rg.GET("/simulations/:run_id/paths/:path_no", s.getSimulationPath)
}

func (s Services) registerJobRoutes(rg *gin.RouterGroup) {
	rg.GET("/jobs/:job_id", s.getJob)
	rg.POST("/jobs/:job_id/cancel", s.cancelJob)
	rg.GET("/jobs/:job_id/events", s.jobEvents)
}

func (s Services) createSimulation(c *gin.Context) {
	var req service.CreateSimulationRequest
	if err := c.ShouldBindJSON(&req); err != nil && err != io.EOF {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	req.PlanID = c.Param("plan_id")
	req.IdempotencyKey = c.GetHeader("Idempotency-Key")
	out, err := s.Simulations.Create(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listSimulations(c *gin.Context) {
	out, err := s.Simulations.ListByPlan(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"simulations": out})
}

func (s Services) getSimulation(c *gin.Context) {
	out, err := s.Simulations.GetRun(c.Request.Context(), c.Param("run_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listSimulationPaths(c *gin.Context) {
	out, err := s.Simulations.ListPaths(c.Request.Context(), c.Param("run_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"paths": out})
}

func (s Services) getSimulationPath(c *gin.Context) {
	pathNo, err := strconv.Atoi(c.Param("path_no"))
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", "path_no must be integer", nil)
		return
	}
	out, err := s.Simulations.GetPathDetail(c.Request.Context(), c.Param("run_id"), pathNo)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getJob(c *gin.Context) {
	out, err := s.Jobs.Get(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) cancelJob(c *gin.Context) {
	out, err := s.Jobs.Cancel(c.Request.Context(), c.Param("job_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) jobEvents(c *gin.Context) {
	jobID := c.Param("job_id")
	if _, err := s.Jobs.Get(c.Request.Context(), jobID); err != nil {
		FailErr(c, err)
		return
	}
	ch, unsub := s.Jobs.EventsHub().Subscribe(jobID)
	defer unsub()

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		Fail(c, http.StatusInternalServerError, "internal_error", "streaming unsupported", nil)
		return
	}

	ctx := c.Request.Context()
	for {
		select {
		case <-ctx.Done():
			return
		case ev, open := <-ch:
			if !open {
				return
			}
			frame, err := jobs.FormatSSE(ev)
			if err != nil {
				return
			}
			if _, err := c.Writer.Write(frame); err != nil {
				return
			}
			flusher.Flush()
			if ev.Status == "succeeded" || ev.Status == "failed" || ev.Status == "canceled" {
				return
			}
		}
	}
}
