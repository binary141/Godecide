package sdk

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
)

// CreateDeployment ingests a raw DMN XML document, parses and validates it
// server-side, and stores it as a new deployment. Redeploying the same
// decision name bumps its version rather than replacing it. It maps to
// POST /api/deployments.
func (c *Client) CreateDeployment(ctx context.Context, dmnXML string) (Deployment, error) {
	var d Deployment
	err := c.do(ctx, http.MethodPost, "/api/deployments", nil, []byte(dmnXML), &d)
	return d, err
}

// ListDeployments returns a page of deployments, newest first, without
// their XML bodies. It maps to GET /api/deployments.
func (c *Client) ListDeployments(ctx context.Context, opts ListOptions) (DeploymentPage, error) {
	var page DeploymentPage
	err := c.do(ctx, http.MethodGet, "/api/deployments", paginationQuery(opts), nil, &page)
	return page, err
}

// GetDeployment returns a single deployment, including its XML body. It
// maps to GET /api/deployments/{id}.
func (c *Client) GetDeployment(ctx context.Context, id int64) (Deployment, error) {
	var d Deployment
	err := c.do(ctx, http.MethodGet, "/api/deployments/"+strconv.FormatInt(id, 10), nil, nil, &d)
	return d, err
}

// GetLatestDeploymentByName returns the highest-versioned deployment for
// name, including its XML body. It maps to GET /api/deployments/latest?name=.
func (c *Client) GetLatestDeploymentByName(ctx context.Context, name string) (Deployment, error) {
	var d Deployment
	q := url.Values{"name": {name}}
	err := c.do(ctx, http.MethodGet, "/api/deployments/latest", q, nil, &d)
	return d, err
}

// GetDeploymentByNameVersion returns a specific historical version of a
// named deployment, including its XML body. It maps to
// GET /api/deployments/latest?name=&version=.
func (c *Client) GetDeploymentByNameVersion(ctx context.Context, name string, version int) (Deployment, error) {
	var d Deployment
	q := url.Values{"name": {name}, "version": {strconv.Itoa(version)}}
	err := c.do(ctx, http.MethodGet, "/api/deployments/latest", q, nil, &d)
	return d, err
}

// DeleteDeployment removes a deployment by id. It maps to
// DELETE /api/deployments/{id}.
func (c *Client) DeleteDeployment(ctx context.Context, id int64) error {
	return c.do(ctx, http.MethodDelete, "/api/deployments/"+strconv.FormatInt(id, 10), nil, nil, nil)
}

// EvaluationError is returned by EvaluateDeployment when the deployment was
// found and the request was well-formed, but evaluation itself failed (e.g.
// a UNIQUE hit policy table with overlapping rules, or a timeout). Trace
// still reflects whichever rules matched before the failure.
type EvaluationError struct {
	Message string
	Trace   []DecisionTrace
}

func (e *EvaluationError) Error() string { return fmt.Sprintf("dmn: evaluation failed: %s", e.Message) }

// EvaluateDeployment evaluates inputs against a deployed DMN by id and
// records the call in that deployment's evaluation history. It maps to
// POST /api/deployments/{id}/evaluate.
//
// A non-nil *EvaluationError means the deployment ran but produced an
// error (as opposed to *APIError, which means the request itself failed,
// e.g. deployment not found).
func (c *Client) EvaluateDeployment(ctx context.Context, id int64, inputs map[string]any) (EvaluationResult, error) {
	body, err := json.Marshal(struct {
		Inputs map[string]any `json:"inputs"`
	}{Inputs: inputs})
	if err != nil {
		return EvaluationResult{}, err
	}

	path := "/api/deployments/" + strconv.FormatInt(id, 10) + "/evaluate"
	status, respBody, err := c.request(ctx, http.MethodPost, path, nil, body)
	if err != nil {
		return EvaluationResult{}, err
	}

	var res struct {
		Outputs map[string]any  `json:"outputs,omitempty"`
		Trace   []DecisionTrace `json:"trace,omitempty"`
		Error   string          `json:"error,omitempty"`
	}
	if status == http.StatusUnprocessableEntity || status == http.StatusGatewayTimeout {
		if err := json.Unmarshal(respBody, &res); err != nil {
			return EvaluationResult{}, fmt.Errorf("dmn: decode response: %w", err)
		}
		return EvaluationResult{Trace: res.Trace}, &EvaluationError{Message: res.Error, Trace: res.Trace}
	}
	if status < 200 || status >= 300 {
		return EvaluationResult{}, apiErrorFromBody(status, respBody)
	}

	if err := json.Unmarshal(respBody, &res); err != nil {
		return EvaluationResult{}, fmt.Errorf("dmn: decode response: %w", err)
	}
	return EvaluationResult{Outputs: res.Outputs, Trace: res.Trace}, nil
}

// ListEvaluations returns a page of past evaluation calls for a deployment,
// newest first. It maps to GET /api/deployments/{id}/evaluations.
func (c *Client) ListEvaluations(ctx context.Context, deploymentID int64, opts ListOptions) (EvaluationPage, error) {
	var page EvaluationPage
	path := "/api/deployments/" + strconv.FormatInt(deploymentID, 10) + "/evaluations"
	err := c.do(ctx, http.MethodGet, path, paginationQuery(opts), nil, &page)
	return page, err
}
