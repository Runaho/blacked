package providers

import (
	"context"
	"errors"
	"math/rand"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
)

// Process manager errors
var (
	ErrProcessAlreadyRunning = errors.New("a provider process is already running")
	ErrProcessNotFound       = errors.New("process not found")
)

// Retry config
const (
	retryAttempts   = 3
	retryBaseDelay  = 100 * time.Millisecond
	retryMaxDelay   = 500 * time.Millisecond
)

// ProcessPersistence is the interface for persisting process status to DB.
// Defined here to avoid import cycle (providers → repository → providers).
type ProcessPersistence interface {
	InsertProcess(ctx context.Context, status *ProcessStatus) error
	UpdateProcessStatus(ctx context.Context, status *ProcessStatus) error
}

// ProcessManager handles centralized process state management.
// It ensures only one provider processing job can run at a time,
// whether triggered by startup, cron scheduler, or API endpoint.
// When a persistence backend is set, every state change is persisted.
type ProcessManager struct {
	mu             sync.RWMutex
	currentProcess *ProcessStatus
	isRunning      atomic.Bool
	history        []*ProcessStatus
	maxHistory     int
	persistence    ProcessPersistence // optional DB persistence
}

var (
	globalProcessManager *ProcessManager
	processManagerOnce   sync.Once
)

// GetProcessManager returns the singleton process manager instance
func GetProcessManager() *ProcessManager {
	processManagerOnce.Do(func() {
		globalProcessManager = &ProcessManager{
			maxHistory: 100, // Keep last 100 process records in memory
			history:    make([]*ProcessStatus, 0, 100),
		}
	})
	return globalProcessManager
}

// SetPersistence attaches a persistence backend for process records.
// This should be called once at application startup after DB init.
func (pm *ProcessManager) SetPersistence(persistence ProcessPersistence) {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	pm.persistence = persistence
	log.Info().Msg("Provider process manager DB persistence enabled")
}

// TryStartProcess attempts to start a new process.
// Returns the process ID if successful, or an error if a process is already running.
// Persists to DB if persistence is configured.
// Uses exponential backoff with jitter for retry attempts.
func (pm *ProcessManager) TryStartProcess(ctx context.Context, source string, providersToProcess, providersToRemove []string) (string, error) {
	var lastErr error

	for attempt := 0; attempt < retryAttempts; attempt++ {
		// Fast path: check if already running without lock
		if !pm.isRunning.Load() {
			pm.mu.Lock()

			// Double-check under lock
			if !pm.isRunning.Load() {
				processID := uuid.New().String()

				// Initialize per-provider status tracking
				providerStatuses := make([]*ProviderStatus, 0, len(providersToProcess))
				now := time.Now()
				for _, name := range providersToProcess {
					providerStatuses = append(providerStatuses, &ProviderStatus{
						Name:          name,
						Status:        "pending",
						CurrentAction: "pending",
						StartedAt:     &now,
					})
				}

				pm.currentProcess = &ProcessStatus{
					ID:                 processID,
					Status:             "running",
					StartTime:          now,
					Providers:          providerStatuses,
					ProvidersProcessed: providersToProcess,
					ProvidersRemoved:   providersToRemove,
				}
				pm.isRunning.Store(true)

				// Persist to DB if configured
				if pm.persistence != nil {
					if err := pm.persistence.InsertProcess(ctx, pm.currentProcess); err != nil {
						log.Warn().Err(err).Str("process_id", processID).Msg("Failed to persist process start to DB")
					}
				}

				log.Info().
					Str("process_id", processID).
					Str("source", source).
					Strs("providers", providersToProcess).
					Msg("Process started")

				pm.mu.Unlock()
				return processID, nil
			}

			pm.mu.Unlock()
		}

		lastErr = ErrProcessAlreadyRunning

		// Don't wait after last attempt
		if attempt < retryAttempts-1 {
			// Exponential backoff with jitter
			delay := retryBaseDelay * time.Duration(1<<attempt)
			if delay > retryMaxDelay {
				delay = retryMaxDelay
			}
			// Add jitter (±25%)
			jitter := time.Duration(rand.Int63n(int64(delay / 4)))
			delay = delay - jitter/2 + jitter

			// Respect context cancellation
			select {
			case <-ctx.Done():
				return "", ctx.Err()
			case <-time.After(delay):
			}
		}
	}

	log.Warn().
		Int("attempts", retryAttempts).
		Msg("Failed to start process after all retry attempts")
	return "", lastErr
}

