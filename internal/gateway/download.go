package gateway

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

// downloadHandler serves an export once, by the single-use token a TUI session
// showed as a link and QR code (see internal/exports). Any failure is a bare 404,
// so the endpoint says nothing about which tokens or files exist.
func downloadHandler(dir func() string, log func(user, status, details string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := r.URL.Path[len("/dl/"):] // the route is /dl/ or /DL/ (QR codes are upper case)
		file, user, err := exports.Claim(dir(), token)
		if err != nil {
			log("-", "DENIED", "unknown or used download link from "+r.RemoteAddr)
			http.NotFound(w, r)
			return
		}
		// The link's owner may have lost the permission since making it.
		if p, err := access.Load(); err != nil || !p.MayDownload(user) {
			log(user, "DENIED", "no exports.download permission, from "+r.RemoteAddr)
			http.NotFound(w, r)
			return
		}
		// Open inside the export folder only: os.OpenInRoot refuses any path (or
		// symlink) that would leave it, whatever the link record said.
		f, err := os.OpenInRoot(dir(), filepath.Base(file))
		if err != nil {
			http.NotFound(w, r)
			return
		}
		defer f.Close()
		fi, err := f.Stat()
		if err != nil {
			http.NotFound(w, r)
			return
		}
		name := filepath.Base(file)
		w.Header().Set("Content-Disposition", `attachment; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox")
		log(user, "SUCCESS", name+" to "+r.RemoteAddr)
		http.ServeContent(w, r, name, fi.ModTime(), f)
	}
}

func auditDownload(user, status, details string) {
	_ = audit.New().Log(user, "", "EXPORT_DOWNLOAD", status, details)
}
