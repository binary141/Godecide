package engine

import (
	"errors"
	"time"
)

// DefaultEvaluationTimeout is the wall-clock budget callers should give a
// single Evaluate/EvaluateWithTrace call when the input DMN isn't trusted
// (e.g. it came in over HTTP). It has no effect unless a caller opts in via
// EvaluateTimeout/EvaluateWithTraceTimeout.
const DefaultEvaluationTimeout = 30 * time.Second

// ErrEvaluationTimeout is returned by EvaluateTimeout/EvaluateWithTraceTimeout
// when evaluation doesn't finish within the given timeout.
var ErrEvaluationTimeout = errors.New("evaluation exceeded time limit")

// EvaluateTimeout is Evaluate, except the call returns ErrEvaluationTimeout
// once timeout elapses instead of waiting indefinitely. Go has no way to
// preempt a running goroutine, so a pathological expression (e.g. a
// for/some/every boxed expression iterating an unbounded range) keeps
// consuming CPU in the background after this returns - callers are
// protected from hanging on the request, not from the underlying resource
// cost of the runaway evaluation.
func (d Definitions) EvaluateTimeout(context map[string]any, timeout time.Duration) (map[string]any, error) {
	type result struct {
		outputs map[string]any
		err     error
	}

	done := make(chan result, 1)
	go func() {
		outputs, err := d.Evaluate(context)
		done <- result{outputs, err}
	}()

	select {
	case r := <-done:
		return r.outputs, r.err
	case <-time.After(timeout):
		return nil, ErrEvaluationTimeout
	}
}

// EvaluateWithTraceTimeout is EvaluateWithTrace with the same timeout
// behavior as EvaluateTimeout.
func (d Definitions) EvaluateWithTraceTimeout(context map[string]any, timeout time.Duration) (map[string]any, []DecisionTrace, error) {
	type result struct {
		outputs map[string]any
		trace   []DecisionTrace
		err     error
	}

	done := make(chan result, 1)
	go func() {
		outputs, trace, err := d.EvaluateWithTrace(context)
		done <- result{outputs, trace, err}
	}()

	select {
	case r := <-done:
		return r.outputs, r.trace, r.err
	case <-time.After(timeout):
		return nil, nil, ErrEvaluationTimeout
	}
}
