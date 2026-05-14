package sources

import (
	"blacked/internal/config"
	"testing"

	"github.com/gocolly/colly/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRegistry_RegisterAndGet(t *testing.T) {
	r := NewRegistry()

	src := &Source{
		ID:         "test-src",
		ProviderID: "test-provider",
		Name:       "Test Source",
		SourceURL:  "https://example.com/list.txt",
		Enabled:    true,
	}

	r.Register(src)

	got := r.Get("test-src")
	require.NotNil(t, got)
	assert.Equal(t, "test-src", got.ID)
	assert.Equal(t, "test-provider", got.ProviderID)

	// Duplicate registration should be a no-op (warn logged)
	r.Register(src)
	assert.Equal(t, 1, r.Count())
}

func TestRegistry_Deregister(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "a", ProviderID: "p1", Enabled: true})
	r.Register(&Source{ID: "b", ProviderID: "p1", Enabled: true})

	assert.Equal(t, 2, r.Count())
	r.Deregister("a")
	assert.Equal(t, 1, r.Count())
	assert.Nil(t, r.Get("a"))

	// Deregister non-existent is safe
	r.Deregister("nonexistent")
	assert.Equal(t, 1, r.Count())
}

func TestRegistry_GetByProvider(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "a", ProviderID: "p1", Enabled: true})
	r.Register(&Source{ID: "b", ProviderID: "p1", Enabled: true})
	r.Register(&Source{ID: "c", ProviderID: "p2", Enabled: true})

	p1Sources := r.GetByProvider("p1")
	assert.Len(t, p1Sources, 2)

	p2Sources := r.GetByProvider("p2")
	assert.Len(t, p2Sources, 1)

	assert.Equal(t, 0, r.CountByProvider("nonexistent"))
}

func TestRegistry_FilterEnabled(t *testing.T) {
	r := NewRegistry()
	r.Register(&Source{ID: "enabled", ProviderID: "p", Enabled: true})
	r.Register(&Source{ID: "disabled", ProviderID: "p", Enabled: false})

	enabled := r.FilterEnabled()
	assert.Len(t, enabled, 1)
	assert.Equal(t, "enabled", enabled[0].ID)
}

func TestRegistry_NilSourcePanics(t *testing.T) {
	assert.Panics(t, func() { NewRegistry().Register(nil) })
	assert.Panics(t, func() { NewRegistry().Register(&Source{}) })
}

func TestDefaultRegistry(t *testing.T) {
	// Reset for clean test state
	DefaultRegistry = NewRegistry()

	src := &Source{ID: "global", ProviderID: "p", Enabled: true}
	Register(src)

	assert.Equal(t, src, GetSource("global"))
	assert.Len(t, AllSources(), 1)
	assert.Len(t, EnabledSources(), 1)
}

func TestBloomType_DepthWeight(t *testing.T) {
	assert.Equal(t, 0.3, DepthWeight[BloomDomain])
	assert.Equal(t, 1.0, DepthWeight[BloomHostPath])
	assert.Equal(t, 0.8, DepthWeight[BloomIP])
}

func TestConfidenceLevel(t *testing.T) {
	assert.Equal(t, "critical", ConfidenceLevel(0.95))
	assert.Equal(t, "high", ConfidenceLevel(0.70))
	assert.Equal(t, "medium", ConfidenceLevel(0.55))
	assert.Equal(t, "low", ConfidenceLevel(0.25))
	assert.Equal(t, "informational", ConfidenceLevel(0.10))
}

func TestSource_Constructor_OISD(t *testing.T) {
	settings := &config.CollectorConfig{
		ParserWorkers:   4,
		ParserBatchSize: 1000,
	}
	collyClient := colly.NewCollector()

	src := NewOISDBigSource(settings, collyClient)
	assert.Equal(t, "oisd-big", src.ID)
	assert.Equal(t, "oisd", src.ProviderID)
	assert.Equal(t, "blocklist", src.Category)
	assert.True(t, src.Enabled)
	assert.Len(t, src.BloomTypes, 2)
	assert.NotNil(t, src.Fetcher)
	assert.NotNil(t, src.Parser)

	nsfw := NewOISDNSFWSource(settings, collyClient)
	assert.Equal(t, "oisd-nsfw", nsfw.ID)
	assert.Equal(t, "oisd", nsfw.ProviderID)
	assert.Equal(t, "nsfw", nsfw.Category)
}

func TestSource_Constructor_URLHaus(t *testing.T) {
	settings := &config.CollectorConfig{
		ParserWorkers:   4,
		ParserBatchSize: 1000,
	}
	collyClient := colly.NewCollector()

	src := NewURLHausSource(settings, collyClient)
	assert.Equal(t, "urlhaus-online", src.ID)
	assert.Equal(t, "abuse-ch", src.ProviderID)
	assert.Equal(t, "malware", src.Category)
	assert.NotNil(t, src.Fetcher)
	assert.NotNil(t, src.Parser)
}

func TestSource_Constructor_OpenPhish(t *testing.T) {
	settings := &config.CollectorConfig{
		ParserWorkers:   4,
		ParserBatchSize: 1000,
	}
	collyClient := colly.NewCollector()

	src := NewOpenPhishSource(settings, collyClient)
	assert.Equal(t, "openphish-feed", src.ID)
	assert.Equal(t, "openphish", src.ProviderID)
	assert.Equal(t, "phishing", src.Category)
	assert.NotNil(t, src.Fetcher)
	assert.NotNil(t, src.Parser)
}

func TestSource_Constructor_PhishTank(t *testing.T) {
	settings := &config.CollectorConfig{
		ParserWorkers:   4,
		ParserBatchSize: 1000,
	}
	collyClient := colly.NewCollector()

	src := NewPhishTankSource(settings, collyClient)
	assert.Equal(t, "phishtank-online-valid", src.ID)
	assert.Equal(t, "phishtank", src.ProviderID)
	assert.Equal(t, "phishing", src.Category)
	assert.NotNil(t, src.Fetcher)
	assert.NotNil(t, src.Parser)
}
