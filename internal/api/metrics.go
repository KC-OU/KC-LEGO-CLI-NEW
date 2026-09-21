package api

import (
	"fmt"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/sync"
)

type metricSpec struct {
	name, help, kind string
	value            float64
}

func renderMetrics(stats sync.Stats) string {
	status := 1.0
	var last sync.SyncResult
	if stats.LastResult != nil {
		last = *stats.LastResult
	}
	if stats.LastResult == nil || stats.LastError != "" {
		status = 0
	}

	specs := []metricSpec{
		{"partdb_sync_status", "1 if the last sync succeeded, 0 otherwise", "gauge", status},
		{"partdb_synced_categories", "Categories synced in the last run", "gauge", float64(last.SyncedCategories)},
		{"partdb_synced_parts", "Parts synced in the last run", "gauge", float64(last.SyncedParts)},
		{"partdb_synced_locations", "Locations synced in the last run", "gauge", float64(last.SyncedLocations)},
		{"partdb_synced_stock_records", "Stock records synced in the last run", "gauge", float64(last.SyncedStockRecords)},
		{"partdb_total_stock_qty", "Total stock quantity across all synced parts", "gauge", float64(last.TotalStockQty)},
		{"partdb_sync_duration_seconds", "Duration of the last sync run", "gauge", last.DurationMS / 1000.0},
		{"partdb_sync_runs_total", "Total number of sync runs", "counter", float64(stats.TotalSyncCount)},
		{"partdb_sync_errors_total", "Total number of failed sync runs", "counter", float64(stats.TotalErrorCount)},
		{"partdb_api_requests_total", "Total number of API GET requests served", "counter", float64(stats.APIRequestsTotal)},
		{"partdb_sync_uptime_seconds", "Seconds since the sync service started", "gauge", time.Since(stats.StartedAt).Seconds()},
	}

	var b strings.Builder
	for _, m := range specs {
		fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s %s\n%s %g\n", m.name, m.help, m.name, m.kind, m.name, m.value)
	}
	return b.String()
}
