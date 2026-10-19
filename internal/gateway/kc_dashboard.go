package gateway

import (
	_ "embed"
	"net/http"
)

//go:embed kc_dashboard.html
var kcDashboardHTML []byte

//go:embed kc_onboarding.html
var kcOnboardingHTML []byte

// serveKCDashboard wires up the KC-Parts web dashboard and onboarding pages:
// a read-only, sample-data prototype the business can use and share today,
// ahead of wiring it to real sync/ticket data and the CLI's own login system.
func serveKCDashboard(mux *http.ServeMux) {
	mux.HandleFunc("/dashboard", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(kcDashboardHTML)
	})
	mux.HandleFunc("/onboarding", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(kcOnboardingHTML)
	})
}
