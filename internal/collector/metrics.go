package collector

import (
	"errors"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	once sync.Once
	mc   *MetricsCollector

	// Error variables for collector metrics
	ErrMetricsCollectorNotInitialized = errors.New("metrics collector not initialized")
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

	// Business-level metrics for query operations
	CheckTotal         *prometheus.CounterVec // Counter for total /check requests (bloom only)
	HitTotal           *prometheus.CounterVec // Counter for total /hit requests (full lookup)
	HitBlockedTotal    *prometheus.CounterVec // Counter for /hit requests that found a match (blocked)
	HitAllowedTotal    *prometheus.CounterVec // Counter for /hit requests with no match (allowed)
	BulkCheckTotal     *prometheus.CounterVec // Counter for total /bulk-check requests
	BulkHitTotal       *prometheus.CounterVec // Counter for total /bulk-hit requests
	BulkHitBlockedTotal *prometheus.CounterVec // Counter for /bulk-hit requests with at least one blocked

	// Queue and backpressure metrics for DB write channel
	QueueDepth         *prometheus.GaugeVec   // Current queue depth (number of batches waiting)
	QueueHighWatermark  *prometheus.GaugeVec   // High watermark (peak queue depth)
	BackpressureEvents  *prometheus.CounterVec // Count of backpressure activation events
	QueueDroppedBatches *prometheus.CounterVec // Count of batches dropped due to saturation

	// Per-provider fetch metrics
	ProviderRequestsTotal      *prometheus.CounterVec   // HTTP requests made per provider, labeled by status (success/failure)
	ProviderPagesFetchedTotal  *prometheus.CounterVec   // Pages fetched per provider (for multi-page providers like AlienVault)
	ProviderBytesTransferredTotal *prometheus.CounterVec // Bytes downloaded per provider
	ProviderFetchDurationSeconds *prometheus.HistogramVec // Fetch time histogram per provider
}

