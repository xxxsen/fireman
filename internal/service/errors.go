package service

import (
	"errors"
	"strings"

	"github.com/fireman/fireman/internal/marketdata"
)

// MapSnapshotError converts snapshot errors to AppError.
func MapSnapshotError(err error) error {
	var se *marketdata.SnapshotError
	if errors.As(err, &se) {
		return newErr(se.Code, se.Message, se.Details)
	}
	return err
}

var (
	ErrNotFound          = errors.New("not found")
	ErrVersionConflict   = errors.New("version conflict")
	ErrValidation        = errors.New("validation failed")
	ErrBuiltinScenario   = errors.New("builtin scenario")
	ErrScenarioInUse     = errors.New("scenario in use")
	ErrHoldingReadOnly   = errors.New("holding fields read only")
	ErrInstrumentMissing = errors.New("instrument not found")
)

// AppError carries a stable API error code.
type AppError struct {
	Code    string
	Message string
	Details map[string]any
	Err     error
}

func (e *AppError) Error() string {
	if e.Message != "" {
		return e.Message
	}
	return e.Code
}

func (e *AppError) Unwrap() error { return e.Err }

func newErr(code, message string, details map[string]any) *AppError {
	return &AppError{Code: code, Message: message, Details: details}
}

// NewPublicError lets infrastructure adapters preserve a stable business error
// code without exposing service internals.
func NewPublicError(code, message string, details map[string]any) *AppError {
	return newErr(code, message, details)
}

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
