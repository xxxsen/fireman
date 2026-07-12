package api

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/repository"
	"github.com/fireman/fireman/internal/resourcedb"
	taskcore "github.com/fireman/fireman/internal/task"
)

const (
	resourceTTL            = 7 * 24 * time.Hour
	maxResourceUploadBytes = 128 << 20
)

var errInvalidTaskCursor = errors.New("invalid task cursor")

type InternalDeps struct {
	Logger      *slog.Logger
	Coordinator *taskcore.Coordinator
	Resources   *resourcedb.DB
}

func NewInternalRouter(ctx context.Context, deps InternalDeps) *gin.Engine {
	logger := deps.Logger
	if logger == nil {
		logger = slog.Default()
	}
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery(), requestIDMiddleware(), requestLogger(logger))
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/internal/worker-tasks", internalListTasks(ctx, deps))
	r.GET("/internal/worker-tasks/:task_id", internalGetTask(ctx, deps))
	r.POST("/internal/worker-tasks/:task_id/claim", internalClaimTask(ctx, deps))
	r.POST("/internal/worker-tasks/:task_id/heartbeat", internalHeartbeatTask(ctx, deps))
	r.POST("/internal/worker-tasks/:task_id/release", internalReleaseTask(ctx, deps))
	r.POST("/internal/worker-tasks/:task_id/resources", internalUploadResource(ctx, deps))
	r.POST("/internal/worker-tasks/:task_id/result", internalReportResult(ctx, deps))
	return r
}

type taskCursor struct {
	Priority  int    `json:"p"`
	CreatedAt int64  `json:"c"`
	ID        string `json:"i"`
}

//nolint:gocognit // Cursor validation is intentionally kept beside the worker protocol response.
func internalListTasks(parent context.Context, deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := requestScopedContext(parent, c.Request.Context())
		defer cancel()
		workerType := strings.TrimSpace(c.Query("worker_type"))
		status := strings.TrimSpace(c.Query("status"))
		if workerType == "" || status == "" {
			Fail(c, http.StatusBadRequest, "invalid_request", "worker_type and status are required", nil)
			return
		}
		if !deps.Coordinator.Registry().SupportsWorkerType(workerType) {
			failTaskError(c, taskcore.NewError(taskcore.ErrWorkerTypeMismatch,
				"unsupported worker_type", map[string]any{"worker_type": workerType}))
			return
		}
		if !validWorkerTaskStatus(status) {
			Fail(c, http.StatusBadRequest, "invalid_request", "unsupported task status", nil)
			return
		}
		types := splitNonEmpty(c.Query("types"))
		for _, typ := range types {
			if _, err := deps.Coordinator.Registry().Require(workerType, typ); err != nil {
				failTaskError(c, err)
				return
			}
		}
		limit := atoiDefault(c.Query("limit"), 20)
		if limit < 1 || limit > 100 {
			Fail(c, http.StatusBadRequest, "invalid_request", "limit must be between 1 and 100", nil)
			return
		}
		if status != repository.WorkerTaskStatusPending {
			items, total, err := deps.Coordinator.List(ctx, repository.WorkerTaskFilter{
				WorkerType: workerType, Types: types, Statuses: []string{status}, Limit: limit,
			})
			if err != nil {
				failTaskError(c, err)
				return
			}
			OK(c, gin.H{"items": items, "total": total, "next_cursor": ""})
			return
		}
		var afterPriority *int
		var afterCreated *int64
		var afterID string
		if raw := strings.TrimSpace(c.Query("cursor")); raw != "" {
			cursor, err := decodeTaskCursor(raw)
			if err != nil {
				Fail(c, http.StatusBadRequest, "invalid_request", "invalid cursor", nil)
				return
			}
			afterPriority, afterCreated, afterID = &cursor.Priority, &cursor.CreatedAt, cursor.ID
		}
		items, err := deps.Coordinator.ListClaimable(ctx, workerType, types, limit,
			afterPriority, afterCreated, afterID)
		if err != nil {
			failTaskError(c, err)
			return
		}
		next := ""
		if len(items) == limit {
			last := items[len(items)-1]
			next = encodeTaskCursor(taskCursor{Priority: last.Priority, CreatedAt: last.CreatedAt, ID: last.ID})
		}
		OK(c, gin.H{"items": items, "next_cursor": next})
	}
}

