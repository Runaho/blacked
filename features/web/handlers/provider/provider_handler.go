package provider

import (
	"blacked/features/providers/services"
	"blacked/features/web/handlers/response"
	"net/http"
	"sync"

	"github.com/labstack/echo/v4"
	"github.com/rs/zerolog/log"
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
		return response.BadRequest(c, "Invalid request body: "+err.Error())
	}

	ctx := c.Request().Context()
	isRunning, err := h.providerProcessService.IsProcessRunning(ctx)
	if err != nil {
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Failed to check process status", err.Error())
	}
	if isRunning {
		return response.Error(c, http.StatusConflict,
			"Another process is already running. Please wait for it to complete.")
	}

	processID, err := h.providerProcessService.StartProcess(ctx, req.ProvidersToProcess, req.ProvidersToRemove)
	if err != nil {
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Failed to start provider process", err.Error())
	}

	return response.Success(c, map[string]any{
		"process_id": processID,
		"message":    "Provider processing started. Use process_id to check status.",
	})
}

func (h *ProviderHandler) ListProcesses(c echo.Context) error {
	statuses, err := h.providerProcessService.ListProcesses(c.Request().Context())
	if err != nil {
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Failed to list processes", err.Error())
	}
	return response.Success(c, statuses)
}

func (h *ProviderHandler) GetProcessStatus(c echo.Context) error {
	processID := c.Param("processID")
	if processID == "" {
		return response.BadRequest(c, "processID is required")
	}

	status, err := h.providerProcessService.GetProcessStatus(c.Request().Context(), processID)
	if err != nil {
		log.Err(err).Str("process_id", processID).Msg("Failed to get process status")
		if err == services.ErrProcessNotFound {
			return response.NotFound(c, "Process not found", processID)
		}
		return response.ErrorWithDetails(c, http.StatusInternalServerError,
			"Failed to get process status", err.Error())
	}

	return response.Success(c, status)
}
