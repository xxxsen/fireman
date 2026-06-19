package api

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/service"
)

func (s Services) registerInstrumentRoutes(rg *gin.RouterGroup) {
	rg.GET("/instruments", s.listInstruments)
	rg.POST("/instruments/resolve", s.resolveInstrument)
	rg.POST("/instruments/import-async", s.importInstrumentAsync)
	rg.GET("/instruments/:instrument_id/fetch-status", s.getInstrumentFetchStatus)
	rg.POST("/instruments/:instrument_id/retry-fetch", s.retryInstrumentFetch)
	rg.POST("/instruments/import/preview", s.previewInstrumentImport)
	rg.POST("/instruments/import", s.importInstrument)
	rg.GET("/instruments/:instrument_id", s.getInstrument)
	rg.GET("/instruments/:instrument_id/detail", s.getInstrumentDetail)
	rg.POST("/instruments/:instrument_id/refresh", s.refreshInstrument)
	rg.DELETE("/instruments/:instrument_id", s.deleteInstrument)
	rg.GET("/instruments/:instrument_id/annual-returns", s.getInstrumentAnnualReturns)
	rg.GET("/instruments/:instrument_id/return-series", s.getInstrumentReturnSeries)

	rg.GET("/plans/:plan_id/holdings/:holding_id/simulation-snapshot", s.getHoldingSimulationSnapshot)
	rg.POST("/plans/:plan_id/holdings/:holding_id/sync-simulation-snapshot", s.syncHoldingSimulationSnapshot)
}

func (s Services) listInstruments(c *gin.Context) {
	// Backward compatible: without pagination/search params, return the full list.
	if c.Query("limit") == "" && c.Query("q") == "" && c.Query("cursor") == "" &&
		c.Query("offset") == "" {
		out, err := s.Instruments.List(c.Request.Context(), c.Query("valuation_date"))
		if err != nil {
			FailErr(c, err)
			return
		}
		OK(c, gin.H{"instruments": out})
		return
	}

	opts := repository.InstrumentSearchOptions{
		Query:         c.Query("q"),
		AssetClass:    c.Query("asset_class"),
		Region:        c.Query("region"),
		Status:        c.Query("status"),
		ExcludeSystem: true,
		Limit:         atoiDefault(c.Query("limit"), 10),
		Offset:        atoiDefault(c.Query("cursor"), atoiDefault(c.Query("offset"), 0)),
	}
	if v := strings.TrimSpace(c.Query("exclude_ids")); v != "" {
		for _, id := range strings.Split(v, ",") {
			if trimmed := strings.TrimSpace(id); trimmed != "" {
				opts.ExcludeIDs = append(opts.ExcludeIDs, trimmed)
			}
		}
	}
	out, err := s.Instruments.Search(c.Request.Context(), opts)
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

func decodeInstrumentImportBody[T any](
	c *gin.Context,
	checkReadOnly func([]byte) error,
) (T, bool) {
	var zero T
	body, err := c.GetRawData()
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return zero, false
	}
	if roErr := checkReadOnly(body); roErr != nil {
		FailErr(c, roErr)
		return zero, false
	}
	var req T
	if err := json.Unmarshal(body, &req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return zero, false
	}
	return req, true
}

func (s Services) previewInstrumentImport(c *gin.Context) {
	req, ok := decodeInstrumentImportBody[service.InstrumentImportRequest](c, service.CheckInstrumentReadOnlyFields)
	if !ok {
		return
	}
	out, err := s.Instruments.Preview(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) importInstrument(c *gin.Context) {
	req, ok := decodeInstrumentImportBody[service.InstrumentImportRequest](c, service.CheckInstrumentReadOnlyFields)
	if !ok {
		return
	}
	out, err := s.Instruments.Import(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) resolveInstrument(c *gin.Context) {
	var req service.InstrumentResolveRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.Instruments.Resolve(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) importInstrumentAsync(c *gin.Context) {
	req, ok := decodeInstrumentImportBody[service.InstrumentImportAsyncRequest](c,
		service.CheckInstrumentImportAsyncFields)
	if !ok {
		return
	}
	out, err := s.Instruments.ImportAsync(c.Request.Context(), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getInstrumentFetchStatus(c *gin.Context) {
	out, err := s.Instruments.GetFetchStatus(c.Request.Context(), c.Param("instrument_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) retryInstrumentFetch(c *gin.Context) {
	out, err := s.Instruments.RetryFetch(c.Request.Context(), c.Param("instrument_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getInstrument(c *gin.Context) {
	out, err := s.Instruments.Get(c.Request.Context(), c.Param("instrument_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) getInstrumentDetail(c *gin.Context) {
	out, err := s.Instruments.GetDetail(c.Request.Context(), c.Param("instrument_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) refreshInstrument(c *gin.Context) {
	var req service.InstrumentRefreshOptions
	if err := c.ShouldBindJSON(&req); err != nil && !errors.Is(err, io.EOF) {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if c.Query("force") == "true" || c.Query("force") == "1" {
		req.Force = true
	}
	out, err := s.Instruments.Refresh(c.Request.Context(), c.Param("instrument_id"), req)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) deleteInstrument(c *gin.Context) {
	if err := s.Instruments.Delete(c.Request.Context(), c.Param("instrument_id")); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"deleted": true})
}

func (s Services) getInstrumentAnnualReturns(c *gin.Context) {
	inclusion := c.Query("inclusion_date")
	out, err := s.Instruments.AnnualReturns(c.Request.Context(), c.Param("instrument_id"), inclusion)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"annual_returns": out})
}

func (s Services) getInstrumentReturnSeries(c *gin.Context) {
	rangeKey := c.Query("range")
	if rangeKey == "" {
		rangeKey = "3m"
	}
	out, err := s.Instruments.ReturnSeries(c.Request.Context(), c.Param("instrument_id"), rangeKey)
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
