package starbox

import (
	"errors"
	"fmt"

	"github.com/1set/starlet"
	"go.starlark.net/resolve"
	"go.starlark.net/starlark"
	"go.starlark.net/syntax"
)

// RunErrorKind classifies why a Run/Call failed. The zero value
// RunErrorUnknown is intentional and fail-loud: an unrecognised error is never
// silently treated as a known kind.
type RunErrorKind int

const (
	// RunErrorUnknown is an error that could not be classified.
	RunErrorUnknown RunErrorKind = iota
	// RunErrorSyntax is a parse failure (the script is not valid Starlark).
	RunErrorSyntax
	// RunErrorCompile is a resolve failure: an undefined name, a bad load
	// binding, and similar pre-execution problems.
	RunErrorCompile
	// RunErrorModuleWithheld is load() of a real module the active set withholds.
	RunErrorModuleWithheld
	// RunErrorMaxSteps is the execution step budget being exceeded.
	RunErrorMaxSteps
	// RunErrorOutputLimit is the result exceeding the configured output limit.
	RunErrorOutputLimit
	// RunErrorEval is a runtime evaluation error (the script raised or failed
	// while executing).
	RunErrorEval
)

// String returns a short, stable name for the kind.
func (k RunErrorKind) String() string {
	switch k {
	case RunErrorSyntax:
		return "syntax"
	case RunErrorCompile:
		return "compile"
	case RunErrorModuleWithheld:
		return "module_withheld"
	case RunErrorMaxSteps:
		return "max_steps"
	case RunErrorOutputLimit:
		return "output_limit"
	case RunErrorEval:
		return "eval"
	default:
		return "unknown"
	}
}

// RunError classifies a failed run by Kind while preserving the original error
// chain: errors.As / errors.Is against the underlying typed errors
// (ModuleWithheldError, starlet.MaxStepsExceededError, OutputLimitExceededError,
// *starlark.EvalError, …) keep working through it via Unwrap.
type RunError struct {
	Kind RunErrorKind
	Err  error // the underlying error
}

// Error implements error.
func (e *RunError) Error() string {
	if e == nil || e.Err == nil {
		return "run error: <nil>"
	}
	return fmt.Sprintf("run error [%s]: %v", e.Kind, e.Err)
}

// Unwrap returns the underlying error so the original chain stays reachable
// (errors.As / errors.Is pass through a *RunError unchanged).
func (e *RunError) Unwrap() error { return e.Err }

// ClassifyRunError wraps a Run/Call error in a *RunError tagged with its Kind,
// classifying by the most specific typed error first (a withheld-module or
// budget error also presents as a Starlark eval error, so those are matched
// before RunErrorEval). It returns nil for a nil error and preserves the
// original error via Unwrap, so callers can both switch on Kind and errors.As
// to the specific typed error for details.
func ClassifyRunError(err error) *RunError {
	if err == nil {
		return nil
	}
	return &RunError{Kind: classifyRunErrorKind(err), Err: err}
}

func classifyRunErrorKind(err error) RunErrorKind {
	var withheld ModuleWithheldError
	if errors.As(err, &withheld) {
		return RunErrorModuleWithheld
	}
	var maxSteps starlet.MaxStepsExceededError
	if errors.As(err, &maxSteps) {
		return RunErrorMaxSteps
	}
	var outLimit OutputLimitExceededError
	if errors.As(err, &outLimit) {
		return RunErrorOutputLimit
	}
	var synErr syntax.Error
	if errors.As(err, &synErr) {
		return RunErrorSyntax
	}
	var resErr resolve.ErrorList
	if errors.As(err, &resErr) {
		return RunErrorCompile
	}
	var evalErr *starlark.EvalError
	if errors.As(err, &evalErr) {
		return RunErrorEval
	}
	return RunErrorUnknown
}
