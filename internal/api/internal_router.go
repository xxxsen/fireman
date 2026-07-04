package api

import (
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/resourcedb"
	"github.com/fireman/fireman/internal/service"
)

// resourceTTL is how long uploaded task results stay readable in resource_db.
const resourceTTL = 7 * 24 * time.Hour

// maxResourceUploadBytes caps the compressed upload size accepted from the
// sidecar.
const maxResourceUploadBytes = 128 << 20 // 128 MiB

// InternalDeps groups dependencies of the internal (sidecar-facing) HTTP
// listener. This listener is never published outside the docker network.
type InternalDeps struct {
	Logger      *slog.Logger
	PostProcess *service.PostProcessService
	Resources   *resourcedb.DB
}

// NewInternalRouter builds the internal Gin engine serving the sidecar worker:
//
//   - POST /internal/resources: upload a task result payload. resource_db is
//     owned exclusively by Go; the sidecar never opens it. The resource key is
//     the payload's sha256, so retried uploads are idempotent.
//   - POST /internal/tasks/:task_id/post-process: apply a pre_complete task's
//     result to business tables; returns the success/retryable/permanent
//     classification.
func NewInternalRouter(deps InternalDeps) *gin.Engine {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(requestIDMiddleware())
	r.Use(requestLogger(logger))

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	r.POST("/internal/resources", uploadResourceHandler(deps))
	r.POST("/internal/tasks/:task_id/post-process", postProcessHandler(deps))
	return r
}

func uploadResourceHandler(deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxResourceUploadBytes))
		if err != nil {
			Fail(c, http.StatusRequestEntityTooLarge, "resource_too_large",
				"resource payload exceeds upload limit", nil)
			return
		}
		if len(body) == 0 {
			Fail(c, http.StatusBadRequest, "invalid_request", "resource payload is empty", nil)
			return
		}

		contentType := headerOrDefault(c, "X-Fireman-Content-Type", "application/json")
		contentEncoding := headerOrDefault(c, "X-Fireman-Content-Encoding", "gzip")
		schemaVersion, err := strconv.Atoi(headerOrDefault(c, "X-Fireman-Schema-Version", "1"))
		if err != nil || schemaVersion <= 0 {
			Fail(c, http.StatusBadRequest, "invalid_request", "X-Fireman-Schema-Version must be a positive integer", nil)
			return
		}

		// Optional end-to-end integrity check: when the sidecar precomputes the
		// digest, a transport corruption is rejected before any write.
		if declared := strings.ToLower(strings.TrimSpace(
			c.GetHeader("X-Fireman-Content-SHA256"))); declared != "" {
			sum := sha256.Sum256(body)
			if actual := hex.EncodeToString(sum[:]); actual != declared {
				Fail(c, http.StatusBadRequest, "resource_checksum_mismatch",
					"declared sha256 does not match uploaded payload", map[string]any{
						"declared": declared, "actual": actual,
					})
				return
			}
		}

		env, err := deps.Resources.InsertContent(
			c.Request.Context(), contentType, contentEncoding, schemaVersion,
			body, time.Now(), resourceTTL,
		)
		if err != nil {
			Fail(c, http.StatusInternalServerError, "resource_store_failed", err.Error(), nil)
			return
		}
		OK(c, env)
	}
}

func headerOrDefault(c *gin.Context, key, fallback string) string {
	if v := strings.TrimSpace(c.GetHeader(key)); v != "" {
		return v
	}
	return fallback
}

func postProcessHandler(deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		result := deps.PostProcess.Process(c.Request.Context(), c.Param("task_id"))
		// Always 200: the classification travels in the body and the sidecar
		// drives retry/terminal-state decisions from it, not from HTTP codes.
		OK(c, result)
	}
}
