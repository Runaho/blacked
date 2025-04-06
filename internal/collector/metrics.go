package collector

import (
	"fmt"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	once sync.Once
	mc   *MetricsCollector
)

type ProviderMetrics struct {
	SyncStatus   string
	ProviderName string
}

type MetricsCollector struct {
	providerMetrics  map[string]*ProviderMetrics // Keep providerMetrics map for *status* tracking (optional, can be removed if not needed)
	syncCount        *prometheus.CounterVec      // Counter for total syncs initiated per provider
	syncSuccessCount *prometheus.CounterVec      // Counter for successful syncs per provider
	syncFailedCount  *prometheus.CounterVec      // Counter for failed syncs per provider
	syncDuration     *prometheus.GaugeVec        // Gauge for sync duration (or use Histogram/Summary for more stats)
	entriesSaved     *prometheus.CounterVec      // Counter for saved entries per provider
	entriesDeleted   *prometheus.CounterVec      // Counter for deleted entries per provider
	entriesProcessed *prometheus.GaugeVec        // Gauge for total processed entries

	ImportRequestsTotal *prometheus.CounterVec // Counter for total import requests received
	EntriesParsedTotal  *prometheus.CounterVec // Counter for total blacklist entries parsed from
	EntriesSavedTotal   *prometheus.CounterVec // Counter for total blacklist entries saved from import
	ImportErrorsTotal   *prometheus.CounterVec // Counter for total import requests that resulted in errors
}

func GetMetricsCollector() (*MetricsCollector, error) {
	if mc == nil {
		return nil, fmt.Errorf("MetricsCollector not initialized")
	}
	return mc, nil
}

// NewMetricsCollector - Initialize Prometheus metrics here
func NewMetricsCollector(providerNames []string) *MetricsCollector {
	once.Do(func() {
		_mc := &MetricsCollector{
			providerMetrics: make(map[string]*ProviderMetrics),

			syncCount: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_provider_sync_total",
				Help: "Total number of blacklist sync operations initiated by provider.",
			}, []string{"provider"}),

			syncSuccessCount: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_provider_sync_success_total",
				Help: "Total number of successful blacklist sync operations by provider.",
			}, []string{"provider"}),

			syncFailedCount: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_provider_sync_failed_total",
				Help: "Total number of failed blacklist sync operations by provider.",
			}, []string{"provider"}),

			syncDuration: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: "blacklist_provider_sync_duration_seconds",
				Help: "Duration of blacklist sync operations in seconds.",
			}, []string{"provider"}),

			entriesSaved: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_entries_saved_total",
				Help: "Total number of blacklist entries saved during sync by provider.",
			}, []string{"provider"}),

			entriesDeleted: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_entries_deleted_total",
				Help: "Total number of blacklist entries deleted during sync by provider.",
			}, []string{"provider"}),

			entriesProcessed: promauto.NewGaugeVec(prometheus.GaugeOpts{ // Gauge as it is a value at a point in time, could also be Counter if you are counting cumulatively.
				Name: "blacklist_entries_processed_total",
				Help: "Total number of entries processed by provider during last sync.",
			}, []string{"provider"}),

			ImportRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_json_import_requests_total",
				Help: "Total number of import requests received.",
			}, []string{"provider"}),

			EntriesParsedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_json_entries_parsed_total",
				Help: "Total number of blacklist entries parsed from import requests.",
			}, []string{"provider"}),

			EntriesSavedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_json_entries_saved_total",
				Help: "Total number of blacklist entries saved into database from import requests.",
			}, []string{"provider"}),

			ImportErrorsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacklist_json_import_errors_total",
				Help: "Total number of import requests that resulted in errors.",
			}, []string{"provider"}),
		}
		// Populate _mc’s providerMetrics
		for _, name := range providerNames {
			_mc.providerMetrics[name] = &ProviderMetrics{SyncStatus: "idle"}
		}

		// After _mc is fully ready, set the global mc
		mc = _mc
	})

	return mc
}

// GetProviderMetrics -  Mostly for status tracking (optional now). Prometheus metrics themselves handle aggregation
func (mc *MetricsCollector) GetProviderMetrics(providerName string) *ProviderMetrics {
	if _, exists := mc.providerMetrics[providerName]; !exists {
		mc.providerMetrics[providerName] = &ProviderMetrics{SyncStatus: "idle"}
	}
	return mc.providerMetrics[providerName]
}

