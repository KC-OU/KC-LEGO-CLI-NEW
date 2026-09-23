package main

import (
	"context"
	"fmt"
	"net"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/backup"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

const (
	stOK   = "ok"
	stWarn = "warn"
	stFail = "fail"
)

type check struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

func ok(name, detail string) check { return check{Name: name, Status: stOK, Detail: detail} }
func warn(name, detail, hint string) check {
	return check{Name: name, Status: stWarn, Detail: detail, Hint: hint}
}
func fail(name, detail, hint string) check {
	return check{Name: name, Status: stFail, Detail: detail, Hint: hint}
}
func hasFail(cs []check) bool {
	for _, c := range cs {
		if c.Status == stFail {
			return true
		}
	}
	return false
}

func newDoctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check the install: services, tokens, keys, clock, file permissions, audit chain and backups",
		Long: "Runs every check and reports OK / WARN / FAIL with what to do about each. Exits 0 when nothing failed\n" +
			"(warnings are fine), 1 when something did — so it can run from cron or a monitor. A backup older than\n" +
			"a week counts as a failure.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
			defer cancel()
			checks := runDoctor(ctx)
			for _, c := range checks {
				line := c.Name + ": " + c.Detail
				switch c.Status {
				case stOK:
					say(ui.Status(t, true, line))
				case stWarn:
					say(ui.Warn(t, line))
				default:
					say(ui.Status(t, false, line))
				}
				if c.Hint != "" && c.Status != stOK {
					say("    " + t.Muted.Render("→ "+c.Hint))
				}
			}
			bad := hasFail(checks)
			if err := emit(map[string]any{"ok": !bad, "checks": checks}); err != nil {
				return err
			}
			if bad {
				return withCode(exitFailure, fmt.Errorf("doctor found problems"))
			}
			return nil
		},
	}
}

func runDoctor(ctx context.Context) []check {
	cs := []check{
		checkModernWMS(ctx),
		checkPartDBFile(),
		checkPartDBToken(ctx),
		checkRebrickable(ctx),
		checkCatalog(),
		checkClock(),
		checkGateway(),
	}
	cs = append(cs, checkSecretFiles()...)
	cs = append(cs, checkAudit(), checkBackups(config.Get(config.ModernWMSBackupDir), time.Now()), checkLegoBackup(time.Now()), checkPlugins(), checkTmux())
	return cs
}

// checkTmux is a warning, not a failure: without tmux the web terminal falls back to
// today's one-process-per-connection behaviour (see internal/gateway/web.go
// webCommand) — asks for 2FA on every reconnect instead of sharing a persistent
// session, but still works.
func checkTmux() check {
	if _, err := exec.LookPath("tmux"); err != nil {
		return warn("Web terminal session", "tmux is not installed", "install tmux so the web terminal keeps you signed in across reconnects; without it, every reconnect asks for 2FA again")
	}
	return ok("Web terminal session", "tmux found — the web terminal persists across reconnects")
}

func checkModernWMS(ctx context.Context) check {
	c := wmsdb.NewClient()
	c.Timeout = 15 * time.Second
	if _, err := c.RunScript(ctx, "print('ok')"); err != nil {
		return fail("ModernWMS", "cannot reach the container: "+firstLine(err.Error()), "is the '"+c.Container+"' container running? (docker ps)")
	}
	return ok("ModernWMS", "container '"+c.Container+"' answers")
}

func checkPartDBFile() check {
	db, err := partdb.Open("")
	if err != nil {
		return fail("Part-DB database", err.Error(), "check PARTDB_DB_PATH and that the file is readable")
	}
	defer db.Close()
	return ok("Part-DB database", "readable ("+config.Get(config.PartDBDBPath)+")")
}

func checkPartDBToken(ctx context.Context) check {
	if config.Get(config.PartDBAPIToken) == "" {
		return warn("Part-DB API token", "not set — new parts are saved in the LEGO collection but not sent to Part-DB",
			"create an Edit-level token in Part-DB, then Admin > Settings & API Keys > Part-DB API Token")
	}
	w := partdb.NewWriter(nil)
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := w.API().Ping(pctx); err != nil {
		return fail("Part-DB API token", err.Error(), "check PARTDB_API_URL, and the token's scope and expiry")
	}
	return ok("Part-DB API token", "accepted by "+config.Get(config.PartDBAPIURL))
}

