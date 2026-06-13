package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
)

func (s Services) registerRebalanceDraftRoutes(rg *gin.RouterGroup) {
	rg.POST("/plans/:plan_id/rebalance-drafts", s.createRebalanceDraft)
	rg.GET("/plans/:plan_id/rebalance-drafts/active", s.getActiveRebalanceDraft)
	rg.GET("/plans/:plan_id/rebalance-drafts/:draft_id", s.getRebalanceDraft)
	rg.PATCH("/plans/:plan_id/rebalance-drafts/:draft_id/lines", s.patchRebalanceDraftLines)
	rg.POST("/plans/:plan_id/rebalance-drafts/:draft_id/undo", s.undoRebalanceDraft)
	rg.POST("/plans/:plan_id/rebalance-drafts/:draft_id/commit", s.commitRebalanceDraft)
	rg.DELETE("/plans/:plan_id/rebalance-drafts/:draft_id", s.cancelRebalanceDraft)
	rg.POST("/plans/:plan_id/asset-refresh", s.submitAssetRefresh)
}

func (s Services) createRebalanceDraft(c *gin.Context) {
	var req service.CreateRebalanceDraftRequest
	_ = c.ShouldBindJSON(&req)
	out, err := s.RebalanceDrafts.Create(c.Request.Context(), c.Param("plan_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getActiveRebalanceDraft(c *gin.Context) {
	out, err := s.RebalanceDrafts.GetActive(c.Request.Context(), c.Param("plan_id"))
	if errors.Is(err, repository.ErrNoActiveRebalanceDraft) {
		OK(c, nil)
		return
	}
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getRebalanceDraft(c *gin.Context) {
	out, err := s.RebalanceDrafts.Get(c.Request.Context(), c.Param("plan_id"), c.Param("draft_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) patchRebalanceDraftLines(c *gin.Context) {
	var req service.PatchRebalanceDraftLinesRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.RebalanceDrafts.PatchLines(c.Request.Context(), c.Param("plan_id"), c.Param("draft_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) undoRebalanceDraft(c *gin.Context) {
	out, err := s.RebalanceDrafts.Undo(c.Request.Context(), c.Param("plan_id"), c.Param("draft_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) commitRebalanceDraft(c *gin.Context) {
	var req service.CommitRebalanceDraftRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.RebalanceDrafts.Commit(c.Request.Context(), c.Param("plan_id"), c.Param("draft_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) cancelRebalanceDraft(c *gin.Context) {
	if err := s.RebalanceDrafts.Cancel(c.Request.Context(), c.Param("plan_id"), c.Param("draft_id")); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"canceled": true})
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
