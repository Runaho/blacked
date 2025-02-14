package providers

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/db"
	"blacked/internal/logger"
	"blacked/internal/utils"
	"bufio"
	"bytes"
	"context"
	"io"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	logger.InitializeLogger()
	db.GetTestDB()
	db.EnsureDBSchemaExists(db.WithTesting(true))

	code := m.Run()

	os.Exit(code)
}

func TestNewProviders(t *testing.T) {
	_, db, _, err := utils.Initialize(t)
	defer db.Close()

	assert.NoError(t, err, "Expected no error initializing providers")
	// Test that we have sources and names
	ps, err := NewProviders()
	assert.NoError(t, err, "Expected no error creating new providers")
	assert.NotEmpty(t, ps, "Expected some providers to be returned")

	names := ps.Names()
	sources := ps.Sources()
	assert.Equal(t, len(*ps), len(names), "Length of providers should match length of provider names.")
	assert.Equal(t, len(*ps), len(sources), "Length of providers should match length of provider sources.")
}

func TestProvidersProcess(t *testing.T) {
	logger.InitializeLogger()
	_, db, _, err := utils.Initialize(t)
	assert.NoError(t, err, "Expected no error initializing providers")
	defer db.Close()
	// Test that we have sources and names
	ps, err := NewProviders()
	assert.NoError(t, err, "Expected no error creating new providers")
	assert.NotEmpty(t, ps, "Expected some providers to be returned")

	err = ps.Process()
	assert.Nil(t, err, "Expecting no error or handle gracefully based on your environment or mocks")
}

func TestCheckIfLinkExists(t *testing.T) {
	logger.InitializeLogger()

	ctx, testDB, _, err := utils.Initialize(t)
	assert.NoError(t, err, "Expected no error initializing providers")
	defer testDB.Close()

	dbRepo := repository.NewSQLiteRepository(testDB)

	testCases := []struct {
		name         string
		link         string
		expectedHits []entries.Hit
	}{
		{
			name: "Exact URL match",
			link: "0124498474f7c13ac9a2-6b191446002b31342189d56cabcf5227.r11.cf2.rackcdn.com",
			expectedHits: []entries.Hit{{
				MatchType:    "EXACT_URL",
				MatchedValue: "0124498474f7c13ac9a2-6b191446002b31342189d56cabcf5227.r11.cf2.rackcdn.com",
			}},
		},
		{
			name: "Host match",
			link: "0jaqkuc24kdjvpgdc8va.maherstcottage.com.au",
			expectedHits: []entries.Hit{{
				MatchType:    "HOST",
				MatchedValue: "0jaqkuc24kdjvpgdc8va.maherstcottage.com.au",
			}},
		},
		{
			name: "source url match",
			link: "https://userauthme02.com/com8K70UXaW9Smnd.html",
			expectedHits: []entries.Hit{{
				MatchType:    "EXACT_URL",
				MatchedValue: "https://userauthme02.com/com8K70UXaW9Smnd.html",
			}},
		},
		{
			name: "path match",
			link: "http://5.175.249.223/hiddenbin/boatnet.ppc",
			expectedHits: []entries.Hit{{
				MatchType:    "PATH",
				MatchedValue: "/hiddenbin/boatnet.ppc",
			}},
		},
		{
			name:         "No match",
			link:         "https://www.you-shall-not.find",
			expectedHits: []entries.Hit{},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			log.Info().Msgf("Running test case: %s with link: %s", tc.name, tc.link)
			hits, err := dbRepo.QueryLink(ctx, tc.link)
			assert.NoError(t, err, "Expected no error from CheckIfLinkExists")

			if len(tc.expectedHits) > 0 {
				for i, hit := range hits {
					log.Info().Msgf("Hit %d: %+v", i, hit)
				}
				assert.Equal(t, len(tc.expectedHits) > 0, len(hits) > 0, "Expected 'found' to be equal")
			} else {
				log.Info().Msgf("Expected no hits for link: %s, current hits: %v", tc.link, hits)

				ids := make([]string, len(hits))
				for _, hit := range hits {
					ids = append(ids, hit.ID)
				}

				entries, _ := dbRepo.GetEntriesByIDs(ctx, ids)
				log.Info().Msgf("Entries: %v", entries)

				assert.Equal(t, len(tc.expectedHits), len(hits), "Expected 'found' to be equal")
			}
		})
	}
}

func TestProcess_ConcurrentProviders(t *testing.T) {
	// ... setup test database, colly client ...
	_, db, _, err := utils.Initialize(t)
	defer db.Close()

	mockProviders := Providers{
		&MockProvider{NameVal: "MockProvider1", SourceVal: "mock://provider1", FetchDelay: 100 * time.Millisecond, ParseDelay: 50 * time.Millisecond},
		&MockProvider{NameVal: "MockProvider2", SourceVal: "mock://provider2", FetchDelay: 150 * time.Millisecond, ParseDelay: 75 * time.Millisecond},
	}

	startTime := time.Now()
	err = mockProviders.Process() // Process the mock providers
	endTime := time.Now()

	assert.NoError(t, err)

	duration := endTime.Sub(startTime)
	log.Info().Dur("duration", duration).Msg("Total processing time")

	// Assert that total duration is *less* than the sum of individual provider delays
	// if they were processed sequentially.  This indicates concurrency.
	expectedSequentialDuration := (mockProviders[0].(*MockProvider).FetchDelay + mockProviders[0].(*MockProvider).ParseDelay) +
		(mockProviders[1].(*MockProvider).FetchDelay + mockProviders[1].(*MockProvider).ParseDelay)
	assert.Less(t, duration, expectedSequentialDuration, "Expected concurrent processing to be faster than sequential")

}

// MockProvider (Example - you'll need to flesh this out more)
type MockProvider struct {
	NameVal    string
	SourceVal  string
	FetchDelay time.Duration
	ParseDelay time.Duration
	Repository repository.BlacklistRepository
	ProcessID  uuid.UUID
}

func (m *MockProvider) Name() string                                      { return m.NameVal }
func (m *MockProvider) Source() string                                    { return m.SourceVal }
func (m *MockProvider) SetRepository(repo repository.BlacklistRepository) { m.Repository = repo }
func (m *MockProvider) SetProcessID(id uuid.UUID)                         { m.ProcessID = id }
func (m *MockProvider) Fetch() (io.Reader, error) {
	time.Sleep(m.FetchDelay)                              // Simulate fetch delay
	return bytes.NewReader([]byte("line1\nline2\n")), nil // Example data
}
func (m *MockProvider) Parse(data io.Reader) error {
	time.Sleep(m.ParseDelay) // Simulate parse delay
	scanner := bufio.NewScanner(data)
	for scanner.Scan() {
		line := scanner.Text()
		entry := entries.Entry{
			ID:        uuid.New().String(),
			ProcessID: m.ProcessID.String(),
			Source:    m.Name(),
			SourceURL: m.Source(),
			CreatedAt: time.Now(),
			UpdatedAt: time.Now(),
		}
		entry.SetURL(line)
		m.Repository.SaveEntry(context.Background(), entry) // Or BatchSaveEntries
	}
	return scanner.Err()
}