func checkRebrickable(ctx context.Context) check {
	if config.Get(config.RebrickableAPIKey) == "" {
		return warn("Rebrickable key", "not set — lookups use the offline catalog only",
			"add one under Admin > Settings & API Keys for live colour lists and parts the catalog lacks")
	}
	db, err := openLego()
	if err != nil {
		return fail("Rebrickable key", "cannot open the LEGO store: "+err.Error(), "")
	}
	defer db.Close()
	pctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	// One cached lookup (30 days), so this costs at most a request a month.
	if _, err := lego.NewClientFor(db).GetPart(pctx, "3001"); err != nil {
		return fail("Rebrickable key", err.Error(), "check the key, or wait if Rebrickable is rate-limiting")
	}
	return ok("Rebrickable key", "accepted")
}

func checkCatalog() check {
	db, err := openLego()
	if err != nil {
		return fail("Offline catalog", err.Error(), "")
	}
	defer db.Close()
	parts, _, when, err := db.CatalogStatus()
	if err != nil {
		return fail("Offline catalog", err.Error(), "")
	}
	var empty []string
	for _, tb := range db.CatalogTables() {
		if tb.Rows == 0 {
			empty = append(empty, tb.File)
		}
	}
	switch {
	case parts == 0 || when.IsZero():
		return warn("Offline catalog", "empty — search and adding need the API until it is loaded", "run: wms lego catalog refresh")
	case len(empty) > 0:
		return warn("Offline catalog", "loaded, but these files are empty: "+strings.Join(empty, ", "), "run: wms lego catalog refresh --force")
	case time.Since(when) > 90*24*time.Hour:
		return warn("Offline catalog", fmt.Sprintf("%d parts, %d days old", parts, int(time.Since(when).Hours()/24)), "run: wms lego catalog refresh")
	}
	return ok("Offline catalog", fmt.Sprintf("%d parts, %d sets, refreshed %s", parts, db.SetsInCatalog(), when.Format("2006-01-02")))
}

// checkClock matters because TOTP codes are only valid for their 30 s step:
// a clock that has drifted rejects every correct code.
func checkClock() check {
	b, err := exec.Command("timedatectl", "show", "-p", "NTPSynchronized", "--value").Output()
	if err != nil {
		return warn("Clock", "cannot tell whether it is synchronised (no timedatectl)", "2FA codes need the clock within about 30 seconds")
	}
	if strings.TrimSpace(string(b)) != "yes" {
		return fail("Clock", "not synchronised with a time server", "2FA codes will be rejected as the clock drifts: enable NTP (timedatectl set-ntp true)")
	}
	return ok("Clock", "synchronised (2FA codes depend on it)")
}

func checkGateway() check {
	active := exec.Command("systemctl", "is-active", "--quiet", "wms-gateway.service").Run() == nil
	var listening []string
	for _, p := range strings.Split(config.Get(config.ListenPorts)+","+config.Get(config.GatewayPort), ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if c, err := net.DialTimeout("tcp", "127.0.0.1:"+p, time.Second); err == nil {
			c.Close()
			listening = append(listening, p)
		}
	}
	switch {
	case !active && len(listening) == 0:
		return fail("Gateway", "wms-gateway.service is not running", "systemctl start wms-gateway.service")
	case len(listening) == 0:
		return fail("Gateway", "service is active but nothing is listening", "journalctl -u wms-gateway.service")
	}
	return ok("Gateway", "listening on "+strings.Join(listening, ", "))
}

// checkSecretFiles flags files holding secrets that anyone but the owner can read.
func checkSecretFiles() []check {
	files := map[string]string{
		"Settings file (API keys)": config.Get(config.SettingsFile),
		"2FA store":                config.Get(config.TwoFAFile),
		"Audit log file":           config.Get(config.AuditLogFile),
	}
	var cs []check
	for _, name := range []string{"Settings file (API keys)", "2FA store", "Audit log file"} {
		cs = append(cs, checkPerms(name, files[name]))
	}
	return cs
}

