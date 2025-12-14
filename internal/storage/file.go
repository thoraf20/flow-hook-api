package storage

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/google/uuid"
)

const (
	DataDir      = "./data"
	RequestsDir  = "requests"
	MaxBodySize  = 10 * 1024 * 1024 // 10MB
)

// EnsureDir creates the directory structure if it doesn't exist
func EnsureDir(path string) error {
	return os.MkdirAll(path, 0755)
}

// StoreRequestBody stores a request body to the file system
// Returns the file path relative to data directory
func StoreRequestBody(endpointID, requestID uuid.UUID, body []byte) (string, error) {
	if len(body) > MaxBodySize {
		return "", fmt.Errorf("body size %d exceeds maximum %d", len(body), MaxBodySize)
	}

	// Create directory structure: data/requests/{endpoint_id}/
	endpointDir := filepath.Join(DataDir, RequestsDir, endpointID.String())
	if err := EnsureDir(endpointDir); err != nil {
		return "", fmt.Errorf("failed to create endpoint directory: %w", err)
	}

	// Store file: {request_id}.body
	filename := fmt.Sprintf("%s.body", requestID.String())
	filePath := filepath.Join(endpointDir, filename)

	if err := os.WriteFile(filePath, body, 0644); err != nil {
		return "", fmt.Errorf("failed to write body file: %w", err)
	}

	// Return relative path from data directory
	relativePath := filepath.Join(RequestsDir, endpointID.String(), filename)
	return relativePath, nil
}

// ReadRequestBody reads a request body from the file system
func ReadRequestBody(bodyPath string) ([]byte, error) {
	fullPath := filepath.Join(DataDir, bodyPath)
	return os.ReadFile(fullPath)
}

// DeleteRequestBody deletes a request body file
func DeleteRequestBody(bodyPath string) error {
	fullPath := filepath.Join(DataDir, bodyPath)
	return os.Remove(fullPath)
}

