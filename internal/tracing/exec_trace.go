package tracing

import (
	"os"
	"path/filepath"
	"runtime/trace"
	"sync"
	"time"

	"github.com/rs/zerolog/log"
)

var (
	execTraceMu     sync.Mutex
	execTraceActive bool
	traceFile       *os.File
	traceStartedAt  time.Time
	traceFileName   string
)

// ShouldStartExecTrace checks if execution tracing is enabled via environment variables.
// Set BLACKEDEXECTRACE=1 to enable tracing.
// Optionally set BLACKEDEXECTRACE_SCOPE to filter (e.g., "providers", "URLHAUS").
func ShouldStartExecTrace(scope string) bool {
	if os.Getenv("BLACKEDEXECTRACE") != "1" {
		return false
	}
	if wantedScope := os.Getenv("BLACKEDEXECTRACE_SCOPE"); wantedScope != "" && wantedScope != scope {
		return false
	}
	return true
}

// StartExecTrace begins a Go runtime execution trace.
// Returns a stop function that must be called to finalize the trace.
// If tracing is already active or not enabled, returns a no-op function.
func StartExecTrace(name, processID string) (stop func()) {
	if !ShouldStartExecTrace(name) {
		return func() {}
	}

	execTraceMu.Lock()
	if execTraceActive {
		execTraceMu.Unlock()
		log.Debug().Msg("Exec trace already active, skipping")
		return func() {}
	}
	execTraceActive = true
	execTraceMu.Unlock()

	traceStartedAt = time.Now()
	traceDir := filepath.Join(".", "traces")
	if err := os.MkdirAll(traceDir, 0o755); err != nil {
		log.Warn().Err(err).Msg("Failed to create traces directory; skipping exec trace")
		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()
		return func() {}
	}

	traceFileName = filepath.Join(traceDir, name+"-"+processID+"-"+traceStartedAt.UTC().Format("20060102T150405Z")+".out")
	var err error
	traceFile, err = os.Create(traceFileName)
	if err != nil {
		log.Warn().Err(err).Str("file", traceFileName).Msg("Failed to create exec trace file; skipping exec trace")
		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()
		return func() {}
	}

	if err := trace.Start(traceFile); err != nil {
		_ = traceFile.Close()
		log.Warn().Err(err).Str("file", traceFileName).Msg("Failed to start exec trace")
		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()
		return func() {}
	}

	log.Info().
		Str("name", name).
		Str("process_id", processID).
		Str("file", traceFileName).
		Msg("Go exec trace started")

	return func() {
		trace.Stop()
		_ = traceFile.Close()

		execTraceMu.Lock()
		execTraceActive = false
		execTraceMu.Unlock()

		log.Info().
			Str("name", name).
			Str("process_id", processID).
			Dur("duration", time.Since(traceStartedAt)).
			Str("file", traceFileName).
			Msg("Go exec trace stopped")
	}
}

// IsExecTraceActive returns true if a trace is currently being recorded.
func IsExecTraceActive() bool {
	execTraceMu.Lock()
	defer execTraceMu.Unlock()
	return execTraceActive
}
