package providers

import (
	"context"
	"errors"
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

// ProcessManager handles centralized process state management.
// It ensures only one provider processing job can run at a time,
// whether triggered by startup, cron scheduler, or API endpoint.
type ProcessManager struct {
	mu             sync.RWMutex
	currentProcess *ProcessStatus
	isRunning      atomic.Bool
	history        []*ProcessStatus
	maxHistory     int
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

// TryStartProcess attempts to start a new process.
// Returns the process ID if successful, or an error if a process is already running.
func (pm *ProcessManager) TryStartProcess(ctx context.Context, source string, providersToProcess, providersToRemove []string) (string, error) {
	// Fast path: check if already running without lock
	if pm.isRunning.Load() {
		return "", ErrProcessAlreadyRunning
	}

	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Double-check under lock
	if pm.isRunning.Load() {
		return "", ErrProcessAlreadyRunning
	}

	processID := uuid.New().String()

	pm.currentProcess = &ProcessStatus{
		ID:                 processID,
		Status:             "running",
		StartTime:          time.Now(),
		ProvidersProcessed: providersToProcess,
		ProvidersRemoved:   providersToRemove,
	}
	pm.isRunning.Store(true)

	log.Info().
		Str("process_id", processID).
		Str("source", source).
		Strs("providers", providersToProcess).
		Msg("Process started")

	return processID, nil
}

// FinishProcess marks the current process as completed or failed
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

// ResetForTesting resets the process manager state (for tests only)
func (pm *ProcessManager) ResetForTesting() {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	pm.currentProcess = nil
	pm.isRunning.Store(false)
	pm.history = make([]*ProcessStatus, 0, pm.maxHistory)
}
