package db

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConnect(t *testing.T) {
	s, err := initializeDatabase()
	if err != nil {
		assert.NoError(t, err)
	}
	defer s.Close()

	assert.NotEmpty(t, s)
}

func TestConnectTest(t *testing.T) {
	SetTesting(true)
	s, err := initializeDatabase()
	if err != nil {
		assert.NoError(t, err)
	}
	defer s.Close()

	assert.NotEmpty(t, s)
}
