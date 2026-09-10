package main

import (
	"embed"
	"flag"
	"io/fs"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)

//go:embed web
var webFS embed.FS

type evaluateRequest struct {
	Spec   TableSpec      `json:"spec"`
	Inputs map[string]any `json:"inputs"`
}

type evaluateResponse struct {
	Outputs map[string]any `json:"outputs,omitempty"`
	Error   string         `json:"error,omitempty"`
}

type exportResponse struct {
	XML   string `json:"xml,omitempty"`
	Error string `json:"error,omitempty"`
}

func handleHealthcheck(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func handleEvaluate(c *gin.Context) {
	var req evaluateRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, evaluateResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	def := buildDefinitions(req.Spec)

	outputs, err := def.Evaluate(req.Inputs)
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, evaluateResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, evaluateResponse{Outputs: outputs})
}

func handleExport(c *gin.Context) {
	var spec TableSpec
	if err := c.ShouldBindJSON(&spec); err != nil {
		c.JSON(http.StatusBadRequest, exportResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	def := buildDefinitions(spec)

	xmlBytes, err := toXML(def)
	if err != nil {
		c.JSON(http.StatusInternalServerError, exportResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, exportResponse{XML: string(xmlBytes)})
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	router := gin.Default()
	router.GET("/healthz", handleHealthcheck)
	router.POST("/api/evaluate", handleEvaluate)
	router.POST("/api/export", handleExport)
	router.NoRoute(gin.WrapH(http.FileServer(http.FS(static))))

	log.Printf("dmn table builder listening on %s", *addr)
	log.Fatal(router.Run(*addr))
}
