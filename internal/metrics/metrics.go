package metrics

import "github.com/prometheus/client_golang/prometheus"

var (
	SSHConnectionsActive       prometheus.Gauge
	SessionsActive             *prometheus.GaugeVec
	WebSocketConnectionsActive prometheus.Gauge
	HTTPRequestsTotal          *prometheus.CounterVec
	HTTPRequestDuration        *prometheus.HistogramVec
	AuthFailuresTotal          *prometheus.CounterVec
	SSHErrorsTotal             *prometheus.CounterVec
)

// Init registers all application metrics with the default Prometheus registry.
// Call once from main before wiring any other components.
func Init() {
	SSHConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentic_hive_ssh_connections_active",
		Help: "Current cached SSH connections in the pool.",
	})

	SessionsActive = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Name: "agentic_hive_sessions_active",
		Help: "Active tmux sessions per server (updated each poll cycle).",
	}, []string{"server_id"})

	WebSocketConnectionsActive = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "agentic_hive_websocket_connections_active",
		Help: "Current terminal WebSocket connections.",
	})

	HTTPRequestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentic_hive_http_requests_total",
		Help: "Total HTTP requests by method, path pattern, and status.",
	}, []string{"method", "path", "status"})

	HTTPRequestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "agentic_hive_http_request_duration_seconds",
		Help:    "HTTP request latency by method and path pattern.",
		Buckets: []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5},
	}, []string{"method", "path"})

	AuthFailuresTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentic_hive_auth_failures_total",
		Help: "Login failures by reason (invalid_credentials, token_expired, token_invalid).",
	}, []string{"reason"})

	SSHErrorsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "agentic_hive_ssh_errors_total",
		Help: "SSH connection/exec errors per server.",
	}, []string{"server_id"})

	prometheus.MustRegister(
		SSHConnectionsActive,
		SessionsActive,
		WebSocketConnectionsActive,
		HTTPRequestsTotal,
		HTTPRequestDuration,
		AuthFailuresTotal,
		SSHErrorsTotal,
	)

	// Pre-initialise known label sets so that HELP/TYPE lines appear at startup
	// before any real observations arrive.
	for _, reason := range []string{"invalid_credentials", "token_expired", "token_invalid"} {
		AuthFailuresTotal.WithLabelValues(reason).Add(0)
	}
}
