package engine

import (
	"errors"
	"testing"
	"time"
)

// slowDefinitions parses a DMN document whose single decision iterates a
// large range - the same shape of attacker-supplied FEEL that finding #3 in
// FINDINGS.md describes as an unbounded-evaluation DoS vector.
func slowDefinitions(t *testing.T) Definitions {
	t.Helper()

	xmlDoc := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20230324/MODEL/" id="d1" name="d1" namespace="ns">
  <decision name="slow" id="slow">
    <variable name="slow" id="v1"/>
    <literalExpression>
      <text>for x in 1..5000000 return x * x</text>
    </literalExpression>
  </decision>
</definitions>`

	d, err := Parse([]byte(xmlDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func TestEvaluateTimeout(t *testing.T) {
	d := slowDefinitions(t)

	start := time.Now()
	_, err := d.EvaluateTimeout(map[string]any{}, 10*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrEvaluationTimeout) {
		t.Fatalf("expected ErrEvaluationTimeout, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("EvaluateTimeout should return promptly once the deadline passes, took %v", elapsed)
	}
}

func TestEvaluateWithTraceTimeout(t *testing.T) {
	d := slowDefinitions(t)

	start := time.Now()
	_, _, err := d.EvaluateWithTraceTimeout(map[string]any{}, 10*time.Millisecond)
	elapsed := time.Since(start)

	if !errors.Is(err, ErrEvaluationTimeout) {
		t.Fatalf("expected ErrEvaluationTimeout, got %v", err)
	}
	if elapsed > time.Second {
		t.Fatalf("EvaluateWithTraceTimeout should return promptly once the deadline passes, took %v", elapsed)
	}
}

func TestEvaluateTimeoutNotTriggeredForFastEvaluation(t *testing.T) {
	xmlDoc := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20230324/MODEL/" id="d1" name="d1" namespace="ns">
  <decision name="fast" id="fast">
    <variable name="fast" id="v1"/>
    <literalExpression>
      <text>1 + 1</text>
    </literalExpression>
  </decision>
</definitions>`

	d, err := Parse([]byte(xmlDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outputs, err := d.EvaluateTimeout(map[string]any{}, DefaultEvaluationTimeout)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["fast"] == nil {
		t.Fatalf("expected an output for decision \"fast\", got %+v", outputs)
	}
}
