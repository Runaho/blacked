package providers

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTryStartProcess_RetryLogic(t *testing.T) {
	pm := &ProcessManager{
		maxHistory: 100,
		history:    make([]*ProcessStatus, 0, 100),
	}

	ctx := context.Background()

	// Start first process
	processID1, err := pm.TryStartProcess(ctx, "test", []string{"provider1"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, processID1)

	// Finish first process immediately to allow retry to succeed
	pm.FinishProcess(processID1, nil)

	// Try to start second process - should succeed immediately (no contention)
	processID2, err := pm.TryStartProcess(ctx, "test", []string{"provider2"}, nil)
	require.NoError(t, err)
	require.NotEmpty(t, processID2)
	assert.NotEqual(t, processID1, processID2)

	// Clean up
	pm.FinishProcess(processID2, nil)
}

func TestTryStartProcess_ImmediateFailureAfterMaxRetries(t *testing.T) {
	pm := &ProcessManager{
		maxHistory: 100,
		history:    make([]*ProcessStatus, 0, 100),
	}

	ctx := context.Background()

	// Start a process that never finishes
	processID1, err := pm.TryStartProcess(ctx, "test", []string{"provider1"}, nil)
	require.NoError(t, err)

	// Create a context with very short timeout
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// Try to start another process - should fail after retries
	_, err = pm.TryStartProcess(ctx, "test", []string{"provider2"}, nil)
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)

	// Clean up
	pm.FinishProcess(processID1, nil)
}

func TestTryStartProcess_ContextCancellation(t *testing.T) {
	pm := &ProcessManager{
		maxHistory: 100,
		history:    make([]*ProcessStatus, 0, 100),
	}

	// Start a process that never finishes
	ctx := context.Background()
	processID1, err := pm.TryStartProcess(ctx, "test", []string{"provider1"}, nil)
	require.NoError(t, err)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Try to start another process - should immediately return cancelled error
	_, err = pm.TryStartProcess(ctx, "test", []string{"provider2"}, nil)
	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)

	// Clean up
	pm.FinishProcess(processID1, nil)
}
