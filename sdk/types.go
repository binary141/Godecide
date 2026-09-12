package sdk

import (
	"encoding/json"
	"time"
)

// InputSpec is a single input column of a decision table. Source, when
// non-empty, is the ID of another NodeSpec in the same GraphSpec whose
// output feeds this column instead of an external input.
type InputSpec struct {
	Label   string `json:"label"`
	TypeRef string `json:"typeRef,omitempty"`
	Source  string `json:"source,omitempty"`
}

// OutputSpec is a single output column of a decision table.
type OutputSpec struct {
	Name    string `json:"name"`
	TypeRef string `json:"typeRef,omitempty"`
}

// RuleSpec is one row of a decision table: one raw (unnormalized) cell per
// input column followed by one per output column.
type RuleSpec struct {
	InputEntries  []string `json:"inputEntries"`
	OutputEntries []string `json:"outputEntries"`
}

// NodeSpec is a single decision table node in a graph. ID is caller-assigned
// and doubles as the exported decision's ID, so evaluation outputs and
// requiredDecision edges can be keyed by it directly.
type NodeSpec struct {
	ID           string       `json:"id"`
	DecisionName string       `json:"decisionName"`
	HitPolicy    string       `json:"hitPolicy,omitempty"`
	Aggregation  string       `json:"aggregation,omitempty"`
	Inputs       []InputSpec  `json:"inputs"`
	Outputs      []OutputSpec `json:"outputs"`
	Rules        []RuleSpec   `json:"rules"`
}

// GraphSpec is a whole decision graph: one or more decision table nodes,
// optionally wired to each other's outputs via InputSpec.Source. It is the
// request body for Evaluate and Export.
type GraphSpec struct {
	Nodes []NodeSpec `json:"nodes"`
}

// MatchedRule identifies a single decision-table rule that fired during
// evaluation.
type MatchedRule struct {
	RuleIndex int    `json:"ruleIndex"`
	RuleID    string `json:"ruleId,omitempty"`
}

// DecisionTrace records which rule(s) fired for a single decision within a
// graph, in evaluation order.
type DecisionTrace struct {
	DecisionID   string        `json:"decisionId"`
	DecisionName string        `json:"decisionName"`
	MatchedRules []MatchedRule `json:"matchedRules"`
}

// EvaluationResult is the outcome of evaluating a deployment: either
// Outputs is populated, or the evaluation failed (surfaced by Evaluate*
// methods as an error) while Trace still reflects whatever rules matched
// before the failure.
type EvaluationResult struct {
	Outputs map[string]any  `json:"outputs,omitempty"`
	Trace   []DecisionTrace `json:"trace,omitempty"`
}

// Deployment is a single ingested DMN file. XML is omitted from list
// responses (see ListDeployments) but populated by Get/GetByName/Create.
type Deployment struct {
	ID         int64     `json:"id"`
	Name       string    `json:"name"`
	Namespace  string    `json:"namespace"`
	DMNVersion string    `json:"dmnVersion"`
	Version    int       `json:"version"`
	XML        string    `json:"xml,omitempty"`
	CreatedAt  time.Time `json:"createdAt"`
}

// DeploymentPage is a page of deployments, newest first.
type DeploymentPage struct {
	Deployments []Deployment `json:"deployments"`
	Total       int          `json:"total"`
	Limit       int          `json:"limit"`
	Offset      int          `json:"offset"`
}

// Evaluation is a single recorded call to a deployment's evaluate endpoint:
// the inputs it ran with, the outputs or error it produced, which rules
// fired, and when.
type Evaluation struct {
	ID           int64           `json:"id"`
	DeploymentID int64           `json:"deploymentId"`
	Inputs       json.RawMessage `json:"inputs"`
	Outputs      json.RawMessage `json:"outputs,omitempty"`
	Trace        json.RawMessage `json:"trace,omitempty"`
	Error        *string         `json:"error,omitempty"`
	CreatedAt    time.Time       `json:"createdAt"`
}

// EvaluationPage is a page of evaluation history for one deployment, newest
// first.
type EvaluationPage struct {
	Evaluations []Evaluation `json:"evaluations"`
	Total       int          `json:"total"`
	Limit       int          `json:"limit"`
	Offset      int          `json:"offset"`
}

// ListOptions paginates a listing. A zero value requests the server's
// defaults (currently limit 20, offset 0).
type ListOptions struct {
	Limit  int
	Offset int
}
