package provider

import (
	"blacked/cmd/provider_processor"
	"net/http"
	"sync"
	"time"

	"github.com/labstack/echo/v4"
	"github.com/rs/xid"
)

// ProcessStatus holds the status of a provider processing task.
type ProcessStatus struct {
	ID               string              `json:"id"`
	Status           string              `json:"status"` // "running", "completed", "failed"
	StartTime        time.Time           `json:"start_time"`
	EndTime          time.Time           `json:"end_time,omitempty"`
	ProvidersProcessed []string          `json:"providers_processed,omitempty"`
	ProvidersRemoved   []string          `json:"providers_removed,omitempty"`
	Error            string              `json:"error,omitempty"`
}

// processStatuses is an in-memory store for process statuses.
var processStatuses sync.Map // Use sync.Map for concurrent access

// isProcessRunning tracks if a provider processing is currently running.
var isProcessRunning bool
var processRunningMutex sync.Mutex

type ProviderProcessInput struct {
	ProvidersToProcess []string `json:"providers_to_process"`
	ProvidersToRemove  []string `json:"providers_to_remove"`
}

type ProviderHandler struct{}

func NewProviderHandler() *ProviderHandler {
	return &ProviderHandler{}
}

func (h *ProviderHandler) ProcessProviders(c echo.Context) error {
	req := &ProviderProcessInput{}
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body", "details": err.Error()})
	}

	processRunningMutex.Lock()
	if isProcessRunning {
		processRunningMutex.Unlock()
		return c.JSON(http.StatusConflict, map[string]interface{}{
			"error":   "Process Conflict",
			"message": "Another process is already running. Please wait for it to complete.",
		})
	}
	isProcessRunning = true
	processRunningMutex.Unlock()

	processID := xid.New().String()
	status := &ProcessStatus{
		ID:               processID,
		Status:           "running",
		StartTime:        time.Now(),
		ProvidersProcessed: req.ProvidersToProcess,
		ProvidersRemoved:   req.ProvidersToRemove,
	}
	processStatuses.Store(processID, status) // Store status in memory

	go func() { // Run the process in a goroutine to avoid blocking the handler
		err := provider_processor.Process(req.ProvidersToProcess, req.ProvidersToRemove)
		if err != nil {
			status.Status = "failed"
			status.EndTime = time.Now()
			status.Error = err.Error()
		} else {
			status.Status = "completed"
			status.EndTime = time.Now()
		}
		processStatuses.Store(processID, status) // Update status after completion

		processRunningMutex.Lock()
		isProcessRunning = false // Reset the running flag when process finishes
		processRunningMutex.Unlock()
	}()

	return c.JSON(http.StatusAccepted, map[string]interface{}{
		"process_id": processID,
		"message":    "Provider processing started. Use process_id to check status.",
	})
}

func (h *ProviderHandler) GetProcessStatus(c echo.Context) error {
	processID := c.Param("processID")
	if processID == "" {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"error": "processID is required"})
	}

	statusInterface, ok := processStatuses.Load(processID)
	if !ok {
		return c.JSON(http.StatusNotFound, map[string]interface{}{"error": "Process not found", "process_id": processID})
	}
	status, ok := statusInterface.(*ProcessStatus)
	if !ok {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "Internal server error - status data type issue"})
	}

	return c.JSON(http.StatusOK, status)
}

func (h *ProviderHandler) ListProcesses(c echo.Context) error {
	var statuses []*ProcessStatus
	processStatuses.Range(func(key, value interface{}) bool {
		if status, ok := value.(*ProcessStatus); ok {
			statuses = append(statuses, status)
		}
		return true // continue iterating
	})
	return c.JSON(http.StatusOK, statuses)
}
