package api

import (
	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/service"
)

// registerAdminRoutes mounts the read-only observation namespace. Handlers do
// parameter parsing and envelopes only; every derived semantic (stale,
// duration, timeline) lives in AdminService. The endpoints share one Group so
// a future auth middleware mounts in a single place.
func (s Services) registerAdminRoutes(rg *gin.RouterGroup) {
	admin := rg.Group("/admin")
	admin.GET("/overview", s.adminOverview)
	admin.GET("/worker-tasks", s.adminListWorkerTasks)
	admin.GET("/worker-tasks/:task_id", s.adminWorkerTaskDetail)
	admin.GET("/jobs", s.adminListJobs)
	admin.GET("/post-process-records", s.adminListPostProcessRecords)
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
		Type:   c.Query("type"),
		Status: c.Query("status"),
		Query:  c.Query("q"),
		Limit:  atoiDefault(c.Query("limit"), 20),
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

func (s Services) adminListJobs(c *gin.Context) {
	out, err := s.Admin.ListJobs(c.Request.Context(), service.AdminJobListParams{
		Type:   c.Query("type"),
		Status: c.Query("status"),
		PlanID: c.Query("plan_id"),
		Limit:  atoiDefault(c.Query("limit"), 20),
		Offset: atoiDefault(c.Query("offset"), 0),
	})
	if err != nil {
		FailErr(c, err)
		return
	}
	OK(c, out)
}

func (s Services) adminListPostProcessRecords(c *gin.Context) {
	out, err := s.Admin.ListPostProcessRecords(c.Request.Context(), service.AdminPostProcessRecordParams{
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
