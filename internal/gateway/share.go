package gateway

import (
	"net/http"
	"os"
	"path/filepath"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

// shareHandler serves a multi-use share link (see internal/exports.NewShareLink)
// inline, in a browser — a wishlist or collection page meant to be looked at
// as many times as it's opened until it expires, not downloaded once. Same
// trust model as downloadHandler: no login, gated only by the token and the
// link owner's own exports.download permission, and any failure is a bare
// 404 so the endpoint says nothing about which tokens or files exist.
func shareHandler(dir func() string, log func(user, status, details string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet && r.Method != http.MethodHead {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		token := r.URL.Path[len("/share/"):] // the route is /share/ or /SHARE/ (QR codes are upper case)
		file, user, err := exports.Open(dir(), token)
		if err != nil {
			log("-", "DENIED", "unknown or expired share link from "+r.RemoteAddr)
			http.NotFound(w, r)
			return
		}
		if p, err := access.Load(); err != nil || !p.MayDownload(user) {
			log(user, "DENIED", "no exports.download permission, from "+r.RemoteAddr)
			http.NotFound(w, r)
			return
		}
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
		w.Header().Set("Content-Disposition", `inline; filename="`+name+`"`)
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Security-Policy", "sandbox")
		log(user, "SUCCESS", name+" viewed from "+r.RemoteAddr)
		http.ServeContent(w, r, name, fi.ModTime(), f)
	}
}

func auditShare(user, status, details string) {
	_ = audit.New().Log(user, "", "SHARE_VIEW", status, details)
}
