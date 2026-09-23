// Package metrics exposes a small set of Prometheus counters for the gateway:
// audit events (which covers sign-ins, 2FA outcomes, exports and downloads —
// see internal/audit.Logger.Log), telnet connections and throttling, and
// notification deliveries. Nothing here is imported by the CLI's one-shot
// commands; it only matters while the gateway (telnet/web) is running.
package metrics

import (
	"context"
	"errors"
	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var reg = prometheus.NewRegistry()

func counterVec(name, help string, labels ...string) *prometheus.CounterVec {
	c := prometheus.NewCounterVec(prometheus.CounterOpts{Name: name, Help: help}, labels)
	reg.MustRegister(c)
	return c
}

func counter(name, help string) prometheus.Counter {
	c := prometheus.NewCounter(prometheus.CounterOpts{Name: name, Help: help})
	reg.MustRegister(c)
	return c
}

var (
	// AuditEvents mirrors every internal/audit.Logger.Log call, by action and
	// outcome — sign-ins, 2FA, exports and downloads are all audited actions,
	// so this one metric covers all four without a separate counter each.
	AuditEvents = counterVec("wms_audit_events_total", "Audit log entries, by action and outcome.", "action", "status")

	TelnetConnections = counter("wms_telnet_connections_total", "Telnet sessions accepted.")
	TelnetThrottled   = counter("wms_telnet_throttled_total", "Telnet connections refused by the rate limiter.")

	// NotifyDeliveries is by outcome ("success"/"failure"), not by provider —
	// providers are user-configured free text, not a bounded label set.
	NotifyDeliveries = counterVec("wms_notify_deliveries_total", "Notification delivery attempts, by outcome.", "outcome")
)

// Serve runs the /metrics endpoint on addr (0.0.0.0:<port> — see
// config.MetricsPort) until ctx is cancelled. It is its own listener,
// independent of the telnet/web gateway mux, so a scrape never shares a port
// with anything public-facing; it listens on every interface (not just
// loopback) only because the dockerised Prometheus needs to reach it via
// host.docker.internal, which cannot cross into host loopback — it is still
// never reverse-proxied or tunnelled out, unlike the telnet/web gateway.
func Serve(ctx context.Context, addr string) error {
	mux := http.NewServeMux()
	mux.Handle("/metrics", promhttp.HandlerFor(reg, promhttp.HandlerOpts{}))
	srv := &http.Server{Addr: addr, Handler: mux}
	go func() {
		<-ctx.Done()
		_ = srv.Close()
	}()
	if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