func checkPerms(name, path string) check {
	fi, err := os.Stat(path)
	if os.IsNotExist(err) {
		return ok(name, "not created yet ("+path+")")
	}
	if err != nil {
		return fail(name, err.Error(), "")
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return fail(name, fmt.Sprintf("%s is readable by other users (mode %04o)", path, fi.Mode().Perm()), "chmod 600 "+path)
	}
	return ok(name, fmt.Sprintf("private (mode %04o)", fi.Mode().Perm()))
}

func checkAudit() check {
	l := audit.New()
	r, err := l.Verify()
	switch {
	case err != nil:
		return fail("Audit log", err.Error(), "")
	case !r.OK():
		return fail("Audit log", fmt.Sprintf("hash chain broken at line %d: %s", r.BrokenAt, r.BrokenWhy), "someone edited the log; run: wms audit verify")
	case r.Genesis == 0:
		return warn("Audit log", "no hash chain yet", "it starts with the next audited action")
	}
	return ok("Audit log", fmt.Sprintf("chain intact (%d chained lines)", r.Chained))
}

// checkBackups fails when there is no backup at all or the newest is over a
// week old: a backup job that quietly stopped is the failure worth catching.
func checkBackups(dir string, now time.Time) check {
	newestName, newest, found := backup.Newest(dir)
	hint := "run: wms backup   (schedule it with cron or a systemd timer)"
	switch {
	case !found:
		return fail("Backups", "none found in "+dir, hint)
	case now.Sub(newest) > backup.StaleAfter:
		return fail("Backups", fmt.Sprintf("the newest is %d days old (%s)", int(now.Sub(newest).Hours()/24), newestName), hint)
	case now.Sub(newest) > 26*time.Hour:
		return warn("Backups", fmt.Sprintf("the newest is %d days old (%s)", int(now.Sub(newest).Hours()/24), newestName), hint)
	}
	return ok("Backups", "newest is "+newestName)
}

// checkLegoBackup warns when a collection that holds something has no recent backup: the LEGO
// data is only in lego.db (plus what was synced to Part-DB), so a lost file is a lost collection.
func checkLegoBackup(now time.Time) check {
	db, err := openLego()
	if err != nil {
		return warn("LEGO backup", "could not open the collection: "+err.Error(), "")
	}
	st, err := db.Stats()
	db.Close()
	if err != nil || st.SetTitles+st.PartLines == 0 {
		return ok("LEGO backup", "nothing in the collection yet")
	}
	hint := "run: wms lego backup [--encrypt]   (schedule it with cron or a systemd timer)"
	newestName, newest := newestLegoBackup()
	switch {
	case newest.IsZero():
		return warn("LEGO backup", "your collection has no backup yet", hint)
	case now.Sub(newest) > 30*24*time.Hour:
		return warn("LEGO backup", fmt.Sprintf("the newest is %d days old (%s)", int(now.Sub(newest).Hours()/24), newestName), hint)
	}
	return ok("LEGO backup", "newest is "+newestName)
}

func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

func checkPlugins() check {
	m := pluginManager()
	infos, err := m.List()
	if err != nil {
		if os.IsNotExist(err) {
			return ok("Plugins", "none (no plugin folder)")
		}
		return fail("Plugins", err.Error(), "the plugin folder must be owned by root and not writable by others: chmod go-w "+m.Dir)
	}
	var enabled int
	var problems []string
	for _, i := range infos {
		if i.Entry.Enabled {
			enabled++
		}
		switch {
		case i.Problem != "" && (i.Entry.Enabled || i.Known):
			problems = append(problems, i.Name+": "+i.Problem)
		case i.Modified:
			problems = append(problems, i.Name+": changed since it was enabled")
		}
	}
	if len(problems) > 0 {
		return fail("Plugins", strings.Join(problems, "; "), "review the file, then: wms plugin enable <name>")
	}
	return ok("Plugins", fmt.Sprintf("%d in the folder, %d enabled, all unchanged", len(infos), enabled))
}
