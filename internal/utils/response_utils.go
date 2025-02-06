package utils

import (
	"blacked/internal/config"
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"time"

	"github.com/rs/zerolog/log"
)

// ResponseMetadata holds metadata information for a stored response.
type ResponseMetadata struct {
	ProcessID string    `json:"process_id"`
	CreatedAt time.Time `json:"created_at"`
	// You can add more metadata fields here in the future if needed.
	Description string `json:"description,omitempty"` // Example of storing a description
}

func GetResponseReader(sourceURL string, fetchFunc func() (io.Reader, error), providerName string, processID string) (io.Reader, *ResponseMetadata, error) {
	cfg := config.GetConfig()
	storePath := cfg.Collector.StorePath
	storeResponses := cfg.Collector.StoreResponses

	dataFilename, metaFilename := generateFilenames(storePath, providerName)

	if storeResponses {
		reader, meta, err := getStoredResponse(dataFilename, metaFilename)
		if err == nil {
			log.Info().Msgf("Using stored response from: %s, Process ID: %s", dataFilename, meta.ProcessID)
			return reader, meta, nil
		}
		log.Warn().Err(err).Msgf("Stored response not found or invalid (%v), fetching from source.", err)
	}

	// Fetch data from source using the provided fetch function - now returns io.Reader directly
	responseReader, fetchErr := fetchFunc()
	if fetchErr != nil {
		log.Error().Err(fetchErr).Msgf("Failed to fetch response from source: %s", sourceURL)
		return nil, nil, fmt.Errorf("error fetching response from source: %w", fetchErr)
	}

	if storeResponses {
		metadata := ResponseMetadata{
			ProcessID:   processID,
			CreatedAt:   time.Now(),
			Description: fmt.Sprintf("Response from %s sync run at %s", providerName, time.Now().Format(time.RFC3339)),
		}
		if err := saveResponseToFile(dataFilename, metaFilename, responseReader, metadata); err != nil {
			log.Error().Err(err).Msgf("Failed to save response to file %s and metadata to %s", dataFilename, metaFilename)
		} else {
			log.Info().Msgf("Response saved to file: %s and metadata to: %s, Process ID: %s", dataFilename, metaFilename, metadata.ProcessID)
		}
		i, _, e := getStoredResponse(dataFilename, metaFilename) // Return stored response reader after saving (or attempting to save)
		return i, &metadata, e
	}

	return responseReader, nil, nil // Return reader from fetched response (without saving)
}

// getStoredResponse attempts to open and return a reader for the stored response and metadata.
// No changes needed for getStoredResponse function
func getStoredResponse(dataFilename string, metaFilename string) (io.Reader, *ResponseMetadata, error) {
	metaFile, err := os.Open(metaFilename)
	if err != nil {
		return nil, nil, fmt.Errorf("metadata file not found: %w", err)
	}
	defer metaFile.Close()
	metadata := &ResponseMetadata{}
	if err := json.NewDecoder(metaFile).Decode(metadata); err != nil {
		return nil, nil, fmt.Errorf("failed to decode metadata from file: %w", err)
	}

	if time.Since(metadata.CreatedAt) > 24*time.Hour {
		return nil, metadata, fmt.Errorf("stored response is too old (created at %s, process ID: %s)", metadata.CreatedAt, metadata.ProcessID)
	}

	dataFile, err := os.Open(dataFilename)
	if err != nil {
		return nil, metadata, fmt.Errorf("failed to open stored response data file: %w", err)
	}
	return dataFile, metadata, nil
}

// saveResponseToFile saves the response io.Reader to a file and metadata to a separate metadata file.
// Modified to accept io.Reader for data parameter
func saveResponseToFile(dataFilename string, metaFilename string, data io.Reader, metadata ResponseMetadata) error {
	dir := filepath.Dir(dataFilename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return fmt.Errorf("failed to create directory for response files: %w", err)
	}

	dataFile, err := os.Create(dataFilename) // Create data file for writing
	if err != nil {
		return fmt.Errorf("failed to create response data file: %w", err)
	}
	defer dataFile.Close()

	if _, err := io.Copy(dataFile, data); err != nil { // Copy from io.Reader to file
		return fmt.Errorf("failed to write response data to file: %w", err)
	}

	metaJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal metadata to JSON: %w", err)
	}
	if err := ioutil.WriteFile(metaFilename, metaJSON, 0644); err != nil {
		return fmt.Errorf("failed to write metadata to file: %w", err)
	}

	return nil
}

// generateFilenames and RemoveStoredResponse - no changes needed
func generateFilenames(storePath string, providerName string) (dataFilename string, metaFilename string) {
	baseFilename := filepath.Join(storePath, providerName+"_response") // Base filename without extensions
	dataFilename = baseFilename + ".dat"
	metaFilename = baseFilename + ".meta.json"
	return dataFilename, metaFilename
}

// RemoveStoredResponse removes both data and metadata files for a provider.
func RemoveStoredResponse(providerName string) error {
	cfg := config.GetConfig()
	storeResponses := cfg.Collector.StoreResponses
	if !storeResponses {
		return nil
	}

	storePath := cfg.Collector.StorePath
	dataFilename, metaFilename := generateFilenames(storePath, providerName)

	if err := os.Remove(dataFilename); err == nil {
		fmt.Printf("Removed stored response data file: %s\n", dataFilename)
	} else if !os.IsNotExist(err) { // Log error only if it's not "file not exists"
		return fmt.Errorf("failed to remove stored response data file %s: %w", dataFilename, err)
	}

	if err := os.Remove(metaFilename); err == nil {
		fmt.Printf("Removed stored response metadata file: %s\n", metaFilename)
	} else if !os.IsNotExist(err) { // Log error only if it's not "file not exists"
		return fmt.Errorf("failed to remove stored response metadata file %s: %w", metaFilename, err)
	}

	return nil
}
