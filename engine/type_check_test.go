package engine

import "testing"

// mismatchedTypeDefinitions parses a DMN document whose single decision is
// declared typeRef="number" but whose body evaluates to a string - the
// scenario GAPS.md finding #2 describes: plain Evaluate coerces this to
// null per the DMN FEEL type-conformance rules (and the TCK's
// feel_coercion suite depends on that), while EvaluateStrict is expected to
// report it as an error instead.
func mismatchedTypeDefinitions(t *testing.T) Definitions {
	t.Helper()

	xmlDoc := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20230324/MODEL/" id="d1" name="d1" namespace="ns">
  <decision name="mismatched" id="mismatched">
    <variable name="mismatched" id="v1" typeRef="number"/>
    <literalExpression>
      <text>"not a number"</text>
    </literalExpression>
  </decision>
</definitions>`

	d, err := Parse([]byte(xmlDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	return d
}

func TestEvaluateCoercesTypeMismatchToNull(t *testing.T) {
	d := mismatchedTypeDefinitions(t)

	outputs, err := d.Evaluate(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !isFEELNull(outputs["mismatched"]) {
		t.Fatalf("expected null for a non-conforming result, got %#v", outputs["mismatched"])
	}
}

func TestEvaluateStrictReportsTypeMismatch(t *testing.T) {
	d := mismatchedTypeDefinitions(t)

	_, err := d.EvaluateStrict(map[string]any{})
	if err == nil {
		t.Fatalf("expected an error for a result that doesn't conform to its declared typeRef")
	}
}

func TestEvaluateStrictNotTriggeredForConformingResult(t *testing.T) {
	xmlDoc := `<?xml version="1.0" encoding="UTF-8"?>
<definitions xmlns="https://www.omg.org/spec/DMN/20230324/MODEL/" id="d1" name="d1" namespace="ns">
  <decision name="conforming" id="conforming">
    <variable name="conforming" id="v1" typeRef="number"/>
    <literalExpression>
      <text>1 + 1</text>
    </literalExpression>
  </decision>
</definitions>`

	d, err := Parse([]byte(xmlDoc))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}

	outputs, err := d.EvaluateStrict(map[string]any{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outputs["conforming"] == nil {
		t.Fatalf("expected an output for decision \"conforming\", got %+v", outputs)
	}
}
