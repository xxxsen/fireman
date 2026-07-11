// Package api owns the HTTP transport layer: Gin engine, request/response
// models and middleware.
package api

import (
	"context"
	"database/sql"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/db"
	"github.com/fireman/fireman/internal/jobs"
	"github.com/fireman/fireman/internal/service"
)

// Deps groups the dependencies the HTTP layer needs.
type Deps struct {
	DB       *sql.DB
	DBPath   string
	Logger   *slog.Logger
	Services Services
	Worker   *jobs.Worker
}

// NewRouter builds the Gin engine and registers routes.
func NewRouter(ctx context.Context, deps Deps) *gin.Engine {
	_ = ctx
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	if deps.Services.Plans == nil {
		deps.Services = NewServices(deps.DB, deps.DBPath, nil, nil)
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestIDMiddleware())
	r.Use(requestLogger(deps.Logger))

	r.GET("/healthz", healthz(ctx, deps))

	v1 := r.Group("/api/v1")
	v1.Use(maintenanceMiddleware(deps.Services.Maintenance))
	deps.Services.registerPlanRoutes(v1)
	deps.Services.registerQuickFIRERoutes(v1)
	deps.Services.registerScenarioRoutes(v1)
	deps.Services.registerMarketAssetRoutes(v1)
	deps.Services.registerSimulationRoutes(v1)
	deps.Services.registerAssumptionRoutes(v1)
	deps.Services.registerAnalysisRoutes(v1)
	deps.Services.registerJobRoutes(v1)
	deps.Services.registerResearchRoutes(v1)
	deps.Services.registerSystemRoutes(v1)
	deps.Services.registerAdminRoutes(v1)

	return r
}

func maintenanceMiddleware(gate *service.MaintenanceGate) gin.HandlerFunc {
	return func(c *gin.Context) {
		if gate == nil || !gate.Active() {
			c.Next()
			return
		}
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead:
			c.Next()
			return
		}
		if c.Request.Method == http.MethodPost && c.FullPath() == "/api/v1/system/restore" {
			c.Next()
			return
		}
		Fail(c, http.StatusServiceUnavailable, "maintenance", "server is restoring backup", nil)
		c.Abort()
	}
}

func healthz(lifecycleCtx context.Context, deps Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		probeCtx, cancel := context.WithTimeout(lifecycleCtx, 2*time.Second)
		defer cancel()
		if err := db.Ping(probeCtx, deps.DB); err != nil {
			deps.Logger.Error("healthz database probe failed", "error", err)
			c.JSON(http.StatusServiceUnavailable, gin.H{
				"status": "unhealthy",
				"reason": "database_unavailable",
			})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	}
}

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info(
			"http_request",
			"method", c.Request.Method,
			"path", c.Request.URL.Path,
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"request_id", requestID(c),
		)
	}
}
