// Package deployments implements HTTP handlers for ingesting DMN files,
// storing them in the deployments table, and evaluating inputs against a
// previously deployed DMN.
package deployments

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"

	"dmn/db"
	"dmn/engine"

	"github.com/gin-gonic/gin"
)

type evaluateRequest struct {
	Inputs map[string]any `json:"inputs"`
}

type evaluateResponse struct {
	Outputs map[string]any         `json:"outputs,omitempty"`
	Trace   []engine.DecisionTrace `json:"trace,omitempty"`
	Error   string                 `json:"error,omitempty"`
}

// Create ingests a raw DMN XML document from the request body, parses and
// validates it, and stores it as a new deployment.
func Create(c *gin.Context) {
	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unable to read request body: " + err.Error()})
		return
	}
	if len(body) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "request body is empty"})
		return
	}

	def, err := engine.Parse(body)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "invalid dmn file: " + err.Error()})
		return
	}

	if problems := engine.ValidateDefinitions(def); len(problems) > 0 {
		c.JSON(http.StatusUnprocessableEntity, gin.H{"error": "dmn file failed validation", "problems": problems})
		return
	}

	deployment, err := db.CreateDeployment(c.Request.Context(), def.Name, def.Namespace, def.Version, string(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, deployment)
}

const (
	defaultListLimit = 20
	maxListLimit     = 100
)

// List returns a page of deployments, newest first, without their XML
// bodies. Accepts ?limit= (default 20, max 100) and ?offset= (default 0)
// query params.
func List(c *gin.Context) {
	limit, err := parseQueryInt(c, "limit", defaultListLimit)
	if err != nil || limit <= 0 || limit > maxListLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("limit must be an integer between 1 and %d", maxListLimit)})
		return
	}

	offset, err := parseQueryInt(c, "offset", 0)
	if err != nil || offset < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be a non-negative integer"})
		return
	}

	deploymentList, total, err := db.ListDeployments(c.Request.Context(), limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"deployments": deploymentList,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

func parseQueryInt(c *gin.Context, key string, fallback int) (int, error) {
	raw := c.Query(key)
	if raw == "" {
		return fallback, nil
	}
	return strconv.Atoi(raw)
}

// GetLatestByName returns the deployment for the given ?name= query param,
// including its XML body. By default it returns the highest-versioned
// (latest) deployment for that name; passing ?version= pins the lookup to
// that specific historical version instead, mirroring how engines like
// Camunda let you evaluate a specific decision version rather than just
// the newest one.
func GetLatestByName(c *gin.Context) {
	name := c.Query("name")
	if name == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "name query param is required"})
		return
	}

	var (
		deployment db.Deployment
		err        error
	)

	if raw := c.Query("version"); raw != "" {
		version, convErr := strconv.Atoi(raw)
		if convErr != nil || version <= 0 {
			c.JSON(http.StatusBadRequest, gin.H{"error": "version must be a positive integer"})
			return
		}
		deployment, err = db.GetDeploymentByNameVersion(c.Request.Context(), name, version)
	} else {
		deployment, err = db.GetLatestDeploymentByName(c.Request.Context(), name)
	}

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "no deployment found for name"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, deployment)
}

// Get returns a single deployment, including its XML body.
func Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	deployment, err := db.GetDeployment(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, deployment)
}

// Delete removes a deployment by id.
func Delete(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	found, err := db.DeleteDeployment(c.Request.Context(), id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
		return
	}

	c.Status(http.StatusNoContent)
}

