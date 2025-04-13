// package main

// import (
// 	"context"
// 	"encoding/json"
// 	"fmt"
// 	"net/http"
// 	"os"
// 	"path/filepath"
// 	"strings"
// 	"time"

// 	"github.com/docker/docker/api/types/image"
// 	"github.com/docker/docker/client"
// 	"helm.sh/helm/v3/pkg/chart"
// 	"helm.sh/helm/v3/pkg/chart/loader"
// 	"helm.sh/helm/v3/pkg/cli"
// 	"helm.sh/helm/v3/pkg/engine"
// 	"helm.sh/helm/v3/pkg/getter"
// 	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
// 	"k8s.io/apimachinery/pkg/util/yaml"
// )

// // API Types
// type ImageInfo struct {
// 	Name   string `json:"name"`
// 	Size   int64  `json:"size_bytes"`
// 	Layers int    `json:"layer_count"`
// }

// type ChartRequest struct {
// 	ChartURL string `json:"chart_url"`
// }

// type ChartResponse struct {
// 	Images []ImageInfo `json:"images"`
// 	Error  string      `json:"error,omitempty"`
// }

// // Global Docker client
// var dockerClient *client.Client

// func main() {
// 	// Initialize Docker client
// 	var err error
// 	dockerClient, err = client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
// 	if err != nil {
// 		panic(fmt.Sprintf("Failed to create Docker client: %v", err))
// 	}

// 	// API endpoint
// 	http.HandleFunc("/analyze", analyzeHandler)
// 	fmt.Println("Server running on :8080")
// 	http.ListenAndServe(":8080", nil)
// }

// func analyzeHandler(w http.ResponseWriter, r *http.Request) {
// 	if r.Method != http.MethodPost {
// 		http.Error(w, "Only POST requests allowed", http.StatusMethodNotAllowed)
// 		return
// 	}

// 	var req ChartRequest
// 	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
// 		sendError(w, "Invalid request body", http.StatusBadRequest)
// 		return
// 	}

// 	// Process chart
// 	images, err := processHelmChart(req.ChartURL)
// 	if err != nil {
// 		sendError(w, err.Error(), http.StatusInternalServerError)
// 		return
// 	}

// 	// Return results
// 	w.Header().Set("Content-Type", "application/json")
// 	json.NewEncoder(w).Encode(ChartResponse{Images: images})
// }

// func processHelmChart(chartURL string) ([]ImageInfo, error) {
// 	// 1. Download chart
// 	chart, err := downloadChart(chartURL)
// 	if err != nil {
// 		return nil, fmt.Errorf("chart download failed: %v", err)
// 	}

// 	// 2. Render templates
// 	manifests, err := engine.Render(chart, chart.Values)
// 	if err != nil {
// 		return nil, fmt.Errorf("template rendering failed: %v", err)
// 	}

// 	// 3. Extract images
// 	imageNames := parseImagesFromManifests(manifests)
// 	if len(imageNames) == 0 {
// 		return nil, fmt.Errorf("no container images found in chart")
// 	}

// 	// 4. Inspect images
// 	return inspectImages(imageNames)
// }

// // Chart Downloader
// func downloadChart(chartURL string) (*chart.Chart, error) {
//     // Get current executable directory
//     exePath, err := os.Executable()
//     if err != nil {
//         return nil, fmt.Errorf("failed to get executable path: %v", err)
//     }
//     exeDir := filepath.Dir(exePath)

//     // Create chart directory (same location as main.go)
//     chartDir := filepath.Join(exeDir, "helm-chart-cache")
//     if err := os.MkdirAll(chartDir, 0755); err != nil {
//         return nil, fmt.Errorf("failed to create chart directory: %v", err)
//     }

//     // Download using Helm getter
//     providers := getter.All(cli.New())
//     g, err := providers.ByScheme(parseScheme(chartURL))
//     if err != nil {
//         return nil, err
//     }

//     buf, err := g.Get(chartURL, getter.WithTimeout(120*time.Second))
//     if err != nil {
//         return nil, err
//     }

//     // Save to file in our chart directory
//     chartFile := filepath.Join(chartDir, "downloaded-chart.tgz")
//     if err := os.WriteFile(chartFile, buf.Bytes(), 0644); err != nil {
//         return nil, err
//     }

//     // Load chart
//     return loader.Load(chartFile)
// }

// func parseScheme(url string) string {
// 	if strings.HasPrefix(url, "oci://") {
// 		return "oci"
// 	} else if strings.HasPrefix(url, "git+") {
// 		return "git"
// 	}
// 	return "http" // Default
// }

// // Image Extraction
// func parseImagesFromManifests(manifests map[string]string) []string {
// 	var images []string
// 	for _, manifest := range manifests {
// 		docs := strings.Split(manifest, "---")
// 		for _, doc := range docs {
// 			if strings.TrimSpace(doc) == "" {
// 				continue
// 			}

// 			var obj unstructured.Unstructured
// 			if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
// 				continue
// 			}

// 			// Check in containers and initContainers
// 			for _, field := range []string{"containers", "initContainers"} {
// 				if containers, found, _ := unstructured.NestedSlice(obj.Object, "spec", field); found {
// 					extractImages(containers, &images)
// 				}
// 				if containers, found, _ := unstructured.NestedSlice(obj.Object, "spec", "template", "spec", field); found {
// 					extractImages(containers, &images)
// 				}
// 			}
// 		}
// 	}
// 	return unique(images)
// }

// func extractImages(containers []interface{}, images *[]string) {
// 	for _, c := range containers {
// 		if container, ok := c.(map[string]interface{}); ok {
// 			if image, ok := container["image"].(string); ok {
// 				*images = append(*images, image)
// 			}
// 		}
// 	}
// }

// func unique(items []string) []string {
// 	seen := make(map[string]bool)
// 	var result []string
// 	for _, item := range items {
// 		if !seen[item] {
// 			seen[item] = true
// 			result = append(result, item)
// 		}
// 	}
// 	return result
// }

// // Docker Inspection
// func inspectImages(imageNames []string) ([]ImageInfo, error) {
// 	var results []ImageInfo
// 	for _, img := range imageNames {
// 		// Pull image
// 		out, err := dockerClient.ImagePull(context.Background(), img, image.PullOptions{})
// 		if err != nil {
// 			fmt.Printf("Warning: Failed to pull %s: %v\n", img, err)
// 			continue
// 		}
// 		out.Close()

// 		// Inspect
// 		inspected, _, err := dockerClient.ImageInspectWithRaw(context.Background(), img)
// 		if err != nil {
// 			fmt.Printf("Warning: Failed to inspect %s: %v\n", img, err)
// 			continue
// 		}

// 		results = append(results, ImageInfo{
// 			Name:   img,
// 			Size:   inspected.Size,
// 			Layers: len(inspected.RootFS.Layers),
// 		})
// 	}
// 	return results, nil
// }

// // Helpers
// func sendError(w http.ResponseWriter, message string, code int) {
// 	w.Header().Set("Content-Type", "application/json")
// 	w.WriteHeader(code)
// 	json.NewEncoder(w).Encode(ChartResponse{Error: message})
// }