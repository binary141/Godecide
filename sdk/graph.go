package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
)

type evaluateGraphRequest struct {
	Spec   GraphSpec      `json:"spec"`
	Inputs map[string]any `json:"inputs"`
}

// Evaluate builds a decision graph from spec on the fly and evaluates it
// against inputs, without deploying it. It maps to POST /api/evaluate.
//
// A non-nil *EvaluationError means the graph was valid but evaluation
// itself failed (e.g. a UNIQUE hit policy table with overlapping rules, or
// a timeout); *APIError means the request itself was rejected (e.g. a
// malformed spec).
func (c *Client) Evaluate(ctx context.Context, spec GraphSpec, inputs map[string]any) (map[string]any, error) {
	body, err := json.Marshal(evaluateGraphRequest{Spec: spec, Inputs: inputs})
	if err != nil {
		return nil, err
	}

	status, respBody, err := c.request(ctx, http.MethodPost, "/api/evaluate", nil, body)
	if err != nil {
		return nil, err
	}

	var res struct {
		Outputs map[string]any `json:"outputs,omitempty"`
		Error   string         `json:"error,omitempty"`
	}
	if status == http.StatusUnprocessableEntity || status == http.StatusGatewayTimeout {
		if err := json.Unmarshal(respBody, &res); err != nil {
			return nil, fmt.Errorf("godecide: decode response: %w", err)
		}
		return nil, &EvaluationError{Message: res.Error}
	}
	if status < 200 || status >= 300 {
		return nil, apiErrorFromBody(status, respBody)
	}

	if err := json.Unmarshal(respBody, &res); err != nil {
		return nil, fmt.Errorf("godecide: decode response: %w", err)
	}
	return res.Outputs, nil
}

// Export builds a decision graph from spec and renders it as DMN 1.5 XML,
// without deploying it. It maps to POST /api/export.
func (c *Client) Export(ctx context.Context, spec GraphSpec) (string, error) {
	body, err := json.Marshal(spec)
	if err != nil {
		return "", err
	}

	var res struct {
		XML string `json:"xml,omitempty"`
	}
	if err := c.do(ctx, http.MethodPost, "/api/export", nil, body, &res); err != nil {
		return "", err
	}
	return res.XML, nil
}
