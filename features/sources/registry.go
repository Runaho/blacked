package sources

import (
	"sync"

	"github.com/rs/zerolog/log"
)

// DefaultRegistry is the global singleton source registry.
var DefaultRegistry = NewRegistry()

// Registry manages all known sources.
type Registry struct {
	mu      sync.RWMutex
	byID    map[string]*Source
	byProvider map[string][]*Source
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{
		byID:       make(map[string]*Source),
		byProvider: make(map[string][]*Source),
	}
}

// Register adds a source to the registry.
// Panics if a source with the same ID already exists.
func (r *Registry) Register(s *Source) {
	if s == nil {
		panic("cannot register nil source")
	}
	if s.ID == "" {
		panic("source ID is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	if _, exists := r.byID[s.ID]; exists {
		log.Warn().Str("source_id", s.ID).Msg("Source already registered")
		return
	}

	r.byID[s.ID] = s
	r.byProvider[s.ProviderID] = append(r.byProvider[s.ProviderID], s)

	log.Info().
		Str("source_id", s.ID).
		Str("provider_id", s.ProviderID).
		Str("name", s.Name).
		Msg("Source registered")
}

// Get returns a source by ID, or nil if not found.
func (r *Registry) Get(id string) *Source {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.byID[id]
}

// GetByProvider returns all sources for a given provider ID.
func (r *Registry) GetByProvider(providerID string) []*Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	srcs := r.byProvider[providerID]
	if len(srcs) == 0 {
		return nil
	}
	out := make([]*Source, len(srcs))
	copy(out, srcs)
	return out
}

// All returns a snapshot of all registered sources.
func (r *Registry) All() []*Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*Source, 0, len(r.byID))
	for _, s := range r.byID {
		out = append(out, s)
	}
	return out
}

// FilterEnabled returns only enabled sources.
func (r *Registry) FilterEnabled() []*Source {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]*Source, 0, len(r.byID))
	for _, s := range r.byID {
		if s.Enabled {
			out = append(out, s)
		}
	}
	return out
}

// Count returns the total number of registered sources.
func (r *Registry) Count() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byID)
}

// CountByProvider returns the number of sources for a provider.
func (r *Registry) CountByProvider(providerID string) int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.byProvider[providerID])
}

// Deregister removes a source from the registry.
func (r *Registry) Deregister(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	s, ok := r.byID[id]
	if !ok {
		return
	}

	delete(r.byID, id)

	// Rebuild provider slice
	srcs := r.byProvider[s.ProviderID]
	filtered := make([]*Source, 0, len(srcs)-1)
	for _, src := range srcs {
		if src.ID != id {
			filtered = append(filtered, src)
		}
	}
	if len(filtered) == 0 {
		delete(r.byProvider, s.ProviderID)
	} else {
		r.byProvider[s.ProviderID] = filtered
	}

	log.Info().Str("source_id", id).Msg("Source deregistered")
}

// --- Package-level helpers that delegate to DefaultRegistry ---

// Register adds a source to the default registry.
func Register(s *Source) {
	DefaultRegistry.Register(s)
}

// GetSource returns a source by ID from the default registry.
func GetSource(id string) *Source {
	return DefaultRegistry.Get(id)
}

// GetSourcesByProvider returns sources for a provider from the default registry.
func GetSourcesByProvider(providerID string) []*Source {
	return DefaultRegistry.GetByProvider(providerID)
}

// AllSources returns all sources from the default registry.
func AllSources() []*Source {
	return DefaultRegistry.All()
}

// EnabledSources returns only enabled sources.
func EnabledSources() []*Source {
	return DefaultRegistry.FilterEnabled()
}
