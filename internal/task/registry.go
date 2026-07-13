package task

import (
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/fireman/fireman/internal/repository"
)

var (
	errPayloadNotJSONObject = errors.New("payload must be a JSON object")
	errResultMetaInvalid    = errors.New("result_meta must be valid JSON")
)

type CompletionMode string

const (
	CompletionDirect    CompletionMode = "direct"
	CompletionFinalizer CompletionMode = "finalizer"
)

type Definition struct {
	WorkerType           string
	Type                 string
	CompletionMode       CompletionMode
	MaxAttempts          int
	LeaseDuration        time.Duration
	DefaultPriority      int
	InitialProgressTotal int
	ResultPrefix         string
	ValidatePayload      func(json.RawMessage) error
	ValidateResult       func(ResultRequest) error
	RetryClassifier      func(error) bool
	ProcessorKey         string
	FinalizerKey         string
}

type Registry struct{ definitions map[string]Definition }

// Definitions returns a stable snapshot used by startup completeness checks.
func (r *Registry) Definitions() []Definition {
	out := make([]Definition, 0, len(r.definitions))
	for _, definition := range r.definitions {
		out = append(out, definition)
	}
	slices.SortFunc(out, func(a, b Definition) int {
		return strings.Compare(a.WorkerType+"\x00"+a.Type, b.WorkerType+"\x00"+b.Type)
	})
	return out
}

var (
	errDefinitionIdentity   = errors.New("task definition worker_type and type are required")
	errDefinitionRetryLease = errors.New("task definition has invalid retry or lease")
	errDefinitionIncomplete = errors.New("task definition is incomplete")
	errDefinitionFinalizer  = errors.New("task definition has no finalizer")
	errDefinitionDuplicate  = errors.New("duplicate task definition")
)

func NewRegistry(definitions []Definition) (*Registry, error) {
	r := &Registry{definitions: map[string]Definition{}}
	for _, definition := range definitions {
		if definition.WorkerType == "" || definition.Type == "" {
			return nil, errDefinitionIdentity
		}
		if definition.MaxAttempts <= 0 || definition.LeaseDuration <= 0 {
			return nil, fmt.Errorf("%w: %s/%s", errDefinitionRetryLease, definition.WorkerType, definition.Type)
		}
		if definition.ValidatePayload == nil || definition.ValidateResult == nil ||
			definition.RetryClassifier == nil || definition.ProcessorKey == "" {
			return nil, fmt.Errorf("%w: %s/%s", errDefinitionIncomplete, definition.WorkerType, definition.Type)
		}
		if definition.CompletionMode == CompletionFinalizer && definition.FinalizerKey == "" {
			return nil, fmt.Errorf("%w: %s/%s", errDefinitionFinalizer, definition.WorkerType, definition.Type)
		}
		key := definition.WorkerType + "\x00" + definition.Type
		if _, exists := r.definitions[key]; exists {
			return nil, fmt.Errorf("%w: %s/%s", errDefinitionDuplicate, definition.WorkerType, definition.Type)
		}
		r.definitions[key] = definition
	}
	return r, nil
}

func DefaultRegistry() *Registry {
	definitions := []Definition{
		goDefinition(repository.WorkerTaskTypeSimulation, "simulation_run:"),
		goDefinition(repository.WorkerTaskTypeStress, "analysis_result:"),
		goDefinition(repository.WorkerTaskTypeSensitivity, "analysis_result:"),
		goDefinition(repository.WorkerTaskTypeFirePlanImprovement, "fire_plan_improvement_run:"),
		frontierDefinition(),
		goDefinition(repository.WorkerTaskTypeResearchBacktest, "research_backtest_run:"),
		goDefinition(repository.WorkerTaskTypeResearchOptimization, "research_optimization_run:"),
		{
			WorkerType: repository.WorkerTypeGo, Type: repository.WorkerTaskTypeAutoUpdateScan,
			CompletionMode: CompletionDirect, MaxAttempts: 2, LeaseDuration: time.Minute,
			DefaultPriority: 50, InitialProgressTotal: 1, ResultPrefix: "resource:",
			ValidatePayload: validateJSONObject, ValidateResult: validateResultRequest,
			RetryClassifier: defaultRetryClassifier, ProcessorKey: repository.WorkerTaskTypeAutoUpdateScan,
		},
		sidecarDefinition(repository.WorkerTaskTypeAssetDirectorySync),
		sidecarDefinition(repository.WorkerTaskTypeAssetHistorySync),
		sidecarDefinition(repository.WorkerTaskTypeFXRateSync),
	}
	registry, err := NewRegistry(definitions)
	if err != nil {
		panic(err)
	}
	return registry
}

