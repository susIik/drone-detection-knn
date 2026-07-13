package embedding

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

// PANNSClient communicates with the Python PANNS embedding service
type PANNSClient struct {
	serviceURL string
	client     *http.Client
}

// EmbeddingResponse represents the response from the embedding service
type EmbeddingResponse struct {
	Embedding []float64 `json:"embedding"`
	Dimension int       `json:"dimension"`
}

// NewPANNSClient creates a new PANNS embedding client
func NewPANNSClient(serviceURL string) *PANNSClient {
	if serviceURL == "" {
		serviceURL = "http://panns:5002"
	}

	return &PANNSClient{
		serviceURL: serviceURL,
		client: &http.Client{
			Timeout: 30 * time.Second,
		},
	}
}

// HealthCheck verifies the embedding service is running
func (pc *PANNSClient) HealthCheck() error {
	resp, err := pc.client.Get(pc.serviceURL + "/health")
	if err != nil {
		return fmt.Errorf("embedding service not reachable: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("embedding service unhealthy: status %d", resp.StatusCode)
	}

	return nil
}

// EmbedFile generates a PANNS embedding from an audio file
func (pc *PANNSClient) EmbedFile(audioPath string) ([]float64, error) {
	// Open the audio file
	file, err := os.Open(filepath.Clean(audioPath))
	if err != nil {
		return nil, fmt.Errorf("failed to open audio file: %w", err)
	}
	defer file.Close()

	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file to form
	part, err := writer.CreateFormFile("audio", filepath.Base(audioPath))
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := io.Copy(part, file); err != nil {
		return nil, fmt.Errorf("failed to copy file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Send request
	req, err := http.NewRequest("POST", pc.serviceURL+"/embed", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var embResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(embResp.Embedding) == 0 {
		return nil, fmt.Errorf("received empty embedding")
	}

	return embResp.Embedding, nil
}

// EmbedBytes generates a PANNS embedding from audio bytes
func (pc *PANNSClient) EmbedBytes(audioData []byte, filename string) ([]float64, error) {
	// Create multipart form
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	// Add file to form
	part, err := writer.CreateFormFile("audio", filename)
	if err != nil {
		return nil, fmt.Errorf("failed to create form file: %w", err)
	}

	if _, err := part.Write(audioData); err != nil {
		return nil, fmt.Errorf("failed to write file data: %w", err)
	}

	if err := writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close multipart writer: %w", err)
	}

	// Send request
	req, err := http.NewRequest("POST", pc.serviceURL+"/embed", body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())

	resp, err := pc.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("embedding service returned status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	// Parse response
	var embResp EmbeddingResponse
	if err := json.NewDecoder(resp.Body).Decode(&embResp); err != nil {
		return nil, fmt.Errorf("failed to decode response: %w", err)
	}

	if len(embResp.Embedding) == 0 {
		return nil, fmt.Errorf("received empty embedding")
	}

	return embResp.Embedding, nil
}
