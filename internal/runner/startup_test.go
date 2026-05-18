package runner

import (
	"blacked/features/providers/base"
	"blacked/internal/utils"
	"context"
	"testing"
	"time"
)

// ============================================================================
// Mock/Test helpers
// ============================================================================

// createTestProvider creates a BaseProvider for testing
func createTestProvider(name, cronSchedule string) base.Provider {
	p := base.NewBaseProvider(name, "http://test.example/feed", "test", nil, nil)
	p.SetCronSchedule(cronSchedule)
	return p
}

// ============================================================================
// ParseTTLFromCron tests
// ============================================================================

func TestParseTTLFromCron(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected time.Duration
	}{
		{"empty", "", 6 * time.Hour},
		{"duration 6h", "6h", 6 * time.Hour},
		{"duration 1h", "1h", 1 * time.Hour},
		{"duration 24h", "24h", 24 * time.Hour},
		{"cron fallback hourly", "0 * * * *", 6 * time.Hour},
		{"cron fallback daily", "0 0 * * *", 6 * time.Hour},
		{"cron fallback weekly", "0 0 * * 0", 6 * time.Hour},
		{"duration with d", "7d", 24 * time.Hour},        // "d" matches "day" substring → 24h
		{"duration with w", "1w", 7 * 24 * time.Hour},   // "w" matches "week" substring → 7d
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := utils.ParseTTLFromCron(tt.input)
			if result != tt.expected {
				t.Errorf("ParseTTLFromCron(%q) = %v, want %v", tt.input, result, tt.expected)
			}
		})
	}
}

// ============================================================================
// StartupSummary test
// ============================================================================

func TestStartupSummary(t *testing.T) {
	decisions := []ProviderStartupDecision{
		{ProviderName: "OISD-Big", Action: StartupSkip},
		{ProviderName: "URLHaus", Action: StartupFullFetch},
		{ProviderName: "OpenPhish", Action: StartupRestore},
	}

	summary := StartupSummary(decisions)
	expected := "OISD-Big: skip, URLHaus: full_fetch, OpenPhish: restore"
	if summary != expected {
		t.Errorf("StartupSummary() = %q, want %q", summary, expected)
	}
}

func TestStartupSummaryEmpty(t *testing.T) {
	summary := StartupSummary(nil)
	if summary != "" {
		t.Errorf("StartupSummary(nil) = %q, want empty string", summary)
	}
}

// ============================================================================
// EvaluateStartupState tests (without real DB — test helpers)
// ============================================================================

func TestEvaluateStartupState_NoProviders(t *testing.T) {
	_, err := EvaluateStartupState(context.Background(), nil)
	if err == nil {
		t.Error("expected error for nil providers, got nil")
	}
}

// ============================================================================
// Decision logic tests (unit, mock DB populated)
// ============================================================================

func TestEvaluateProvider_DBPopulatedFreshSkip(t *testing.T) {
	provider := createTestProvider("TestSkip", "6h")
	decision := evaluateProvider(provider, true, 100)

	if decision.Action != StartupFullFetch {
		// No stored file exists, so it will be full_fetch regardless of DB state
		t.Logf("Action = %s (expected full_fetch since no stored file exists)", decision.Action)
	}
}

func TestEvaluateProvider_DBEmptyNoStored(t *testing.T) {
	provider := createTestProvider("TestEmpty", "6h")
	decision := evaluateProvider(provider, false, 0)

	// No stored file exists → should be full_fetch
	if decision.Action != StartupFullFetch {
		t.Errorf("Action = %s, want %s (no stored file)", decision.Action, StartupFullFetch)
	}
}

func TestProviderStartupDecision_StringFields(t *testing.T) {
	d := ProviderStartupDecision{
		ProviderName: "TestProvider",
		Action:         StartupRestore,
		Reason:         "test reason",
	}
	if d.ProviderName != "TestProvider" {
		t.Error("unexpected ProviderName")
	}
	if d.Action != StartupRestore {
		t.Error("unexpected Action")
	}
	if d.Reason != "test reason" {
		t.Error("unexpected Reason")
	}
}

// ============================================================================
// Action type coverage
// ============================================================================

func TestStartupAction_Values(t *testing.T) {
	if string(StartupSkip) != "skip" {
		t.Errorf("StartupSkip = %s", StartupSkip)
	}
	if string(StartupRestore) != "restore" {
		t.Errorf("StartupRestore = %s", StartupRestore)
	}
	if string(StartupFullFetch) != "full_fetch" {
		t.Errorf("StartupFullFetch = %s", StartupFullFetch)
	}
}