func validWorkerTaskStatus(status string) bool {
	switch status {
	case repository.WorkerTaskStatusPending, repository.WorkerTaskStatusRunning,
		repository.WorkerTaskStatusPreComplete, repository.WorkerTaskStatusComplete,
		repository.WorkerTaskStatusFailed, repository.WorkerTaskStatusCanceled:
		return true
	default:
		return false
	}
}

func internalGetTask(parent context.Context, deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := requestScopedContext(parent, c.Request.Context())
		defer cancel()
		item, err := deps.Coordinator.Get(ctx, c.Param("task_id"))
		if err != nil {
			failTaskError(c, err)
			return
		}
		if c.Query("worker_type") != item.WorkerType {
			failTaskError(c, taskcore.NewError(taskcore.ErrWorkerTypeMismatch, "task belongs to another worker type", nil))
			return
		}
		OK(c, item)
	}
}

func internalClaimTask(parent context.Context, deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req taskcore.ClaimRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		ctx, cancel := requestScopedContext(parent, c.Request.Context())
		defer cancel()
		item, err := deps.Coordinator.Claim(ctx, c.Param("task_id"), req)
		if err != nil {
			failTaskError(c, err)
			return
		}
		OK(c, item)
	}
}

func internalHeartbeatTask(parent context.Context, deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req taskcore.HeartbeatRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		ctx, cancel := requestScopedContext(parent, c.Request.Context())
		defer cancel()
		item, err := deps.Coordinator.Heartbeat(ctx, c.Param("task_id"), req)
		if err != nil {
			failTaskError(c, err)
			return
		}
		OK(c, item)
	}
}

func internalReleaseTask(parent context.Context, deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req taskcore.OwnedRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		ctx, cancel := requestScopedContext(parent, c.Request.Context())
		defer cancel()
		item, err := deps.Coordinator.Release(ctx, c.Param("task_id"), req)
		if err != nil {
			failTaskError(c, err)
			return
		}
		OK(c, item)
	}
}

func internalUploadResource(parent context.Context, deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		ctx, cancel := requestScopedContext(parent, c.Request.Context())
		defer cancel()
		owned := taskcore.OwnedRequest{
			WorkerType: c.GetHeader("X-Fireman-Worker-Type"),
			WorkerID:   c.GetHeader("X-Fireman-Worker-ID"),
			ClaimToken: c.GetHeader("X-Fireman-Claim-Token"),
		}
		item, err := deps.Coordinator.CheckOwned(ctx, c.Param("task_id"), owned)
		if err != nil {
			failTaskError(c, err)
			return
		}
		definition, err := deps.Coordinator.Registry().Require(item.WorkerType, item.Type)
		if err != nil {
			failTaskError(c, err)
			return
		}
		if definition.CompletionMode != taskcore.CompletionFinalizer {
			failTaskError(c, taskcore.NewError(taskcore.ErrResultKeyInvalid,
				"this task type does not accept external resources", nil))
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(c.Writer, c.Request.Body, maxResourceUploadBytes))
		if err != nil {
			Fail(c, http.StatusRequestEntityTooLarge, "resource_too_large", "resource payload exceeds upload limit", nil)
			return
		}
		if len(body) == 0 {
			Fail(c, http.StatusBadRequest, "invalid_request", "resource payload is empty", nil)
			return
		}
		schemaVersion, err := strconv.Atoi(headerOrDefault(c, "X-Fireman-Schema-Version", "1"))
		if err != nil || schemaVersion <= 0 {
			Fail(c, http.StatusBadRequest, "invalid_request", "X-Fireman-Schema-Version must be positive", nil)
			return
		}
		if declared := strings.ToLower(strings.TrimSpace(c.GetHeader("X-Fireman-Content-SHA256"))); declared != "" {
			sum := sha256.Sum256(body)
			if actual := hex.EncodeToString(sum[:]); actual != declared {
				Fail(c, http.StatusBadRequest, "resource_checksum_mismatch", "declared sha256 does not match payload", nil)
				return
			}
		}
		env, err := deps.Resources.InsertContent(ctx,
			headerOrDefault(c, "X-Fireman-Content-Type", "application/json"),
			headerOrDefault(c, "X-Fireman-Content-Encoding", "gzip"), schemaVersion,
			body, time.Now(), resourceTTL)
		if err != nil {
			Fail(c, http.StatusInternalServerError, "resource_store_failed", err.Error(), nil)
			return
		}
		OK(c, gin.H{"result_key": "resource:" + env.ResourceKey, "resource": env})
	}
}

