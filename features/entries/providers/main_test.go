package providers

import (
	"blacked/features/entries"
	"blacked/features/entries/repository"
	"blacked/internal/db"
	"blacked/internal/logger"
	"blacked/internal/utils"
	"os"
	"testing"

	"github.com/rs/zerolog/log"
	"github.com/stretchr/testify/assert"
)

func TestMain(m *testing.M) {
	// Optionally, you can do setup here, such as initializing logger or environment.
	// For instance:
	logger.InitializeLogger() // if you wanted logging in tests
	db.SetTesting(true)

	code := m.Run()

	// Optionally, do teardown here.

	os.Exit(code)
}

func TestNewProviders(t *testing.T) {
	_, db, _, err := utils.Initialize(t)
	defer db.Close()

	assert.NoError(t, err, "Expected no error initializing providers")
	// Test that we have sources and names
	//
	ps, err := NewProviders(db)
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
	ps, err := NewProviders(db)
	assert.NoError(t, err, "Expected no error creating new providers")
	assert.NotEmpty(t, ps, "Expected some providers to be returned")

	// NOTE: By default, calling ps.Process() attempts a real fetch (HTTP).
	// For an integration test, you might allow it or skip if you lack network, etc.
	// Here, we simply ensure it does not panic and returns an error or nil.
	err = ps.Process()
	// If an external fetch is genuinely attempted, an error can occur if the URL is unreachable.
	// So you might do:
	//   assert.Error(t, err, "Expecting an error because fetch might fail without a network or mocking.")
	// OR if your environment can access the OISD resource, you might do:
	//   assert.NoError(t, err)
	// For demonstration, we’ll just check it’s “not panicking”:
	assert.Nil(t, err, "Expecting no error or handle gracefully based on your environment or mocks")
}

func TestCheckIfLinkExists(t *testing.T) {
	logger.InitializeLogger()

	ctx, testDB, _, err := utils.Initialize(t)
	assert.NoError(t, err, "Expected no error initializing providers")
	defer testDB.Close()

	//ps, err := NewProviders(testDB)
	//assert.NoError(t, err, "Expected no error creating new providers")
	//assert.NotEmpty(t, ps, "Expected some providers to be returned")
	//err = ps.Process()
	//assert.Nil(t, err, "Expecting no error or handle gracefully based on your environment or mocks")
	//assert.NoError(t, err, "Error inserting test data")

	dbRepo := repository.NewDuckDBRepository(testDB)

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
