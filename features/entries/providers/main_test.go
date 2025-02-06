package providers

import (
	"blacked/internal/db"
	"blacked/internal/logger"
	"blacked/internal/utils"
	"os"
	"testing"

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
