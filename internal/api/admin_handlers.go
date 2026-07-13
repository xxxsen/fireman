package api

import (
	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/service"
)

// registerAdminRoutes mounts the administration namespace. Handlers do
// parameter parsing and envelopes only; task cancellation is delegated to the
// same service used by business pages. The endpoints share one Group so a
// future auth middleware mounts in a single place.
func (s Services) registerAdminRoutes(rg *gin.RouterGroup) {
	admin := rg.Group("/admin")
	admin.GET("/overview", s.adminOverview)
	admin.GET("/worker-tasks", s.adminListWorkerTasks)
	admin.GET("/worker-tasks/:task_id", s.adminWorkerTaskDetail)
	admin.POST("/worker-tasks/:task_id/cancel", s.adminCancelWorkerTask)
	admin.GET("/finalize-records", s.adminListFinalizeRecords)
	admin.GET("/data-versions", s.adminListDataVersions)
	admin.GET("/auto-updates", s.adminListAutoUpdates)
	admin.GET("/auto-updates/directories", s.adminListAutoUpdateDirectories)
	admin.POST("/auto-updates/directories", s.adminCreateDirectoryAutoUpdate)
	admin.PUT("/auto-updates/:id", s.adminUpdateAutoUpdate)
}

func (s Services) adminListAutoUpdateDirectories(c *gin.Context) {
	OK(c, s.AutoUpdates.DirectoryUnits())
}

func (s Services) adminListAutoUpdates(c *gin.Context) {
	out, err := s.AutoUpdates.List(c.Request.Context(), service.AutoUpdateListParams{
		TargetType: c.Query("target_type"),
		Enabled:    c.Query("enabled"),
		Query:      c.Query("q"),
		Limit:      atoiDefault(c.Query("limit"), 50),
		Offset:     atoiDefault(c.Query("offset"), 0),
	})
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminCreateDirectoryAutoUpdate(c *gin.Context) {
	var req struct {
		SyncKey       string `json:"sync_key"`
		IntervalHours int    `json:"interval_hours"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.AutoUpdates.CreateDirectory(c.Request.Context(), req.SyncKey, req.IntervalHours)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminUpdateAutoUpdate(c *gin.Context) {
	var req struct {
		Enabled       bool  `json:"enabled"`
		IntervalHours int   `json:"interval_hours"`
		Version       int64 `json:"version"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, 400, "invalid_request", err.Error(), nil)
		return
	}
	out, err := s.AutoUpdates.Update(c.Request.Context(), c.Param("id"), req.Version, req.Enabled, req.IntervalHours)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminOverview(c *gin.Context) {
	out, err := s.Admin.Overview(c.Request.Context())
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminListWorkerTasks(c *gin.Context) {
	out, err := s.Admin.ListWorkerTasks(c.Request.Context(), service.AdminWorkerTaskListParams{
		WorkerType: c.Query("worker_type"),
		Type:       c.Query("type"), Status: c.Query("status"),
		ScopeType: c.Query("scope_type"), ScopeID: c.Query("scope_id"),
		Query: c.Query("q"), Limit: atoiDefault(c.Query("limit"), 20),
		Offset: atoiDefault(c.Query("offset"), 0),
	})
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminWorkerTaskDetail(c *gin.Context) {
	out, err := s.Admin.GetWorkerTaskDetail(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminCancelWorkerTask(c *gin.Context) {
	out, err := s.Tasks.CancelAdmin(c.Request.Context(), c.Param("task_id"))
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminListFinalizeRecords(c *gin.Context) {
	out, err := s.Admin.ListFinalizeRecords(c.Request.Context(), service.AdminFinalizeRecordParams{
		TaskID:   c.Query("task_id"),
		Result:   c.Query("result"),
		TaskType: c.Query("task_type"),
		Limit:    atoiDefault(c.Query("limit"), 20),
		Offset:   atoiDefault(c.Query("offset"), 0),
	})
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminListDataVersions(c *gin.Context) {
	out, err := s.Admin.ListDataVersions(
		c.Request.Context(),
		c.Query("prefix"),
		atoiDefault(c.Query("limit"), 20),
		atoiDefault(c.Query("offset"), 0),
	)
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}
