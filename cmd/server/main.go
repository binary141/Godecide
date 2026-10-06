package main

import (
	"context"
	"embed"
	"errors"
	"flag"
	"io/fs"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"Godecide/db"
	"Godecide/deployments"
	"Godecide/engine"

	"github.com/gin-gonic/gin"
)

const shutdownTimeout = 10 * time.Second

//go:embed web
var webFS embed.FS

type evaluateRequest struct {
	Spec   GraphSpec      `json:"spec"`
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

	outputs, err := def.EvaluateTimeout(req.Inputs, engine.DefaultEvaluationTimeout)
	if errors.Is(err, engine.ErrEvaluationTimeout) {
		c.JSON(http.StatusGatewayTimeout, evaluateResponse{Error: err.Error()})
		return
	}
	if err != nil {
		c.JSON(http.StatusUnprocessableEntity, evaluateResponse{Error: err.Error()})
		return
	}

	c.JSON(http.StatusOK, evaluateResponse{Outputs: outputs})
}

func handleExport(c *gin.Context) {
	var spec GraphSpec
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

	if err := db.Connect(); err != nil {
		log.Fatalf("db connect: %v", err)
	}
	if err := db.RunMigrations(); err != nil {
		log.Fatalf("db migrations: %v", err)
	}

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	router := gin.Default()
	router.GET("/healthz", handleHealthcheck)
	router.POST("/api/evaluate", handleEvaluate)
	router.POST("/api/export", handleExport)
	router.POST("/api/deployments", deployments.Create)
	router.GET("/api/deployments", deployments.List)
	router.GET("/api/deployments/latest", deployments.GetLatestByName)
	router.GET("/api/deployments/:deploymentId", deployments.Get)
	router.GET("/api/deployments/:deploymentId/decisions", deployments.Decisions)
	router.DELETE("/api/deployments/:deploymentId", deployments.Delete)
	router.POST("/api/deployments/:deploymentId/evaluate", deployments.Evaluate)
	router.POST("/api/deployments/:deploymentId/evaluate/batch", deployments.BatchEvaluate)
	router.GET("/api/deployments/:deploymentId/evaluations", deployments.EvaluationHistory)
	router.NoRoute(gin.WrapH(http.FileServer(http.FS(static))))

	srv := &http.Server{
		Addr:    *addr,
		Handler: router,
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	go func() {
		log.Printf("dmn table builder listening on %s", *addr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("listen: %v", err)
		}
	}()

	<-ctx.Done()
	stop()
	log.Println("shutting down")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		log.Printf("graceful shutdown failed: %v", err)
	}

	if err := db.DB.Close(); err != nil {
		log.Printf("db close: %v", err)
	}
}
