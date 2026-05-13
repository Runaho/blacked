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
	SourceTypeJSON    SourceType = "json"
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
	UpdateInterval  sql.NullInt64   `json:"update_interval" db:"update_interval"`
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
var SourceSeed = []Source{
	{ID: "oisd-big", ProviderID: "oisd", Name: "OISD Big", SourceURL: "https://big.oisd.nl", Type: SourceTypeFlat, Enabled: true},
	{ID: "oisd-nsfw", ProviderID: "oisd", Name: "OISD NSFW", SourceURL: "https://nsfw.oisd.nl", Type: SourceTypeFlat, Enabled: true},
	{ID: "urlhaus-urls", ProviderID: "abuse-ch", Name: "URLhaus URLs", SourceURL: "https://urlhaus.abuse.ch/downloads/csv/", Type: SourceTypeCSV, Enabled: true},
	{ID: "malwarebazaar", ProviderID: "abuse-ch", Name: "MalwareBazaar", SourceURL: "https://bazaar.abuse.ch/export/csv/recent/", Type: SourceTypeCSV, Enabled: true},
	{ID: "alienvault-pulses", ProviderID: "alienvault", Name: "AlienVault Pulses", SourceURL: "https://otx.alienvault.com/api/v1/pulses/subscribed", Type: SourceTypeAPI, Enabled: true},
	{ID: "phishtank-online", ProviderID: "phishtank", Name: "PhishTank Online Valid", SourceURL: "http://data.phishtank.com/data/online-valid.csv", Type: SourceTypeCSV, Enabled: true},
	{ID: "spamhaus-drop", ProviderID: "spamhaus", Name: "Spamhaus DROP", SourceURL: "https://www.spamhaus.org/drop/drop.txt", Type: SourceTypeFlat, Enabled: true},
	{ID: "openphish-feed", ProviderID: "oisd", Name: "OpenPhish Feed", SourceURL: "https://openphish.com/feed.txt", Type: SourceTypeFlat, Enabled: true},
}
