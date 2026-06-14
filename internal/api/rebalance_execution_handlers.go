package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
)

func (s Services) registerRebalanceExecutionRoutes(rg *gin.RouterGroup) {
	rg.POST("/plans/:plan_id/rebalance-executions", s.createRebalanceExecution)
	rg.GET("/plans/:plan_id/rebalance-executions", s.listRebalanceExecutions)
	rg.GET("/plans/:plan_id/rebalance-executions/active", s.getActiveRebalanceExecution)
	rg.GET("/plans/:plan_id/rebalance-executions/:execution_id", s.getRebalanceExecution)
	rg.POST("/plans/:plan_id/rebalance-executions/:execution_id/sell", s.sellRebalanceExecution)
	rg.POST("/plans/:plan_id/rebalance-executions/:execution_id/buy", s.buyRebalanceExecution)
	rg.POST("/plans/:plan_id/rebalance-executions/:execution_id/skip", s.skipRebalanceExecutionLine)
	rg.POST("/plans/:plan_id/rebalance-executions/:execution_id/notes", s.noteRebalanceExecution)
	rg.POST("/plans/:plan_id/rebalance-executions/:execution_id/complete", s.completeRebalanceExecution)
	rg.POST("/plans/:plan_id/rebalance-executions/:execution_id/cancel", s.cancelRebalanceExecution)
}

func (s Services) createRebalanceExecution(c *gin.Context) {
	var req service.CreateRebalanceExecutionRequest
	_ = c.ShouldBindJSON(&req)
	out, err := s.RebalanceExecutions.Create(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) listRebalanceExecutions(c *gin.Context) {
	out, err := s.RebalanceExecutions.List(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getActiveRebalanceExecution(c *gin.Context) {
	out, err := s.RebalanceExecutions.GetActive(c.Request.Context(), c.Param("plan_id"))
	if errors.Is(err, repository.ErrNoActiveRebalanceExecution) {
		OK(c, nil)
		return
	}
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getRebalanceExecution(c *gin.Context) {
	out, err := s.RebalanceExecutions.Get(c.Request.Context(), c.Param("plan_id"), c.Param("execution_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) sellRebalanceExecution(c *gin.Context) {
	var req service.ExecutionTradeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.RebalanceExecutions.Sell(c.Request.Context(), c.Param("plan_id"), c.Param("execution_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) buyRebalanceExecution(c *gin.Context) {
	var req service.ExecutionTradeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.RebalanceExecutions.Buy(c.Request.Context(), c.Param("plan_id"), c.Param("execution_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) skipRebalanceExecutionLine(c *gin.Context) {
	var req service.ExecutionSkipRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.RebalanceExecutions.Skip(c.Request.Context(), c.Param("plan_id"), c.Param("execution_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) noteRebalanceExecution(c *gin.Context) {
	var req service.ExecutionNoteRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.RebalanceExecutions.AddNote(c.Request.Context(), c.Param("plan_id"), c.Param("execution_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) completeRebalanceExecution(c *gin.Context) {
	var req service.CompleteRebalanceExecutionRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.RebalanceExecutions.Complete(c.Request.Context(), c.Param("plan_id"), c.Param("execution_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) cancelRebalanceExecution(c *gin.Context) {
	if err := s.RebalanceExecutions.Cancel(c.Request.Context(), c.Param("plan_id"), c.Param("execution_id")); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"canceled": true})
}
