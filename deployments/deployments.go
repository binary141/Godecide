// Package deployments implements HTTP handlers for ingesting DMN files,
// storing them in the deployments table, and evaluating inputs against a
// previously deployed DMN.
package deployments

import (
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"

	"dmn/db"
	"dmn/engine"

	"github.com/gin-gonic/gin"
)

type evaluateRequest struct {
	Inputs map[string]any `json:"inputs"`
}

type evaluateResponse struct {
	Outputs map[string]any `json:"outputs,omitempty"`
	Error   string         `json:"error,omitempty"`
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

	outputs, err := def.Evaluate(req.Inputs)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, evaluateResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, evaluateResponse{Outputs: outputs})
}

func parseID(c *gin.Context) (int64, error) {
	return strconv.ParseInt(c.Param("deploymentId"), 10, 64)
}
