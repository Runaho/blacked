package providers

import "time"

// ProcessStatus holds the status of a provider processing task.
type ProcessStatus struct {
	ID                 string    `json:"id"`
	Status             string    `json:"status"` // "running", "completed", "failed"
	StartTime          time.Time `json:"start_time"`
	EndTime            time.Time `json:"end_time"`
	ProvidersProcessed []string  `json:"providers_processed,omitempty"`
	ProvidersRemoved   []string  `json:"providers_removed,omitempty"`
	Error              string    `json:"error,omitempty"`
}