// FinishProcess marks the current process as completed or failed.
// Persists to DB if persistence is configured.
func (pm *ProcessManager) FinishProcess(processID string, err error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.currentProcess == nil || pm.currentProcess.ID != processID {
		log.Warn().
			Str("process_id", processID).
			Msg("Attempted to finish unknown process")
		return
	}

	pm.currentProcess.EndTime = time.Now()
	if err != nil {
		pm.currentProcess.Status = "failed"
		pm.currentProcess.Error = err.Error()
	} else {
		pm.currentProcess.Status = "completed"
	}

	// Persist to DB if configured
	if pm.persistence != nil {
		ctx := context.Background()
		if err := pm.persistence.UpdateProcessStatus(ctx, pm.currentProcess); err != nil {
			log.Warn().Err(err).Str("process_id", processID).Msg("Failed to persist process status to DB")
		}
	}

	// Add to history
	pm.history = append(pm.history, pm.currentProcess)

	// Trim history if needed
	if len(pm.history) > pm.maxHistory {
		pm.history = pm.history[len(pm.history)-pm.maxHistory:]
	}

	duration := pm.currentProcess.EndTime.Sub(pm.currentProcess.StartTime)

	log.Info().
		Str("process_id", processID).
		Str("status", pm.currentProcess.Status).
		Dur("duration", duration).
		Msg("Process finished")

	pm.currentProcess = nil
	pm.isRunning.Store(false)
}

// IsRunning returns true if a process is currently running
func (pm *ProcessManager) IsRunning() bool {
	return pm.isRunning.Load()
}

// GetCurrentProcess returns the current running process status, or nil if none
func (pm *ProcessManager) GetCurrentProcess() *ProcessStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.currentProcess == nil {
		return nil
	}

	// Return a copy to avoid race conditions
	copy := *pm.currentProcess
	return &copy
}

// GetProcessByID returns a process by ID (current or from history)
func (pm *ProcessManager) GetProcessByID(processID string) (*ProcessStatus, error) {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	// Check current process first
	if pm.currentProcess != nil && pm.currentProcess.ID == processID {
		copy := *pm.currentProcess
		return &copy, nil
	}

	// Check history
	for _, p := range pm.history {
		if p.ID == processID {
			copy := *p
			return &copy, nil
		}
	}

	return nil, ErrProcessNotFound
}

// GetRecentProcesses returns recent process history
func (pm *ProcessManager) GetRecentProcesses(limit int) []*ProcessStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	var result []*ProcessStatus

	// Include current process if running
	if pm.currentProcess != nil {
		current := *pm.currentProcess
		result = append(result, &current)
	}

	// Add history items (most recent first)
	historyLen := len(pm.history)
	for i := historyLen - 1; i >= 0 && len(result) < limit; i-- {
		copy := *pm.history[i]
		result = append(result, &copy)
	}

	return result
}

// GetProviderStatus returns the status of a specific provider within the current process
func (pm *ProcessManager) GetProviderStatus(providerName string) *ProviderStatus {
	pm.mu.RLock()
	defer pm.mu.RUnlock()

	if pm.currentProcess == nil {
		return nil
	}

	for _, ps := range pm.currentProcess.Providers {
		if ps.Name == providerName {
			return ps
		}
	}
	return nil
}

// UpdateProviderStatus updates the status of a specific provider within the current process
func (pm *ProcessManager) UpdateProviderStatus(providerName string, status string, currentAction string, pageCurrent int, pageTotal int, bytesTransferred int64, retryCount int, lastError *string, entriesProcessed int) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	if pm.currentProcess == nil {
		return
	}

	now := time.Now()
	for _, ps := range pm.currentProcess.Providers {
		if ps.Name == providerName {
			ps.Status = status
			ps.CurrentAction = currentAction
			ps.PageCurrent = pageCurrent
			ps.PageTotal = pageTotal
			ps.BytesTransferred = bytesTransferred
			ps.RetryCount = retryCount
			ps.LastError = lastError
			ps.EntriesProcessed = entriesProcessed
			if status == "running" && ps.StartedAt == nil {
				ps.StartedAt = &now
			}
			if status == "done" || status == "error" || status == "skipped" {
				ps.CompletedAt = &now
			}
			return
		}
	}
}

// ResetForTesting resets the process manager state (for tests only)
func (pm *ProcessManager) ResetForTesting() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.currentProcess = nil
	pm.isRunning.Store(false)
	pm.history = make([]*ProcessStatus, 0, pm.maxHistory)
}