// Evaluate loads a deployment's DMN, re-parses it, and evaluates it against
// the posted inputs.
func Evaluate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	deployment, err := db.GetDeployment(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	var req evaluateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, evaluateResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	def, err := engine.Parse([]byte(deployment.XML))
	if err != nil {
		c.JSON(http.StatusInternalServerError, evaluateResponse{Error: "unable to parse stored dmn: " + err.Error()})
		return
	}

	outputs, trace, evalErr := def.EvaluateWithTraceTimeout(req.Inputs, engine.DefaultEvaluationTimeout)

	if errors.Is(evalErr, engine.ErrEvaluationTimeout) {
		c.JSON(http.StatusGatewayTimeout, evaluateResponse{Error: evalErr.Error()})
		return
	}

	if err := db.RecordEvaluation(c.Request.Context(), deployment.ID, req.Inputs, outputs, trace, evalErr); err != nil {
		log.Printf("record evaluation for deployment %d: %v", deployment.ID, err)
	}

	if evalErr != nil {
		c.JSON(http.StatusUnprocessableEntity, evaluateResponse{Error: evalErr.Error(), Trace: trace})
		return
	}

	c.JSON(http.StatusOK, evaluateResponse{Outputs: outputs, Trace: trace})
}

// requirementView describes one edge into a decision: either an external
// input or another decision it depends on.
type requirementView struct {
	Type string `json:"type"` // "input" or "decision"
	Ref  string `json:"ref"`
}

// decisionView is a read-only, cockpit-friendly rendering of a single
// <decision>, carrying just enough of its decision table(s) to display them.
type decisionView struct {
	ID       string                 `json:"id"`
	Name     string                 `json:"name"`
	Variable engine.Variable        `json:"variable"`
	Requires []requirementView      `json:"requires"`
	Tables   []engine.DecisionTable `json:"tables"`
}

// decisionsResponse is the payload for GET .../decisions: the parsed
// decision requirement graph of a deployment, for read-only display.
type decisionsResponse struct {
	Name      string             `json:"name"`
	Namespace string             `json:"namespace"`
	Version   string             `json:"dmnVersion"`
	InputData []engine.InputData `json:"inputData"`
	Decisions []decisionView     `json:"decisions"`
}

// Decisions parses a deployment's stored DMN XML and returns its decisions
// and their decision tables, for read-only display (e.g. a Camunda
// Cockpit-style definition view) rather than evaluation.
func Decisions(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	deployment, err := db.GetDeployment(c.Request.Context(), id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			c.JSON(http.StatusNotFound, gin.H{"error": "deployment not found"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	def, err := engine.Parse([]byte(deployment.XML))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "unable to parse stored dmn: " + err.Error()})
		return
	}

	decisions := make([]decisionView, len(def.Decisions))
	for i, d := range def.Decisions {
		var requires []requirementView
		for _, ir := range d.InformationRequirements {
			switch {
			case ir.RequiredInput != nil:
				requires = append(requires, requirementView{Type: "input", Ref: strings.TrimPrefix(ir.RequiredInput.Href, "#")})
			case ir.RequiredDecision != nil:
				requires = append(requires, requirementView{Type: "decision", Ref: strings.TrimPrefix(ir.RequiredDecision.Href, "#")})
			}
		}

		decisions[i] = decisionView{
			ID:       d.ID,
			Name:     d.Name,
			Variable: d.Variable,
			Requires: requires,
			Tables:   d.DecisionTables,
		}
	}

	c.JSON(http.StatusOK, decisionsResponse{
		Name:      def.Name,
		Namespace: def.Namespace,
		Version:   def.Version,
		InputData: def.InputData,
		Decisions: decisions,
	})
}

// EvaluationHistory returns a page of past evaluation calls for a
// deployment, newest first, so callers can ask what a decision returned for
// given inputs at some point in the past.
func EvaluationHistory(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	limit, err := parseQueryInt(c, "limit", defaultListLimit)
	if err != nil || limit <= 0 || limit > maxListLimit {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("limit must be an integer between 1 and %d", maxListLimit)})
		return
	}

	offset, err := parseQueryInt(c, "offset", 0)
	if err != nil || offset < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "offset must be a non-negative integer"})
		return
	}

	evaluations, total, err := db.ListEvaluations(c.Request.Context(), id, limit, offset)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"evaluations": evaluations,
		"total":       total,
		"limit":       limit,
		"offset":      offset,
	})
}

func parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("deploymentId"), 10, 64)
}