func goDefinition(taskType, prefix string) Definition {
	return Definition{
		WorkerType: repository.WorkerTypeGo, Type: taskType, CompletionMode: CompletionDirect,
		MaxAttempts: 2, LeaseDuration: time.Minute, DefaultPriority: 100,
		ResultPrefix: prefix, ValidatePayload: validateJSONObject,
		ValidateResult: validateResultRequest, RetryClassifier: defaultRetryClassifier,
		ProcessorKey: taskType,
	}
}

func frontierDefinition() Definition {
	definition := goDefinition(repository.WorkerTaskTypeFireFrontier, "fire_frontier_run:")
	definition.RetryClassifier = frontierRetryClassifier
	return definition
}

// Frontier algorithm/input failures are deterministic. Only transient SQLite
// contention and temporary I/O conditions may consume the second attempt.
func frontierRetryClassifier(err error) bool {
	if err == nil {
		return false
	}
	var taskErr *Error
	if errors.As(err, &taskErr) {
		return false
	}
	message := strings.ToLower(err.Error())
	for _, fragment := range []string{
		"database is locked", "database table is locked", "database is busy",
		"resource temporarily unavailable", "temporary i/o", "i/o timeout",
	} {
		if strings.Contains(message, fragment) {
			return true
		}
	}
	return false
}

func sidecarDefinition(taskType string) Definition {
	return Definition{
		WorkerType: repository.WorkerTypeSidecar, Type: taskType, CompletionMode: CompletionFinalizer,
		MaxAttempts: 2, LeaseDuration: time.Minute, DefaultPriority: 100,
		InitialProgressTotal: 1, ResultPrefix: "resource:", ValidatePayload: validateJSONObject,
		ValidateResult: validateResultRequest, RetryClassifier: defaultRetryClassifier,
		ProcessorKey: taskType, FinalizerKey: taskType,
	}
}

func validateJSONObject(raw json.RawMessage) error {
	var value map[string]any
	if len(raw) == 0 || json.Unmarshal(raw, &value) != nil || value == nil {
		return errPayloadNotJSONObject
	}
	return nil
}

func validateResultRequest(req ResultRequest) error {
	if err := req.Validate(); err != nil {
		return err
	}
	if len(req.ResultMeta) > 0 && !json.Valid(req.ResultMeta) {
		return errResultMetaInvalid
	}
	return nil
}

func defaultRetryClassifier(err error) bool {
	var taskErr *Error
	if !errors.As(err, &taskErr) {
		return true
	}
	switch taskErr.Code {
	case ErrPayloadInvalid, ErrTypeUnsupported, ErrResultKeyInvalid, ErrResultConflict:
		return false
	default:
		return true
	}
}

func (r *Registry) Lookup(workerType, taskType string) (Definition, bool) {
	definition, ok := r.definitions[workerType+"\x00"+taskType]
	return definition, ok
}

func (r *Registry) SupportsWorkerType(workerType string) bool {
	for _, definition := range r.definitions {
		if definition.WorkerType == workerType {
			return true
		}
	}
	return false
}

func (r *Registry) DefinitionsFor(workerType string) []Definition {
	out := make([]Definition, 0)
	for _, definition := range r.definitions {
		if definition.WorkerType == workerType {
			out = append(out, definition)
		}
	}
	return out
}

func (r *Registry) Require(workerType, taskType string) (Definition, error) {
	definition, ok := r.Lookup(workerType, taskType)
	if !ok {
		return Definition{}, NewError(ErrTypeUnsupported, "unsupported worker_type/task type combination", map[string]any{
			"worker_type": workerType, "type": taskType,
		})
	}
	return definition, nil
}

func (d Definition) ValidateResultKey(key string) error {
	if !strings.HasPrefix(key, d.ResultPrefix) || len(key) == len(d.ResultPrefix) {
		return NewError(ErrResultKeyInvalid, "result_key does not match task type", map[string]any{
			"expected_prefix": d.ResultPrefix,
		})
	}
	return nil
}
