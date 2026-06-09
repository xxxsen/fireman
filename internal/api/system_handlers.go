package api

import (
	"io"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/service"
)

func (s Services) registerSystemRoutes(rg *gin.RouterGroup) {
	rg.GET("/system/backup", s.downloadBackup)
	rg.POST("/system/restore", s.restoreBackup)
	rg.GET("/plans/:plan_id/export/json", s.exportPlanJSON)
	rg.GET("/plans/:plan_id/export/targets.csv", s.exportTargetsCSV)
	rg.GET("/plans/:plan_id/export/rebalance.csv", s.exportRebalanceCSV)
}

func (s Services) downloadBackup(c *gin.Context) {
	data, name, err := s.System.DownloadBackup(c.Request.Context())
	if err != nil {
		FailErr(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename="+name)
	c.Data(http.StatusOK, "application/octet-stream", data)
}

func (s Services) restoreBackup(c *gin.Context) {
	body, err := io.ReadAll(io.LimitReader(c.Request.Body, 100<<20+1))
	if err != nil {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	if len(body) > 100<<20 {
		Fail(c, http.StatusBadRequest, "invalid_request", "backup file too large", nil)
		return
	}
	if err := s.System.RestoreBackup(c.Request.Context(), body); err != nil {
		FailErr(c, err)
		return
	}
	OK(c, gin.H{"restored": true, "restart_required": true})
}

func (s Services) exportPlanJSON(c *gin.Context) {
	out, err := s.System.ExportPlanJSON(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	data, err := service.MarshalPlanExport(out)
	if err != nil {
		Fail(c, http.StatusInternalServerError, "export_failed", err.Error(), nil)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=plan-"+c.Param("plan_id")+".json")
	c.Data(http.StatusOK, "application/json", data)
}

func (s Services) exportTargetsCSV(c *gin.Context) {
	data, err := s.System.ExportTargetsCSV(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=targets-"+c.Param("plan_id")+".csv")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}

func (s Services) exportRebalanceCSV(c *gin.Context) {
	data, err := s.System.ExportRebalanceCSV(c.Request.Context(), c.Param("plan_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	c.Header("Content-Disposition", "attachment; filename=rebalance-"+c.Param("plan_id")+".csv")
	c.Data(http.StatusOK, "text/csv; charset=utf-8", data)
}
