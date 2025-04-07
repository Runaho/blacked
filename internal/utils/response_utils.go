package utils

import (
	"blacked/internal/config"
	"encoding/json"
	"errors"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rs/zerolog/log"
)

// Error variables for response utils
var (
	ErrMetadataFileNotFound   = errors.New("metadata file not found")
	ErrDecodeMetadataFile     = errors.New("failed to decode metadata from file")
	ErrStoredResponseTooOld   = errors.New("stored response is too old")
	ErrOpenDataFile           = errors.New("failed to open stored response data file")
	ErrFetchSourceResponse    = errors.New("error fetching response from source")
	ErrSaveResponseDir        = errors.New("failed to create directory for response files")
	ErrCreateResponseDataFile = errors.New("failed to create response data file")
	ErrWriteResponseData      = errors.New("failed to write response data to file")
	ErrMarshalMetadataJSON    = errors.New("failed to marshal metadata to JSON")
	ErrWriteMetadataFile      = errors.New("failed to write metadata to file")
	ErrRemoveResponseDataFile = errors.New("failed to remove stored response data file")
	ErrRemoveResponseMetaFile = errors.New("failed to remove stored response metadata file")
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
			log.Info().Str("file", dataFilename).Str("process_id", meta.ProcessID).Msg("Using stored response")
			return reader, meta, nil
		}
		log.Warn().Err(err).Str("file", dataFilename).Msg("Stored response not found or invalid, fetching from source")
	}

	// Fetch data from source using the provided fetch function
	responseReader, fetchErr := fetchFunc()
	if fetchErr != nil {
		log.Err(fetchErr).Str("url", sourceURL).Str("provider", providerName).Msg("Failed to fetch response from source")
		return nil, nil, ErrFetchSourceResponse
	}

	if storeResponses {
		metadata := ResponseMetadata{
			ProcessID: processID,
			CreatedAt: time.Now(),
		}
		description := strings.Join([]string{"Response from", providerName, "sync run at", time.Now().Format(time.RFC3339)}, " ")
		metadata.Description = description

		if err := saveResponseToFile(dataFilename, metaFilename, responseReader, metadata); err != nil {
			log.Err(err).Str("dataFile", dataFilename).Str("metaFile", metaFilename).Msg("Failed to save response to file")
		} else {
			log.Info().Str("dataFile", dataFilename).Str("metaFile", metaFilename).Str("processID", metadata.ProcessID).Msg("Response saved to file")
		}

		// Get a fresh reader from the saved file to avoid consuming the original reader
		i, _, e := getStoredResponse(dataFilename, metaFilename) // Return stored response reader after saving (or attempting to save)
		return i, &metadata, e
	}

	return responseReader, nil, nil // Return reader from fetched response (without saving)
}

// getStoredResponse attempts to open and return a reader for the stored response and metadata.
func getStoredResponse(dataFilename string, metaFilename string) (io.Reader, *ResponseMetadata, error) {
	metaFile, err := os.Open(metaFilename)
	if err != nil {
		log.Err(err).Str("file", metaFilename).Msg("Metadata file not found")
		return nil, nil, ErrMetadataFileNotFound
	}
	defer metaFile.Close()
	metadata := &ResponseMetadata{}
	if err := json.NewDecoder(metaFile).Decode(metadata); err != nil {
		log.Err(err).Str("file", metaFilename).Msg("Failed to decode metadata from file")
		return nil, nil, ErrDecodeMetadataFile
	}

	if time.Since(metadata.CreatedAt) > 24*time.Hour {
		log.Info().
			Time("created", metadata.CreatedAt).
			Str("processID", metadata.ProcessID).
			Msg("Stored response is too old")
		return nil, metadata, ErrStoredResponseTooOld
	}

	dataFile, err := os.Open(dataFilename)
	if err != nil {
		log.Err(err).Str("file", dataFilename).Msg("Failed to open stored response data file")
		return nil, metadata, ErrOpenDataFile
	}
	return dataFile, metadata, nil
}

// saveResponseToFile saves the response io.Reader to a file and metadata to a separate metadata file.
func saveResponseToFile(dataFilename string, metaFilename string, data io.Reader, metadata ResponseMetadata) error {
	dir := filepath.Dir(dataFilename)
	if err := os.MkdirAll(dir, 0755); err != nil {
		log.Err(err).Str("directory", dir).Msg("Failed to create directory for response files")
		return ErrSaveResponseDir
	}

	dataFile, err := os.Create(dataFilename) // Create data file for writing
	if err != nil {
		log.Err(err).Str("file", dataFilename).Msg("Failed to create response data file")
		return ErrCreateResponseDataFile
	}
	defer dataFile.Close()

	if _, err := io.Copy(dataFile, data); err != nil { // Copy from io.Reader to file
		log.Err(err).Str("file", dataFilename).Msg("Failed to write response data to file")
		return ErrWriteResponseData
	}

	metaJSON, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		log.Err(err).Interface("metadata", metadata).Msg("Failed to marshal metadata to JSON")
		return ErrMarshalMetadataJSON
	}
	if err := ioutil.WriteFile(metaFilename, metaJSON, 0644); err != nil {
		log.Err(err).Str("file", metaFilename).Msg("Failed to write metadata to file")
		return ErrWriteMetadataFile
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
		log.Info().Str("file", dataFilename).Msg("Removed stored response data file")
	} else if !os.IsNotExist(err) { // Log error only if it's not "file not exists"
		log.Err(err).Str("file", dataFilename).Msg("Failed to remove stored response data file")
		return ErrRemoveResponseDataFile
	}

	if err := os.Remove(metaFilename); err == nil {
		log.Info().Str("file", metaFilename).Msg("Removed stored response metadata file")
	} else if !os.IsNotExist(err) { // Log error only if it's not "file not exists"
		log.Err(err).Str("file", metaFilename).Msg("Failed to remove stored response metadata file")
		return ErrRemoveResponseMetaFile
	}

	return nil
}
