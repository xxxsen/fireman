package api

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/service"
)

func (s Services) registerMarketAssetRoutes(rg *gin.RouterGroup) {
	rg.GET("/market-assets", s.listMarketAssets)
	rg.GET("/market-assets/by-key", s.getMarketAssetByKey)
	rg.POST("/market-assets/sync", s.syncMarketAssets)
	rg.POST("/market-assets/history-sync", s.syncMarketAssetHistory)
	rg.POST("/market-assets/fx-sync", s.syncFXRates)
	rg.GET("/tasks/:task_id", s.getWorkerTask)
}

func (s Services) listMarketAssets(c *gin.Context) {
	params := service.MarketAssetListParams{
		Market:          c.Query("market"),
		SymbolQuery:     c.Query("symbol_q"),
		NameQuery:       c.Query("name_q"),
		IncludeInactive: c.Query("include_inactive") == "true",
		Limit:           atoiDefault(c.Query("limit"), 50),
		Offset:          atoiDefault(c.Query("offset"), 0),
	}
	if v := strings.TrimSpace(c.Query("instrument_types")); v != "" {
		for _, t := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(t); trimmed != "" {
				params.InstrumentTypes = append(params.InstrumentTypes, trimmed)
			}
		}
	}
	out, err := s.MarketAssets.ListAssets(c.Request.Context(), params)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getMarketAssetByKey(c *gin.Context) {
	out, err := s.MarketAssets.GetDetail(
		c.Request.Context(),
		c.Query("asset_key"),
		c.Query("adjust_policy"),
		c.Query("point_type"),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) syncMarketAssets(c *gin.Context) {
	var req service.DirectorySyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.MarketAssets.SyncDirectory(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) syncMarketAssetHistory(c *gin.Context) {
	var req service.HistorySyncRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.MarketAssets.SyncHistory(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) syncFXRates(c *gin.Context) {
	out, err := s.MarketAssets.SyncFXRates(c.Request.Context())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getWorkerTask(c *gin.Context) {
	out, err := s.MarketAssets.GetTask(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
