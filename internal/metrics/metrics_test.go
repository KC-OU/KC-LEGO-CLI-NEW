package metrics

import (
	"context"
	"io"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestServeExposesIncrementedCounters(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, addr) }()

	TelnetConnections.Inc()
	AuditEvents.WithLabelValues("LOGIN_MODERNWMS", "SUCCESS").Inc()

	var body string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		resp, err := http.Get("http://" + addr + "/metrics")
		if err == nil {
			b, _ := io.ReadAll(resp.Body)
			resp.Body.Close()
			body = string(b)
			break
		}
		time.Sleep(20 * time.Millisecond)
	}
	if !strings.Contains(body, "wms_telnet_connections_total 1") {
		t.Errorf("missing telnet connections counter:\n%s", body)
	}
	if !strings.Contains(body, `wms_audit_events_total{action="LOGIN_MODERNWMS",status="SUCCESS"} 1`) {
		t.Errorf("missing audit events counter:\n%s", body)
	}

	cancel()
	select {
	case err := <-errc:
		if err != nil {
			t.Errorf("Serve: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Serve did not stop after ctx cancel")
	}
}
