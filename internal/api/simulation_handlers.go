package api

import (
	"errors"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
	taskcore "github.com/fireman/fireman/internal/task"
)

var taskSSEKeepaliveInterval = 15 * time.Second

func (s Services) registerSimulationRoutes(rg *gin.RouterGroup) {
	rg.POST("/plans/:plan_id/simulations", s.createSimulation)
	rg.GET("/plans/:plan_id/simulations", s.listSimulations)
	rg.GET("/plans/:plan_id/simulation-readiness", s.getSimulationReadiness)
	rg.POST("/plans/:plan_id/sync-missing-asset-history", s.syncMissingAssetHistory)
	rg.GET("/plans/:plan_id/simulations/:run_id/scenario-comparison", s.compareScenarios)
	rg.GET("/plans/:plan_id/return-overrides", s.listReturnOverrides)
	rg.PUT("/plans/:plan_id/return-overrides/:asset_key", s.setReturnOverride)
	rg.DELETE("/plans/:plan_id/return-overrides/:asset_key", s.deleteReturnOverride)
	rg.GET("/simulations/:run_id", s.getSimulation)
	rg.GET("/simulations/:run_id/paths", s.listSimulationPaths)
	rg.GET("/simulations/:run_id/paths/:path_no", s.getSimulationPath)

	rg.GET("/plans/:plan_id/holdings/:holding_id/simulation-snapshot", s.getHoldingSimulationSnapshot)
	rg.POST("/plans/:plan_id/holdings/:holding_id/sync-simulation-snapshot", s.syncHoldingSimulationSnapshot)
}

func (s Services) getSimulationReadiness(c *gin.Context) {
	out, err := s.SimulationReadiness.Check(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) syncMissingAssetHistory(c *gin.Context) {
	out, err := s.SimulationReadiness.SyncMissingHistory(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getHoldingSimulationSnapshot(c *gin.Context) {
	out, err := s.HoldingSnapshots.Get(c.Request.Context(), c.Param("plan_id"), c.Param("holding_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) syncHoldingSimulationSnapshot(c *gin.Context) {
	var req service.SyncSnapshotRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.HoldingSnapshots.Sync(c.Request.Context(), c.Param("plan_id"), c.Param("holding_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) registerTaskRoutes(rg *gin.RouterGroup) {
	rg.GET("/tasks", s.listTasks)
	rg.GET("/tasks/:task_id", s.getTask)
	rg.POST("/tasks/:task_id/cancel", s.cancelTask)
	rg.GET("/tasks/:task_id/events", s.taskEvents)
}

func (s Services) listTasks(c *gin.Context) {
	items, total, err := s.Tasks.List(c.Request.Context(), service.TaskListParams{
		WorkerType: c.Query("worker_type"), Type: c.Query("type"), Status: c.Query("status"),
		ScopeType: c.Query("scope_type"), ScopeID: c.Query("scope_id"), Query: c.Query("q"),
		Limit: atoiDefault(c.Query("limit"), 20), Offset: atoiDefault(c.Query("offset"), 0),
	})
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"items": items, "total": total})
}

func (s Services) createSimulation(c *gin.Context) {
	var req service.CreateSimulationRequest
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
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

func (s Services) compareScenarios(c *gin.Context) {
	out, err := s.Simulations.CompareScenarios(
		c.Request.Context(), c.Param("plan_id"), c.Param("run_id"),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listReturnOverrides(c *gin.Context) {
	out, err := s.Simulations.ListReturnOverrides(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"overrides": out})
}

func (s Services) setReturnOverride(c *gin.Context) {
	var req service.SetReturnOverrideRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Simulations.SetReturnOverride(
		c.Request.Context(), c.Param("plan_id"), c.Param("asset_key"), req,
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) deleteReturnOverride(c *gin.Context) {
	err := s.Simulations.DeleteReturnOverride(
		c.Request.Context(), c.Param("plan_id"), c.Param("asset_key"),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"deleted": true})
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

func (s Services) getTask(c *gin.Context) {
	out, err := s.Tasks.Get(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) cancelTask(c *gin.Context) {
	out, err := s.Tasks.Cancel(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) taskEvents(c *gin.Context) {
	taskID := c.Param("task_id")
	flusher, ok := c.Writer.(http.Flusher)
	if !ok {
		Fail(c, http.StatusInternalServerError, "internal_error", "streaming unsupported", nil)
		return
	}

	// Subscribe before reading the persisted snapshot. A state transition in
	// between may produce a duplicate event, but cannot disappear entirely.
	ch, unsub := s.Tasks.EventsHub().Subscribe(taskID)
	defer unsub()
	current, err := s.Tasks.Get(c.Request.Context(), taskID)
	if err != nil {
		FailErr(c, err)
		return
	}

	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Status(http.StatusOK)

	initial := taskcore.Event{
		TaskID: current.ID, Status: current.Status, Phase: current.Phase,
		ProgressCurrent: current.ProgressCurrent, ProgressTotal: current.ProgressTotal,
		AttemptCount: current.AttemptCount, ErrorCode: current.ErrorCode,
		ErrorMessage: current.ErrorMessage, ResultKey: current.ResultKey,
	}
	if !writeTaskEvent(c, flusher, initial) || repository.IsTerminalWorkerTaskStatus(initial.Status) {
		return
	}

	ctx := c.Request.Context()
	keepalive := time.NewTicker(taskSSEKeepaliveInterval)
	defer keepalive.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-keepalive.C:
			if _, err := c.Writer.Write([]byte(": keepalive\n\n")); err != nil {
				return
			}
			flusher.Flush()
		case ev, open := <-ch:
			if !open {
				return
			}
			if !writeTaskEvent(c, flusher, ev) || repository.IsTerminalWorkerTaskStatus(ev.Status) {
				return
			}
		}
	}
}

func writeTaskEvent(c *gin.Context, flusher http.Flusher, event taskcore.Event) bool {
	frame, err := taskcore.FormatSSE(event)
	if err != nil {
		return false
	}
	if _, err := c.Writer.Write(frame); err != nil {
		return false
	}
	flusher.Flush()
	return true
}
