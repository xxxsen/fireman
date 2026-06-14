package service

import (
	"errors"
	"strings"
)

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

func isUniqueConstraintErr(err error) bool {
	if err == nil {
		return false
	}
	return strings.Contains(strings.ToLower(err.Error()), "unique constraint failed")
}
