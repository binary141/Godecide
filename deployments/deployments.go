// Package deployments implements HTTP handlers for ingesting DMN files,
// storing them in the deployments table, and evaluating inputs against a
// previously deployed DMN.
package deployments

import (
	"database/sql"
	"errors"
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

	deployment, err := db.CreateDeployment(def.Name, def.Namespace, def.Version, string(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}

	c.JSON(http.StatusCreated, deployment)
}

// List returns all deployments, newest first, without their XML bodies.
func List(c *gin.Context) {
	deploymentList, err := db.ListDeployments()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, deploymentList)
}

// Get returns a single deployment, including its XML body.
func Get(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	deployment, err := db.GetDeployment(id)
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

// Evaluate loads a deployment's DMN, re-parses it, and evaluates it against
// the posted inputs.
func Evaluate(c *gin.Context) {
	id, err := parseID(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid deployment id"})
		return
	}

	deployment, err := db.GetDeployment(id)
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
