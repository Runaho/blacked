package models

import "database/sql"

// SourceFetchLog tracks every fetch attempt for a Source.
type SourceFetchLog struct {
	ID          int64          `json:"id" db:"id"`
	SourceID    string         `json:"source_id" db:"source_id"`
	Status      string         `json:"status" db:"status"` // pending, running, success, failed
	EntryCount  sql.NullInt64  `json:"entry_count" db:"entry_count"`
	Error       sql.NullString `json:"error" db:"error"`
	DurationMS  sql.NullInt64  `json:"duration_ms" db:"duration_ms"`
	StartedAt   sql.NullTime   `json:"started_at" db:"started_at"`
	FinishedAt  sql.NullTime   `json:"finished_at" db:"finished_at"`
}

// TableName returns the table name for SourceFetchLog.
func (SourceFetchLog) TableName() string {
	return "source_fetch_log"
}