//nolint:nestif // Success-only resource validation must remain inside the authenticated report branch.
func internalReportResult(parent context.Context, deps InternalDeps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req taskcore.ResultRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
			return
		}
		ctx, cancel := requestScopedContext(parent, c.Request.Context())
		defer cancel()
		if req.Outcome == "success" {
			item, err := deps.Coordinator.Get(ctx, c.Param("task_id"))
			if err != nil {
				failTaskError(c, err)
				return
			}
			if item.WorkerType != req.WorkerType {
				failTaskError(c, taskcore.NewError(taskcore.ErrWorkerTypeMismatch,
					"task belongs to another worker type", nil))
				return
			}
			definition, err := deps.Coordinator.Registry().Require(item.WorkerType, item.Type)
			if err != nil {
				failTaskError(c, err)
				return
			}
			if err := definition.ValidateResultKey(req.ResultKey); err != nil {
				failTaskError(c, err)
				return
			}
			key := strings.TrimPrefix(req.ResultKey, "resource:")
			env, err := deps.Resources.EnvelopeByKey(ctx, key)
			if err != nil {
				if errors.Is(err, resourcedb.ErrResourceNotFound) {
					failTaskError(c, taskcore.NewError(taskcore.ErrResultKeyInvalid, "result resource does not exist", nil))
					return
				}
				Fail(c, http.StatusInternalServerError, "resource_read_failed", err.Error(), nil)
				return
			}
			req.ResultMeta, _ = json.Marshal(env)
		}
		item, err := deps.Coordinator.Report(ctx, c.Param("task_id"), req)
		if err != nil {
			failTaskError(c, err)
			return
		}
		OK(c, item)
	}
}

func requestScopedContext(parent, request context.Context) (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(request)
	stopParent := context.AfterFunc(parent, cancel)
	return ctx, func() { stopParent(); cancel() }
}

func headerOrDefault(c *gin.Context, key, fallback string) string {
	if value := strings.TrimSpace(c.GetHeader(key)); value != "" {
		return value
	}
	return fallback
}

func splitNonEmpty(raw string) []string {
	var out []string
	for _, value := range strings.Split(raw, ",") {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

func encodeTaskCursor(value taskCursor) string {
	raw, _ := json.Marshal(value)
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeTaskCursor(raw string) (taskCursor, error) {
	data, err := base64.RawURLEncoding.DecodeString(raw)
	if err != nil {
		return taskCursor{}, err
	}
	var value taskCursor
	if err := json.Unmarshal(data, &value); err != nil || value.ID == "" {
		return taskCursor{}, errInvalidTaskCursor
	}
	return value, nil
}

func failTaskError(c *gin.Context, err error) {
	var taskErr *taskcore.Error
	if !errors.As(err, &taskErr) {
		Fail(c, http.StatusBadRequest, "invalid_request", err.Error(), nil)
		return
	}
	status := http.StatusBadRequest
	switch taskErr.Code {
	case taskcore.ErrNotFound:
		status = http.StatusNotFound
	case taskcore.ErrWorkerTypeMismatch:
		status = http.StatusForbidden
	case taskcore.ErrClaimConflict, taskcore.ErrLeaseLost, taskcore.ErrAlreadyTerminal,
		taskcore.ErrCancelRequested, taskcore.ErrResultConflict:
		status = http.StatusConflict
	}
	Fail(c, status, taskErr.Code, taskErr.Message, taskErr.Details)
}
