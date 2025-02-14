package provider

import (
	"blacked/features/providers/services"
	"fmt"
	"net/http"
	"sync"

	"github.com/labstack/echo/v4"
)

// processStatuses is an in-memory store for process statuses.
var processStatuses sync.Map // Use sync.Map for concurrent access

// isProcessRunning tracks if a provider processing is currently running.
var isProcessRunning bool
var processRunningMutex sync.Mutex

type ProviderProcessInput struct {
	ProvidersToProcess []string `json:"providers_to_process"`
	ProvidersToRemove  []string `json:"providers_to_remove"`
}

type ProviderHandler struct {
	providerProcessService *services.ProviderProcessService
}

func NewProviderHandler(svc *services.ProviderProcessService) *ProviderHandler {
	return &ProviderHandler{
		providerProcessService: svc,
	}
}

func (h *ProviderHandler) ProcessProviders(c echo.Context) error {
	req := &ProviderProcessInput{}
	if err := c.Bind(req); err != nil {
		return c.JSON(http.StatusBadRequest, map[string]interface{}{"error": "Invalid request body", "details": err.Error()})
	}

	ctx := c.Request().Context()
	isRunning, err := h.providerProcessService.IsProcessRunning(ctx)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "Failed to check process status", "details": err.Error()})
	}
	if isRunning {
		return c.JSON(http.StatusConflict, map[string]interface{}{
			"error":   "Process Conflict",
			"message": "Another process is already running. Please wait for it to complete.",
		})
	}

	processID, err := h.providerProcessService.StartProcess(ctx, req.ProvidersToProcess, req.ProvidersToRemove)
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "Failed to start provider process", "details": err.Error()})
	}

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

	status, err := h.providerProcessService.GetProcessStatus(c.Request().Context(), processID)
	if err != nil {
		if err.Error() == fmt.Sprintf("process not found: %s", processID) { // check error message, not ideal, better to have specific error type
			return c.JSON(http.StatusNotFound, map[string]interface{}{"error": "Process not found", "process_id": processID})
		}
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "Failed to get process status", "details": err.Error()})
	}

	return c.JSON(http.StatusOK, status)
}

func (h *ProviderHandler) ListProcesses(c echo.Context) error {
	statuses, err := h.providerProcessService.ListProcesses(c.Request().Context())
	if err != nil {
		return c.JSON(http.StatusInternalServerError, map[string]interface{}{"error": "Failed to list processes", "details": err.Error()})
	}
	return c.JSON(http.StatusOK, statuses)
}