// SetSyncRunning - Now updates Prometheus counter and status
func (mc *MetricsCollector) SetSyncRunning(providerName string) {
	metrics := mc.GetProviderMetrics(providerName)
	metrics.SyncStatus = "running"
	mc.syncCount.With(prometheus.Labels{"provider": providerName}).Inc()     // Increment sync count in Prometheus.
	mc.syncDuration.With(prometheus.Labels{"provider": providerName}).Set(0) // Reset duration gauge for new run
}

// SetSyncSuccess - Update Prometheus counter and status
func (mc *MetricsCollector) SetSyncSuccess(providerName string, duration time.Duration) {
	metrics := mc.GetProviderMetrics(providerName)
	metrics.SyncStatus = "success"
	mc.syncSuccessCount.With(prometheus.Labels{"provider": providerName}).Inc()               // Increment success count
	mc.syncDuration.With(prometheus.Labels{"provider": providerName}).Set(duration.Seconds()) // Set duration gauge in seconds
}

// SetSyncFailed - Update Prometheus counters/gauges and status
func (mc *MetricsCollector) SetSyncFailed(providerName string, err error, duration time.Duration) {
	metrics := mc.GetProviderMetrics(providerName)
	metrics.SyncStatus = "failed"
	// No increment to syncSuccessCount, only increment failed count:
	mc.syncFailedCount.With(prometheus.Labels{"provider": providerName}).Inc()
	mc.syncDuration.With(prometheus.Labels{"provider": providerName}).Set(duration.Seconds()) // Still record duration (even if failed - useful for timeout analysis)
	// Error not directly stored in Prometheus metric (Prometheus metrics are typically numerical),
	// consider logging the error message separately if needed for detailed error analysis.
}

// IncrementSavedCount - Update Prometheus counter
func (mc *MetricsCollector) IncrementSavedCount(providerName string, count int) {
	mc.entriesSaved.With(prometheus.Labels{"provider": providerName}).Add(float64(count))
}

// IncrementDeletedCount - Update Prometheus counter
func (mc *MetricsCollector) IncrementDeletedCount(providerName string, count int) {
	mc.entriesDeleted.With(prometheus.Labels{"provider": providerName}).Add(float64(count))
}

// SetTotalProcessed - Update Prometheus gauge
func (mc *MetricsCollector) SetTotalProcessed(providerName string, count int) {
	mc.entriesProcessed.With(prometheus.Labels{"provider": providerName}).Set(float64(count))
}

func (mc *MetricsCollector) IncrementImportRequests(providerName string) {
	mc.ImportRequestsTotal.With(prometheus.Labels{"provider": providerName}).Inc()
}

func (mc *MetricsCollector) IncrementEntriesParsed(providerName string, count int) {
	mc.EntriesParsedTotal.With(prometheus.Labels{"provider": providerName}).Add(float64(count))
}

func (mc *MetricsCollector) IncrementEntriesSaved(providerName string, count int) {
	mc.EntriesSavedTotal.With(prometheus.Labels{"provider": providerName}).Add(float64(count))
}

func (mc *MetricsCollector) IncrementImportErrors(providerName string) {
	mc.ImportErrorsTotal.With(prometheus.Labels{"provider": providerName}).Inc()
}

// GetAllProviderMetrics - For status tracking (optional, Prometheus has aggregated data directly). Can return less info now.
func (mc *MetricsCollector) GetAllProviderMetrics() map[string]*ProviderMetrics {
	// Returning less detailed metrics here - Prometheus is intended for detailed metrics access now.
	metricsCopy := make(map[string]*ProviderMetrics, len(mc.providerMetrics))
	for name, metrics := range mc.providerMetrics {
		metricsCopy[name] = &ProviderMetrics{
			ProviderName: name,               // Keep provider name maybe for easier correlation.
			SyncStatus:   metrics.SyncStatus, // Just status, other details are in Prometheus now.
		}
	}
	return metricsCopy
}

// String method for ProviderMetrics - Can be simplified.
func (pm *ProviderMetrics) String() string {
	return fmt.Sprintf(`Provider: %s, Status: %s (Detailed metrics in Prometheus)`,
		pm.ProviderName, pm.SyncStatus) // Simpler string now. Detailed metrics are in Prometheus.
}
