package bloom

import (
	"sync"
	"testing"
)

// TestResetSourceRaceCondition tests that ResetSource doesn't cause race conditions
// when concurrent Test() calls are happening.
func TestResetSourceRaceCondition(t *testing.T) {
	bs := NewBloomSet(BloomDomain, 1000)
	
	// Add some initial data
	bs.Add("source1", "test1.example.com")
	bs.Add("source1", "test2.example.com")
	bs.Add("source2", "test3.example.com")
	
	var wg sync.WaitGroup
	
	// Start multiple goroutines that continuously test the filter
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 1000; j++ {
				// Test existing keys
				_ = bs.Test("test1.example.com")
				_ = bs.Test("test2.example.com")
				_ = bs.Test("test3.example.com")
				// Test non-existing keys
				_ = bs.Test("nonexistent.example.com")
			}
		}()
	}
	
	// While tests are running, reset sources multiple times
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			bs.ResetSource("source1")
		}()
		
		wg.Add(1)
		go func() {
			defer wg.Done()
			bs.ResetSource("source2")
		}()
	}
	
	wg.Wait()
	
	// Verify final state
	if bs.Test("test1.example.com") || bs.Test("test2.example.com") || bs.Test("test3.example.com") {
		t.Error("Expected all keys to be removed after source resets")
	}
}

// TestNilReceiverSafety tests that methods handle nil receivers gracefully
func TestNilReceiverSafety(t *testing.T) {
	var bs *BloomSet
	
	// These should not panic
	if bs.Test("test") {
		t.Error("Expected false for nil receiver")
	}
	
	if bs.TestSource("source", "test") {
		t.Error("Expected false for nil receiver")
	}
	
	if bs.SourceCount() != 0 {
		t.Error("Expected 0 for nil receiver")
	}
	
	if bs.TotalKeys() != 0 {
		t.Error("Expected 0 for nil receiver")
	}
}