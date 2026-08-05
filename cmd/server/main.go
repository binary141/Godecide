package main

import (
	"embed"
	"encoding/json"
	"flag"
	"io/fs"
	"log"
	"net/http"
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

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func handleEvaluate(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req evaluateRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, evaluateResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	def := buildDefinitions(req.Spec)

	outputs, err := def.Evaluate(req.Inputs)
	if err != nil {
		writeJSON(w, http.StatusUnprocessableEntity, evaluateResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, evaluateResponse{Outputs: outputs})
}

func handleExport(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var spec TableSpec
	if err := json.NewDecoder(r.Body).Decode(&spec); err != nil {
		writeJSON(w, http.StatusBadRequest, exportResponse{Error: "invalid request body: " + err.Error()})
		return
	}

	def := buildDefinitions(spec)

	xmlBytes, err := toXML(def)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, exportResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, exportResponse{XML: string(xmlBytes)})
}

func main() {
	addr := flag.String("addr", ":8080", "address to listen on")
	flag.Parse()

	static, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatal(err)
	}

	mux := http.NewServeMux()
	mux.Handle("/", http.FileServer(http.FS(static)))
	mux.HandleFunc("/api/evaluate", handleEvaluate)
	mux.HandleFunc("/api/export", handleExport)

	log.Printf("dmn table builder listening on %s", *addr)
	log.Fatal(http.ListenAndServe(*addr, mux))
}