func GetMetricsCollector() (*MetricsCollector, error) {
	if mc == nil {
		return nil, ErrMetricsCollectorNotInitialized
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
				Name: "entries_saved_total",
				Help: "Total number of blacklist entries saved during sync by provider.",
			}, []string{"provider"}),

			entriesDeleted: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "entries_deleted_total",
				Help: "Total number of blacklist entries deleted during sync by provider.",
			}, []string{"provider"}),

			entriesProcessed: promauto.NewGaugeVec(prometheus.GaugeOpts{ // Gauge as it is a value at a point in time, could also be Counter if you are counting cumulatively.
				Name: "entries_processed_total",
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

			// Business-level query metrics
			CheckTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_url_check_total",
				Help: "Total number of /check requests (bloom-only).",
			}, nil),

			HitTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_hit_total",
				Help: "Total number of /hit requests (full bloom+DB+score lookup).",
			}, nil),

			HitBlockedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_hit_blocked_total",
				Help: "Total number of /hit requests that found a blocked match.",
			}, nil),

			HitAllowedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_hit_allowed_total",
				Help: "Total number of /hit requests with no blocked match.",
			}, nil),

			BulkCheckTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_bulk_check_total",
				Help: "Total number of /bulk-check requests.",
			}, nil),

			BulkHitTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_bulk_hit_total",
				Help: "Total number of /bulk-hit requests.",
			}, nil),

			BulkHitBlockedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_bulk_hit_blocked_total",
				Help: "Total number of /bulk-hit requests with at least one blocked URL.",
			}, nil),

			// Queue and backpressure metrics
			QueueDepth: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: "blacked_db_queue_depth",
				Help: "Current number of batches waiting in the DB write queue.",
			}, nil),

			QueueHighWatermark: promauto.NewGaugeVec(prometheus.GaugeOpts{
				Name: "blacked_db_queue_high_watermark",
				Help: "Peak queue depth recorded since startup (high watermark).",
			}, nil),

			BackpressureEvents: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_backpressure_events_total",
				Help: "Number of times backpressure was activated (queue reached high threshold).",
			}, nil),

			QueueDroppedBatches: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_queue_dropped_batches_total",
				Help: "Number of batches dropped due to queue saturation.",
			}, nil),

			// Per-provider fetch metrics
			ProviderRequestsTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_provider_requests_total",
				Help: "Total number of HTTP requests made per provider, labeled by status.",
			}, []string{"provider", "status"}),

			ProviderPagesFetchedTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_provider_pages_fetched_total",
				Help: "Total number of pages fetched per provider (for multi-page providers like AlienVault).",
			}, []string{"provider"}),

			ProviderBytesTransferredTotal: promauto.NewCounterVec(prometheus.CounterOpts{
				Name: "blacked_provider_bytes_transferred_total",
				Help: "Total bytes downloaded per provider.",
			}, []string{"provider"}),

			ProviderFetchDurationSeconds: promauto.NewHistogramVec(prometheus.HistogramOpts{
				Name:    "blacked_provider_fetch_duration_seconds",
				Help:    "HTTP fetch duration histogram per provider.",
				Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
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
	if mc == nil {
		return
	}
	metrics := mc.GetProviderMetrics(providerName)
	if metrics == nil {
		return
	}
	metrics.SyncStatus = "running"
	mc.syncCount.With(prometheus.Labels{"provider": providerName}).Inc()     // Increment sync count in Prometheus.
	mc.syncDuration.With(prometheus.Labels{"provider": providerName}).Set(0) // Reset duration gauge for new run
}

// SetSyncSuccess - Update Prometheus counter and status
func (mc *MetricsCollector) SetSyncSuccess(providerName string, duration time.Duration) {
	if mc == nil {
		return
	}
	metrics := mc.GetProviderMetrics(providerName)
	if metrics == nil {
		return
	}
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

// -- Business query metrics helpers --

// RecordCheck increments the /check request counter.
func (mc *MetricsCollector) RecordCheck() {
	mc.CheckTotal.With(nil).Inc()
}

// RecordHit increments the /hit request counter and whether it was blocked.
func (mc *MetricsCollector) RecordHit(blocked bool) {
	mc.HitTotal.With(nil).Inc()
	if blocked {
		mc.HitBlockedTotal.With(nil).Inc()
	} else {
		mc.HitAllowedTotal.With(nil).Inc()
	}
}

// RecordBulkCheck increments the /bulk-check request counter.
func (mc *MetricsCollector) RecordBulkCheck() {
	mc.BulkCheckTotal.With(nil).Inc()
}

// RecordBulkHit increments the /bulk-hit request counter and whether any URL was blocked.
func (mc *MetricsCollector) RecordBulkHit(anyBlocked bool) {
	mc.BulkHitTotal.With(nil).Inc()
	if anyBlocked {
		mc.BulkHitBlockedTotal.With(nil).Inc()
	}
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
	return "Provider: " + pm.ProviderName + ", Status: " + pm.SyncStatus + " (Detailed metrics in Prometheus)"
}

// -- Queue and backpressure metrics helpers --

// UpdateQueueDepth sets the current queue depth and optionally updates the high watermark.
func (mc *MetricsCollector) UpdateQueueDepth(depth int) {
	mc.QueueDepth.With(nil).Set(float64(depth))
}

// UpdateQueueHighWatermark sets the high watermark value.
func (mc *MetricsCollector) UpdateQueueHighWatermark(highWatermark int) {
	mc.QueueHighWatermark.With(nil).Set(float64(highWatermark))
}

// RecordBackpressureEvent increments the backpressure event counter.
func (mc *MetricsCollector) RecordBackpressureEvent() {
	mc.BackpressureEvents.With(nil).Inc()
}

// RecordDroppedBatch increments the dropped batch counter and returns true if counter was updated.
func (mc *MetricsCollector) RecordDroppedBatch(batchSize int) {
	mc.QueueDroppedBatches.With(nil).Add(float64(batchSize))
}

// -- Per-provider fetch metrics helpers --

// RecordProviderRequest increments the HTTP request counter with status label.
// status should be "success" or "failure".
func (mc *MetricsCollector) RecordProviderRequest(providerName, status string) {
	mc.ProviderRequestsTotal.With(prometheus.Labels{"provider": providerName, "status": status}).Inc()
}

// RecordProviderPagesFetched increments the pages-fetched counter for multi-page providers.
func (mc *MetricsCollector) RecordProviderPagesFetched(providerName string) {
	mc.ProviderPagesFetchedTotal.With(prometheus.Labels{"provider": providerName}).Inc()
}

// RecordProviderBytesTransferred records bytes downloaded for a provider.
func (mc *MetricsCollector) RecordProviderBytesTransferred(providerName string, bytes int64) {
	mc.ProviderBytesTransferredTotal.With(prometheus.Labels{"provider": providerName}).Add(float64(bytes))
}

// RecordProviderFetchDuration records the HTTP fetch duration for a provider.
func (mc *MetricsCollector) RecordProviderFetchDuration(providerName string, duration time.Duration) {
	mc.ProviderFetchDurationSeconds.With(prometheus.Labels{"provider": providerName}).Observe(duration.Seconds())
}
