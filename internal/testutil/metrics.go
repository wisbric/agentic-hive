package testutil

import (
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// GaugeValue reads the current value of a prometheus.Gauge.
// Returns 0 if the gauge is nil or cannot be read.
func GaugeValue(g prometheus.Gauge) float64 {
	if g == nil {
		return 0
	}
	var m dto.Metric
	if err := g.(prometheus.Metric).Write(&m); err != nil {
		return 0
	}
	if m.Gauge != nil {
		return m.Gauge.GetValue()
	}
	return 0
}
