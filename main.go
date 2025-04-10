package main

import (
	"encoding/json"
	"fmt"
	"net/http"
)

type ChartRequest struct {
	URL string `json:"url"`
}

func analyzeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Only POST method is allowed", http.StatusMethodNotAllowed)
		return
	}

	var req ChartRequest
	err := json.NewDecoder(r.Body).Decode(&req)
	if err != nil || req.URL == "" {
		http.Error(w, "Invalid JSON or missing URL", http.StatusBadRequest)
		return
	}

	fmt.Fprintf(w, "Received Helm chart URL: %s\n", req.URL)
}

func main() {
	http.HandleFunc("/analyze", analyzeHandler)
	fmt.Println("Server running on http://localhost:8080")
	http.ListenAndServe(":8080", nil)
}
