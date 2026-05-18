package models

import (
	"database/sql"
	"time"
)

// Entry represents a blacklisted URL record.
// This is the new schema replacement for the legacy entries table.
type Entry struct {
	ID         string         `json:"id" db:"id"`
	SourceID   string         `json:"source_id" db:"source_id"`
	Domain     sql.NullString `json:"domain" db:"domain"`
	Host       sql.NullString `json:"host" db:"host"`
	Path       sql.NullString `json:"path" db:"path"`
	File       sql.NullString `json:"file" db:"file"`
	Query      sql.NullString `json:"query" db:"query"`
	Login      sql.NullString `json:"login" db:"login"`
	IP         sql.NullString `json:"ip" db:"ip"`
	FullURL    sql.NullString `json:"full_url" db:"full_url"`
	Scheme     sql.NullString `json:"scheme" db:"scheme"`
	Confidence sql.NullFloat64 `json:"confidence" db:"confidence"`
	Category   sql.NullString `json:"category" db:"category"`
	CreatedAt  time.Time      `json:"created_at" db:"created_at"`
}

// TableName returns the table name for Entry.
func (Entry) TableName() string {
	return "entries"
}
