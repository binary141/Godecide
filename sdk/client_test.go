package sdk

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestEvaluate(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/evaluate", r.URL.Path)
		var req evaluateGraphRequest
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.Equal(t, "n1", req.Spec.Nodes[0].ID)
		require.Equal(t, "Bob", req.Inputs["name"])
		json.NewEncoder(w).Encode(map[string]any{"outputs": map[string]any{"greeting": "Hello Bob"}})
	}))
	defer srv.Close()

	c := New(srv.URL)
	outputs, err := c.Evaluate(context.Background(), GraphSpec{
		Nodes: []NodeSpec{{ID: "n1", DecisionName: "greet"}},
	}, map[string]any{"name": "Bob"})
	require.NoError(t, err)
	require.Equal(t, "Hello Bob", outputs["greeting"])
}

func TestAPIErrorSurfacesProblems(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{
			"error":    "dmn file failed validation",
			"problems": []string{"cycle detected"},
		})
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.CreateDeployment(context.Background(), "<definitions/>")
	require.Error(t, err)

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	require.Equal(t, http.StatusUnprocessableEntity, apiErr.StatusCode)
	require.Contains(t, apiErr.Message, "validation")
	require.Contains(t, apiErr.Problems, "cycle detected")
}

func TestEvaluateDeploymentSurfacesEvaluationError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/api/deployments/42/evaluate", r.URL.Path)
		w.WriteHeader(http.StatusUnprocessableEntity)
		json.NewEncoder(w).Encode(map[string]any{"error": "no rule matched"})
	}))
	defer srv.Close()

	c := New(srv.URL)
	_, err := c.EvaluateDeployment(context.Background(), 42, map[string]any{})
	require.Error(t, err)

	var evalErr *EvaluationError
	require.ErrorAs(t, err, &evalErr)
	require.Equal(t, "no rule matched", evalErr.Message)
}

func TestListDeploymentsPagination(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "5", r.URL.Query().Get("limit"))
		require.Equal(t, "10", r.URL.Query().Get("offset"))
		json.NewEncoder(w).Encode(map[string]any{"deployments": []any{}, "total": 0, "limit": 5, "offset": 10})
	}))
	defer srv.Close()

	c := New(srv.URL)
	page, err := c.ListDeployments(context.Background(), ListOptions{Limit: 5, Offset: 10})
	require.NoError(t, err)
	require.Equal(t, 5, page.Limit)
	require.Equal(t, 10, page.Offset)
}

func TestDeleteDeploymentNoContent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodDelete, r.Method)
		w.WriteHeader(http.StatusNoContent)
	}))
	defer srv.Close()

	c := New(srv.URL)
	require.NoError(t, c.DeleteDeployment(context.Background(), 1))
}

func TestHealthcheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/healthz", r.URL.Path)
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	require.NoError(t, New(srv.URL).Healthcheck(context.Background()))
}
