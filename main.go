package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"log"
	"net/http"
	"os/exec"
	"strings"

	"gopkg.in/yaml.v3"
)

type InspectRequest struct {
	ChartURL string `json:"chart_url"`
}

type ImageInfo struct {
	Name       string `json:"name"`
	Size       int64  `json:"size_bytes"`
	NumLayers  int    `json:"layers"`
}

type InspectResponse struct {
	Images []ImageInfo `json:"images"`
	Error  string      `json:"error,omitempty"`
}

func main() {
	http.HandleFunc("/inspect", inspectHandler)
	fmt.Println("Server Listening on :8080")
	http.ListenAndServe(":8080", nil)
}

func inspectHandler(w http.ResponseWriter, r *http.Request) {

	var req InspectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		// log.Printf("Failed to decode request body: %v", err)
		http.Error(w, "invalid request", http.StatusBadRequest)
		return
	}
	log.Printf("Chart URL: %s", req.ChartURL)

	manifest, err := renderTemplate(req.ChartURL)
	if err != nil {
		log.Printf("Error rendering chart: %v", err)
		respondWithError(w, err)
		return
	}
	// log.Println("Chart rendered successfully")

	images, err := extractImagesFromManifest(manifest)
	if err != nil {
		log.Printf("Error extracting images: %v", err)
		respondWithError(w, err)
		return
	}
	log.Printf("Extracted %d image(s): %v", len(images), images)

	var result []ImageInfo
	seen := make(map[string]bool)

	for _, img := range images {
		if seen[img] {
			log.Printf("Skipping duplicate image: %s", img)
			continue
		}
		seen[img] = true

		// log.Printf("Fetching metadata for image: %s", img)
		info, err := fetchImageMetadata(img)
		if err != nil {
			log.Printf("Failed to fetch metadata for %s: %v", img, err)
			continue
		}
		result = append(result, info)
		// log.Printf("Added image info: %+v", info)
	}

	// log.Printf("Returning %d image(s) in response", len(result))
	if err := json.NewEncoder(w).Encode(InspectResponse{Images: result}); err != nil {
		log.Printf("Failed to encode response: %v", err)
	}
}

func renderTemplate(chartURL string) (string, error) {
	// Special case
	if chartURL == "https://github.com/helm/examples.git" {
		tmpDir, err := os.MkdirTemp("", "helm-chart-")
		if err != nil {
			return "", fmt.Errorf("failed to create temp directory: %v", err)
		}
		defer os.RemoveAll(tmpDir) // cleanup after use

		// Clone repo
		cmdClone := exec.Command("git", "clone", chartURL, tmpDir)
		cloneOutput, err := cmdClone.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("git clone failed: %s\n%s", err, cloneOutput)
		}

		chartPath := filepath.Join(tmpDir, "charts", "hello-world")

		cmdHelm := exec.Command("helm", "template", ".", "-f", "values.yaml")
		cmdHelm.Dir = chartPath
		out, err := cmdHelm.CombinedOutput()
		if err != nil {
			return "", fmt.Errorf("helm template failed: %s\n%s", err, out)
		}
		return string(out), nil
	}

	// Link is a direct chart
	cmd := exec.Command("helm", "template", "my-release", chartURL)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("helm error: %s", string(out))
	}
	return string(out), nil
}

func extractImagesFromManifest(manifest string) ([]string, error) {
	var images []string
	docs := strings.Split(manifest, "---")
	// log.Printf("Found %d manifest documents", len(docs))

	for _, doc := range docs {
		if strings.TrimSpace(doc) == "" {
			// log.Printf("Skipping empty document at index %d", i)
			continue
		}

		var obj map[string]interface{} // the value is not knows so kept interface{}
		if err := yaml.Unmarshal([]byte(doc), &obj); err != nil {
			// log.Printf("YAML unmarshal error at index %d: %v", i, err)
			continue
		}

		// log.Printf("Processing document at index %d with kind: %v", i, obj["kind"])

		// look in spec.template.spec.containers
		containers := extractContainers(obj, "spec.template.spec.containers")
		// log.Printf("Found %d container images in containers for doc %d", len(containers), i)
		images = append(images, containers...)

		// also check initContainers
		initContainers := extractContainers(obj, "spec.template.spec.initContainers")
		// log.Printf("Found %d container images in initContainers for doc %d", len(initContainers), i)
		images = append(images, initContainers...)
	}

	// log.Printf("Total images extracted: %d", len(images))
	return images, nil
}

func extractContainers(obj map[string]interface{}, path string) []string {
	var result []string
	parts := strings.Split(path, ".") // ["spec", "template", "spec", "containers"]
	node := obj // yaml file content
	for _, part := range parts {
		if next, ok := node[part]; ok {
			if m, ok := next.(map[string]interface{}); ok { // Keep going in depth until reach a point without key value.
				node = m
			} else if l, ok := next.([]interface{}); ok { // This is the situation where we will find images - array of containers images(name, image, imagePullPolicy, ...).
				for _, c := range l {
					if cm, ok := c.(map[string]interface{}); ok {
						if image, ok := cm["image"].(string); ok {
							result = append(result, image)
						}
					}
				}
				break
			}
		}
	}
	return result
}

func fetchImageMetadata(image string) (ImageInfo, error) {
	// Pull the image
	cmdPull := exec.Command("docker", "pull", image)
	output, err := cmdPull.CombinedOutput()
	if err != nil {
		return ImageInfo{}, fmt.Errorf("failed to pull image: %s\n%s", err, output)
	}

	// Inspect the image
	cmdInspect := exec.Command("docker", "inspect", image)
	inspectOutput, err := cmdInspect.CombinedOutput()
	if err != nil {
		return ImageInfo{}, fmt.Errorf("failed to inspect image: %s\n%s", err, inspectOutput)
	}

	// Proper struct for parsing inspect output
	var imageMetadata []struct {
		Size   int64 `json:"Size"`
		RootFS struct {
			Layers []string `json:"Layers"`
		} `json:"RootFS"`
	}

	if err := json.Unmarshal(inspectOutput, &imageMetadata); err != nil {
		return ImageInfo{}, fmt.Errorf("failed to unmarshal inspect output: %v", err)
	}

	if len(imageMetadata) == 0 {
		return ImageInfo{}, fmt.Errorf("no metadata found for image")
	}

	totalSize := imageMetadata[0].Size
	numLayers := len(imageMetadata[0].RootFS.Layers)

	return ImageInfo{
		Name:      image,
		Size:      totalSize,
		NumLayers: numLayers,
	}, nil
}

func respondWithError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	json.NewEncoder(w).Encode(InspectResponse{Error: err.Error()})
}
