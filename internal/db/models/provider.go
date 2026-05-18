package models

import (
	"time"
)

// Provider represents a blacklist data provider.
type Provider struct {
	ID          string    `json:"id" db:"id"`
	Name        string    `json:"name" db:"name"`
	Description string    `json:"description" db:"description"`
	TrustScore  float64   `json:"trust_score" db:"trust_score"`
	CreatedAt   time.Time `json:"created_at" db:"created_at"`
	UpdatedAt   time.Time `json:"updated_at" db:"updated_at"`
}

// TableName returns the table name for Provider.
func (Provider) TableName() string {
	return "providers"
}

// ProviderSeed holds static seed data for built-in providers.
var ProviderSeed = []Provider{
	{ID: "spamhaus", Name: "Spamhaus", Description: "Spamhaus blocklists", TrustScore: 0.95},
	{ID: "abuse-ch", Name: "abuse.ch", Description: "URLHaus and MalwareBazaar", TrustScore: 0.90},
	{ID: "openphish", Name: "OpenPhish", Description: "OpenPhish phishing feed", TrustScore: 0.75},
	{ID: "alienvault", Name: "AlienVault OTX", Description: "Open Threat Exchange", TrustScore: 0.80},
	{ID: "phishtank", Name: "PhishTank", Description: "Community phishing verification", TrustScore: 0.70},
	{ID: "oisd", Name: "OISD", Description: "One Unified Hosts Blocklist", TrustScore: 0.65},
}
