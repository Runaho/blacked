package repository

import (
	"blacked/features/entries"
	"context"
)

// BlacklistRepository interface defines the data access methods for blacklist entries.
type BlacklistRepository interface {
	GetAllEntries(ctx context.Context) ([]entries.Entry, error)
	GetEntryByID(ctx context.Context, id string) (*entries.Entry, error)
	GetEntriesBySource(ctx context.Context, source string) ([]entries.Entry, error)
	GetEntriesByCategory(ctx context.Context, category string) ([]entries.Entry, error)
	SaveEntry(ctx context.Context, entry entries.Entry) error
	BatchSaveEntries(ctx context.Context, entries []entries.Entry) error // Batched UPSERT
	ClearAllEntries(ctx context.Context) error                           // Soft Delete All
	CheckIfHostExists(ctx context.Context, host string) (bool, error)    // Check for active host
	SoftDeleteEntryByID(ctx context.Context, id string) error
}
