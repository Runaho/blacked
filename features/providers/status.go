package providers

import "time"

// ProviderStatus holds the detailed status of a single provider within a process.
type ProviderStatus struct {
	Name            string     `json:"name"`
	Status          string     `json:"status"` // "running", "done", "error", "pending", "skipped"
	CurrentAction   string     `json:"current_action"` // "fetching_page", "parsing", "writing_db", "completed", "failed"
	PageCurrent     int        `json:"page_current,omitempty"`
	PageTotal       int        `json:"page_total,omitempty"`
	BytesTransferred int64     `json:"bytes_transferred"`
	RetryCount      int        `json:"retry_count"`
	LastError       *string    `json:"last_error,omitempty"`
	EntriesProcessed int       `json:"entries_processed"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
}

// ProcessStatus holds the status of a provider processing task.
type ProcessStatus struct {
	ID                 string           `json:"id"`
	Status             string           `json:"status"` // "running", "completed", "failed"
	StartTime          time.Time        `json:"start_time"`
	EndTime            time.Time        `json:"end_time"`
	Providers          []*ProviderStatus `json:"providers,omitempty"`
	ProvidersProcessed []string         `json:"providers_processed,omitempty"`
	ProvidersRemoved   []string         `json:"providers_removed,omitempty"`
	Error              string           `json:"error,omitempty"`
}
