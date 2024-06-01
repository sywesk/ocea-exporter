package counterfetcher

import (
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

var (
	index = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ocea",
		Subsystem: "metering",
		Name:      "index",
	}, []string{"fluid", "local_id"})

	meterIndex = promauto.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "ocea",
		Subsystem: "metering",
		Name:      "device_index",
	}, []string{"serial", "fluid", "local_id"})
)
