package models

import (
	"database/sql"
	"time"
)

// SourceType defines how a source provides its data.
type SourceType string

const (
	SourceTypeFlat    SourceType = "flat"
	SourceTypeAPI     SourceType = "api"
	SourceTypeCSV     SourceType = "csv"
	SourceTypeRSS     SourceType = "rss"
)

// Source represents a unique feed/list under a Provider.
type Source struct {
	ID              string         `json:"id" db:"id"`
	ProviderID      string         `json:"provider_id" db:"provider_id"`
	Name            string         `json:"name" db:"name"`
	SourceURL       string         `json:"source_url" db:"source_url"`
	Type            SourceType     `json:"type" db:"type"`
	TrustScore      sql.NullFloat64 `json:"trust_score" db:"trust_score"`
	UpdateInterval  sql.NullInt64   `json:"update_interval" db:"update_interval"` // seconds
	Enabled         bool           `json:"enabled" db:"enabled"`
	LastFetchAt     sql.NullTime   `json:"last_fetch_at" db:"last_fetch_at"`
	LastFetchStatus sql.NullString `json:"last_fetch_status" db:"last_fetch_status"`
	CreatedAt       time.Time      `json:"created_at" db:"created_at"`
	UpdatedAt       time.Time      `json:"updated_at" db:"updated_at"`
}

// TableName returns the table name for Source.
func (Source) TableName() string {
	return "sources"
}

// SourceSeed holds static seed data for built-in sources.
// update_interval is in seconds (nil = use provider default / system fallback).
var SourceSeed = []Source{
	{ID: "oisd-big", ProviderID: "oisd", Name: "OISD Big", SourceURL: "https://big.oisd.nl/domainswild2", Type: SourceTypeFlat, Enabled: true, UpdateInterval: sql.NullInt64{Int64: 86400, Valid: true}},       // daily
	{ID: "oisd-nsfw", ProviderID: "oisd", Name: "OISD NSFW", SourceURL: "https://nsfw.oisd.nl/domainswild2", Type: SourceTypeFlat, Enabled: true, UpdateInterval: sql.NullInt64{Int64: 86400, Valid: true}},   // daily
	{ID: "urlhaus-urls", ProviderID: "abuse-ch", Name: "URLhaus URLs", SourceURL: "https://urlhaus.abuse.ch/downloads/text/", Type: SourceTypeFlat, Enabled: true, UpdateInterval: sql.NullInt64{Int64: 7200, Valid: true}},    // 2 hours
	{ID: "openphish-feed", ProviderID: "oisd", Name: "OpenPhish Feed", SourceURL: "https://openphish.com/feed.txt", Type: SourceTypeFlat, Enabled: true, UpdateInterval: sql.NullInt64{Int64: 14400, Valid: true}},     // 4 hours
	{ID: "phishtank-online", ProviderID: "phishtank", Name: "PhishTank Online Valid", SourceURL: "http://data.phishtank.com/data/online-valid.csv", Type: SourceTypeCSV, Enabled: true, UpdateInterval: sql.NullInt64{Int64: 14400, Valid: true}}, // 4 hours
	{ID: "spamhaus-drop", ProviderID: "spamhaus", Name: "Spamhaus DROP", SourceURL: "https://www.spamhaus.org/drop/drop.txt", Type: SourceTypeFlat, Enabled: true, UpdateInterval: sql.NullInt64{Int64: 86400, Valid: true}}, // daily
}
