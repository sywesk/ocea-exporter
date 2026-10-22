package counterfetcher

import (
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
)

type fetcherMetrics struct {
	mu               sync.RWMutex
	index            *prometheus.GaugeVec
	meterIndex       *prometheus.GaugeVec
	readingTimestamp *prometheus.GaugeVec
	success          prometheus.Gauge
	lastSuccess      prometheus.Gauge
}

func newFetcherMetrics() *fetcherMetrics {
	return &fetcherMetrics{
		index:            prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "ocea_metering_index", Help: "Sum of current meter indexes per fluid."}, []string{"fluid", "local_id"}),
		meterIndex:       prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "ocea_metering_device_index", Help: "Latest available index per current meter."}, []string{"serial", "fluid", "local_id"}),
		readingTimestamp: prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "ocea_metering_reading_timestamp_seconds", Help: "Timestamp of the source reading; dates without an offset are interpreted as UTC."}, []string{"serial", "fluid", "local_id"}),
		success:          prometheus.NewGauge(prometheus.GaugeOpts{Name: "ocea_scrape_success", Help: "Whether the last portal poll and state save succeeded (1 or 0)."}),
		lastSuccess:      prometheus.NewGauge(prometheus.GaugeOpts{Name: "ocea_last_success_timestamp_seconds", Help: "Timestamp of the last successful portal poll and state save."}),
	}
}
func (m *fetcherMetrics) collectors() []prometheus.Collector {
	return []prometheus.Collector{m.index, m.meterIndex, m.readingTimestamp, m.success, m.lastSuccess}
}
func (m *fetcherMetrics) describe(ch chan<- *prometheus.Desc) {
	for _, collector := range m.collectors() {
		collector.Describe(ch)
	}
}
func (m *fetcherMetrics) collect(ch chan<- prometheus.Metric) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	for _, collector := range m.collectors() {
		collector.Collect(ch)
	}
}

func (m *fetcherMetrics) recordSuccess(snapshot state, now time.Time) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.success.Set(1)
	m.lastSuccess.Set(float64(now.Unix()))
	// Remove series for meters and fluids that have left the current inventory.
	m.index.Reset()
	m.meterIndex.Reset()
	m.readingTimestamp.Reset()
	localID := snapshot.AccountData.Local.Local.ID
	totals := make(map[string]float64)
	for _, state := range snapshot.CounterStates {
		totals[string(state.Fluid)] += state.AbsoluteIndex
		m.meterIndex.WithLabelValues(state.SerialNumber, string(state.Fluid), localID).Set(round3(state.AbsoluteIndex))
		for _, layout := range []string{time.RFC3339Nano, "2006-01-02T15:04:05", "2006-01-02"} {
			if date, err := time.Parse(layout, state.ReadingDate); err == nil {
				m.readingTimestamp.WithLabelValues(state.SerialNumber, string(state.Fluid), localID).Set(float64(date.Unix()))
				break
			}
		}
	}
	for fluid, total := range totals {
		m.index.WithLabelValues(fluid, localID).Set(round3(total))
	}
}

func (m *fetcherMetrics) recordFailure() { m.mu.Lock(); defer m.mu.Unlock(); m.success.Set(0) }
