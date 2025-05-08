package repository

import (
	"blacked/features/entries"
	"blacked/features/entries/enums"
	"context"
)

// BlacklistRepository interface defines the data access methods for blacklist entries.
type BlacklistRepository interface {
	StreamEntries(ctx context.Context, out chan<- entries.EntryStream) error
	StreamEntriesCount(ctx context.Context) (int, error)
	GetAllEntries(ctx context.Context) ([]entries.Entry, error)
	GetEntryByID(ctx context.Context, id string) (*entries.Entry, error)
	GetEntriesBySource(ctx context.Context, source string) ([]entries.Entry, error)
	GetEntriesByCategory(ctx context.Context, category string) ([]entries.Entry, error)
	GetEntriesByIDs(ctx context.Context, ids []string) ([]*entries.Entry, error)
	SaveEntry(ctx context.Context, entry entries.Entry) error
	BatchSaveEntries(ctx context.Context, entries []*entries.Entry) error // Batched UPSERT
	ClearAllEntries(ctx context.Context) error                            // Soft Delete All
	SoftDeleteEntryByID(ctx context.Context, id string) error
	QueryLink(ctx context.Context, link string) ([]entries.Hit, error)
	QueryLinkByType(ctx context.Context, link string, queryType *enums.QueryType) ([]entries.Hit, error)
	QueryExactURLMatch(ctx context.Context, normalizedLink string) []entries.Hit
}
