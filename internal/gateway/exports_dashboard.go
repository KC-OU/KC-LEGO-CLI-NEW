package gateway

import (
	"html/template"
	"net/http"
	"strconv"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
)

// exportsHandler is a browsable alternative to a one-off /dl/ link: sign in
// with the same account the TUI itself checks (HTTP Basic, since this is a
// handful of plain requests, not a session worth a cookie store), and every
// export still inside MaxAge() gets a freshly minted single-use download
// link — so losing or reusing the one link a TUI session showed once no
// longer means redoing the export. Same exports.download permission
// downloadHandler itself already requires; same ListExports/NewLink pair
// internal/uiapp's own export screen uses, just reachable without a
// terminal (see ListExports' own doc comment).
func exportsHandler(dir func() string, authWMS auth.WMSAuthenticator, authPDB auth.PartDBAuthenticator, log func(user, status, details string)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		username, password, ok := r.BasicAuth()
		if !ok {
			w.Header().Set("WWW-Authenticate", `Basic realm="wms exports"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		session, err := auth.AuthenticateUser(r.Context(), authWMS, authPDB, username, password)
		if err != nil {
			log(username, "DENIED", "bad credentials from "+r.RemoteAddr)
			w.Header().Set("WWW-Authenticate", `Basic realm="wms exports"`)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		key := access.Key(session.Source, session.Username)
		p, err := access.Load()
		if err != nil || !p.MayDownload(key) {
			log(session.Username, "DENIED", "no exports.download permission, from "+r.RemoteAddr)
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}

		d := dir()
		exports.Cleanup(d) // opportunistic, same as internal/uiapp's own export screen
		files, err := exports.ListExports(d, session.Username)
		if err != nil {
			http.Error(w, "exports folder unavailable", http.StatusInternalServerError)
			return
		}

		type row struct {
			Name, Size, URL string
			When            string
		}
		rows := make([]row, 0, len(files))
		for _, f := range files {
			tok, err := exports.NewLink(d, f.Path, key)
			if err != nil {
				continue // skip a file this one link couldn't be minted for; the rest still render
			}
			rows = append(rows, row{
				Name: exports.Describe(f.Path),
				URL:  "/dl/" + tok,
				When: f.ModTime.Format("2006-01-02 15:04"),
			})
		}

		log(session.Username, "SUCCESS", strconv.Itoa(len(rows))+" file(s) listed from "+r.RemoteAddr)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Cache-Control", "no-store")
		_ = exportsPage.Execute(w, rows)
	}
}

func auditExports(user, status, details string) {
	_ = audit.New().Log(user, "", "EXPORTS_LIST", status, details)
}

var exportsPage = template.Must(template.New("exports").Parse(`<!DOCTYPE html>
<html><head><meta charset="utf-8"><meta name="viewport" content="width=device-width, initial-scale=1">
<title>My exports</title>
<style>
body{font-family:system-ui,sans-serif;background:#111;color:#eee;margin:0;padding:1.5rem}
h1{font-size:1.1rem;color:#9cf}
table{width:100%;border-collapse:collapse;margin-top:1rem}
td,th{text-align:left;padding:.5rem;border-bottom:1px solid #333}
a{color:#6cf;text-decoration:none}
a:hover{text-decoration:underline}
.empty{color:#888;margin-top:1rem}
</style></head><body>
<h1>My exports</h1>
{{if .}}
<table><tr><th>File</th><th>Saved</th><th></th></tr>
{{range .}}<tr><td>{{.Name}}</td><td>{{.When}}</td><td><a href="{{.URL}}">download</a></td></tr>
{{end}}</table>
{{else}}
<p class="empty">Nothing here yet — exports you make from the TUI show up here for as long as they're kept.</p>
{{end}}
</body></html>`))
