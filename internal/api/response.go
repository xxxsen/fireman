package api

import (
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/fireman/fireman/internal/jsonutil"
	"github.com/fireman/fireman/internal/service"
)

type envelope struct {
	Code      string `json:"code"`
	Message   string `json:"message"`
	Data      any    `json:"data"`
	RequestID string `json:"request_id"`
}

type errorBody struct {
	Code      string         `json:"code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
	RequestID string         `json:"request_id"`
}

func OK(c *gin.Context, data any) {
	if data != nil {
		jsonutil.NonNilSlices(data)
	}
	c.JSON(http.StatusOK, envelope{
		Code: "ok", Message: "success", Data: data, RequestID: requestID(c),
	})
}

func Fail(c *gin.Context, status int, code, message string, details map[string]any) {
	c.JSON(status, errorBody{
		Code: code, Message: message, Details: details, RequestID: requestID(c),
	})
}

func FailErr(c *gin.Context, err error) {
	var ae *service.AppError
	if ok := asAppError(err, &ae); ok {
		status := http.StatusBadRequest
		switch ae.Code {
		case "plan_not_found", "scenario_not_found", "instrument_not_found", "holding_not_found", "snapshot_not_found":
			status = http.StatusNotFound
		case "instrument_fields_read_only", "holding_fields_read_only", "instrument_classification_unsupported",
			"instrument_metadata_conflict", "provider_data_anomaly", "instrument_not_deletable",
			"instrument_in_use", "instrument_not_refreshable", "instrument_refresh_throttled",
			"instrument_insufficient_history", "instrument_already_exists", "instrument_type_mismatch",
			"instrument_not_editable":
			status = http.StatusBadRequest
		case "market_provider_unavailable", "market_provider_timeout":
			status = http.StatusBadGateway
		case "plan_version_conflict", "instrument_version_conflict", "rule_version_conflict":
			status = http.StatusConflict
		case "idempotency_conflict", "job_already_terminal", "system_profile_identity_conflict":
			status = http.StatusConflict
		case "simulation_not_found", "path_not_found", "job_not_found",
			"task_not_found", "market_asset_not_found", "auto_update_rule_not_found":
			status = http.StatusNotFound
		case "research_collection_not_found", "research_item_not_found",
			"research_run_not_found", "research_optimization_not_found", "saved_filter_not_found":
			status = http.StatusNotFound
		case "research_collection_changed", "research_optimization_result_stale":
			status = http.StatusConflict
		case "market_asset_history_empty":
			status = http.StatusBadRequest
		case "simulation_input_invalid", "plan_weights_invalid", "invalid_backup", "invalid_request":
			status = http.StatusBadRequest
		case "parameters_invalid", "foreign_cash_not_supported":
			status = http.StatusBadRequest
		case "builtin_scenario_immutable", "scenario_in_use":
			status = http.StatusBadRequest
		default:
			if ae.Code == "holding_fields_read_only" {
				status = http.StatusBadRequest
			}
		}
		Fail(c, status, ae.Code, ae.Message, ae.Details)
		return
	}
	Fail(c, http.StatusInternalServerError, "internal_error", "internal server error", nil)
}

func asAppError(err error, target **service.AppError) bool {
	if err == nil {
		return false
	}
	ae := &service.AppError{}
	if errors.As(err, &ae) {
		*target = ae
		return true
	}
	return false
}

func requestID(c *gin.Context) string {
	if v, ok := c.Get("request_id"); ok {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}
