package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/backup"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// sysStatus is a quick picture of the machine, for `wms sys status`, the login banner and the launcher's
// status line. Every part has a short timeout, so a stuck docker daemon cannot hang a login.
type sysStatus struct {
	Containers    []containerState `json:"containers"`
	ContainersOK  bool             `json:"containers_known"`
	Up, Total     int              `json:"-"`
	Gateway       string           `json:"gateway"`
	BackupAge     time.Duration    `json:"backup_age_ns"` // -1 when there is none
	LegoBackupAge time.Duration    `json:"lego_backup_age_ns"`
	LowStock      int              `json:"low_stock"`
	MemUsed       string           `json:"memory,omitempty"`
	DiskUsed      string           `json:"disk,omitempty"`
	Uptime        string           `json:"uptime,omitempty"`
}

type containerState struct {
	Name   string `json:"name"`
	State  string `json:"state"`
	Status string `json:"status"`
}

func collectStatus(ctx context.Context, full bool) sysStatus {
	st := sysStatus{BackupAge: -1, LegoBackupAge: -1}
	var wg sync.WaitGroup
	run := func(f func()) {
		wg.Add(1)
		go func() { defer wg.Done(); f() }()
	}
	run(func() {
		c, cancel := context.WithTimeout(ctx, 1500*time.Millisecond)
		defer cancel()
		out, err := exec.CommandContext(c, "docker", "ps", "-a", "--format", "{{.Names}}|{{.State}}|{{.Status}}").Output()
		if err != nil {
			return
		}
		st.ContainersOK = true
		for _, l := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if p := strings.SplitN(l, "|", 3); len(p) == 3 {
				st.Containers = append(st.Containers, containerState{p[0], p[1], p[2]})
				st.Total++
				if p[1] == "running" {
					st.Up++
				}
			}
		}
	})
	run(func() {
		c, cancel := context.WithTimeout(ctx, time.Second)
		defer cancel()
		out, _ := exec.CommandContext(c, "systemctl", "is-active", "wms-gateway.service").Output()
		st.Gateway = strings.TrimSpace(string(out))
		if st.Gateway == "" {
			st.Gateway = "unknown"
		}
	})
	run(func() {
		if _, mod, ok := backup.Newest(config.Get(config.ModernWMSBackupDir)); ok {
			st.BackupAge = time.Since(mod)
		}
		if _, mod := newestLegoBackup(); !mod.IsZero() {
			st.LegoBackupAge = time.Since(mod)
		}
	})
	run(func() {
		if db, err := openLego(); err == nil {
			if low, err := db.LowStock(); err == nil {
				st.LowStock = len(low)
			}
			db.Close()
		}
	})
	if full {
		st.MemUsed, st.DiskUsed, st.Uptime = memDiskUptime()
	}
	wg.Wait()
	return st
}

func newestLegoBackup() (string, time.Time) {
	var newest time.Time
	var name string
	matches, _ := filepath.Glob(filepath.Join(config.Get(config.ModernWMSBackupDir), "lego", "lego_db_*"))
	for _, m := range matches {
		if strings.HasSuffix(m, ".verify") {
			continue
		}
		if fi, err := os.Stat(m); err == nil && fi.ModTime().After(newest) {
			newest, name = fi.ModTime(), filepath.Base(m)
		}
	}
	return name, newest
}

func memDiskUptime() (mem, disk, up string) {
	if b, err := os.ReadFile("/proc/meminfo"); err == nil {
		var total, avail int64
		for _, l := range strings.Split(string(b), "\n") {
			var k string
			var v int64
			if n, _ := fmt.Sscanf(l, "%s %d", &k, &v); n == 2 {
				switch k {
				case "MemTotal:":
					total = v
				case "MemAvailable:":
					avail = v
				}
			}
		}
		if total > 0 {
			mem = fmt.Sprintf("%s / %s", gib(total-avail), gib(total))
		}
	}
	var st syscall.Statfs_t
	if syscall.Statfs("/", &st) == nil {
		bs := float64(st.Bsize)
		total, free := float64(st.Blocks)*bs, float64(st.Bavail)*bs
		disk = fmt.Sprintf("%s / %s (%.0f%% used)", gib(int64((total-free)/1024)), gib(int64(total/1024)), (total-free)*100/max(total, 1))
	}
	if b, err := os.ReadFile("/proc/uptime"); err == nil {
		var secs float64
		if _, err := fmt.Sscanf(string(b), "%f", &secs); err == nil {
			d := time.Duration(secs) * time.Second
			up = fmt.Sprintf("%dd %dh", int(d.Hours())/24, int(d.Hours())%24)
		}
	}
	return
}

func gib(kib int64) string { return fmt.Sprintf("%.1f GB", float64(kib)/1024/1024) }

func ageText(d time.Duration) string {
	switch {
	case d < 0:
		return "none"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 48*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	}
	return fmt.Sprintf("%dd", int(d.Hours())/24)
}

// bannerLine is the one line printed at login. Colours: green fine, yellow to look at, red down.
func bannerLine(st sysStatus, t ui.Theme) string {
	g := func(ok bool, s string) string {
		if ok {
			return t.Success.Render(s)
		}
		return t.Danger.Render(s)
	}
	warn := func(s string) string { return t.Warning.Render(s) }
	parts := []string{t.Brand.Render("[WMS+PartDB]")}
	if st.ContainersOK {
		parts = append(parts, g(st.Up == st.Total && st.Total > 0, fmt.Sprintf("%d/%d up", st.Up, st.Total)))
	} else {
		parts = append(parts, warn("docker ?"))
	}
	parts = append(parts, g(st.Gateway == "active", "gateway "+map[bool]string{true: "up", false: st.Gateway}[st.Gateway == "active"]))
	backupTxt := "backup " + ageText(st.BackupAge)
	if st.BackupAge < 0 || st.BackupAge > 8*24*time.Hour {
		parts = append(parts, warn(backupTxt))
	} else {
		parts = append(parts, t.Muted.Render(backupTxt))
	}
	if st.LowStock > 0 {
		parts = append(parts, warn(fmt.Sprintf("%d low", st.LowStock)))
	}
	return strings.Join(parts, t.Muted.Render(" · ")) + t.Muted.Render("   q launcher · guide")
}

func newSysCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "sys", Short: "Small system helpers used by the launcher and the login banner"}

	cmd.AddCommand(&cobra.Command{
		Use:   "banner",
		Short: "The one-line status shown at login",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			st := collectStatus(ctx, false)
			say(bannerLine(st, ui.New()))
			return emit(st)
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "status",
		Short: "System health: memory, disk, containers, gateway, backups",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			st := collectStatus(ctx, true)
			say(ui.Fact(t, "Memory", st.MemUsed))
			say(ui.Fact(t, "Disk /", st.DiskUsed))
			say(ui.Fact(t, "Uptime", st.Uptime))
			say(ui.Fact(t, "Gateway", st.Gateway))
			say(ui.Fact(t, "Backups", fmt.Sprintf("ModernWMS %s, LEGO %s", ageText(st.BackupAge), ageText(st.LegoBackupAge))))
			say(ui.Fact(t, "Low stock", fmt.Sprintf("%d part(s)", st.LowStock)))
			if !st.ContainersOK {
				say(ui.Warn(t, "docker did not answer"))
			}
			for _, c := range st.Containers {
				line := fmt.Sprintf("%-14s %s", c.Name, c.Status)
				say(ui.Status(t, c.State == "running", line))
			}
			return emit(st)
		},
	})

	var lines int
	var follow bool
	audit := &cobra.Command{
		Use:   "audit",
		Short: "Show the newest audit log entries (-f to follow)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			path := config.Get(config.AuditLogFile)
			if follow {
				c := exec.Command("tail", "-n", fmt.Sprint(lines), "-f", path)
				c.Stdout, c.Stderr = os.Stdout, os.Stderr
				return c.Run()
			}
			tail, err := newAuditLogger().Tail(lines)
			if err != nil {
				return err
			}
			if len(tail) == 0 {
				say("The audit log is empty.")
			}
			for _, l := range tail {
				say(l)
			}
			return emit(map[string]any{"entries": tail})
		},
	}
	audit.Flags().IntVarP(&lines, "lines", "n", 25, "how many entries")
	audit.Flags().BoolVarP(&follow, "follow", "f", false, "keep printing new entries")
	cmd.AddCommand(audit)

	cmd.AddCommand(&cobra.Command{
		Use:   "connect",
		Short: "How to connect from another machine (telnet or a browser)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			ip := "<this-server>"
			if out, err := exec.Command("sh", "-c", "ip route get 1.1.1.1 2>/dev/null | awk '{print $7; exit}'").Output(); err == nil && strings.TrimSpace(string(out)) != "" {
				ip = strings.TrimSpace(string(out))
			}
			ports := strings.Split(config.Get(config.ListenPorts), ",")
			say("Browser  " + t.Warning.Render(fmt.Sprintf("http://%s:%s", ip, config.Get(config.GatewayPort))))
			say("Telnet   " + t.Warning.Render(fmt.Sprintf("telnet %s %s", ip, strings.TrimSpace(ports[0]))))
			say(t.Muted.Render("Same sign-on and screens either way; the window size is followed (a client that cannot report one gets 80x24)."))
			say(t.Muted.Render("2FA is required over telnet and web: wms users 2fa enable <user>."))
			say(t.Muted.Render("Telnet is plain text: keep it on a trusted network or VPN."))
			return nil
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "guide",
		Short: "A short card of the commands worth remembering",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			row := func(k, v string) { say("  " + t.Warning.Render(fmt.Sprintf("%-16s", k)) + v) }
			say(t.Brand.Render(" Quick guide"))
			row("q", "the launcher: type to filter, Enter runs, * pins a favourite")
			row("wms  /  tui", "the full-screen TUI (telnet and web show the same screens)")
			row("wms doctor", "check the whole install")
			row("wms preflight", "is it safe to ship? GO or NO-GO")
			row("wms lego ...", "search, add-part, stats, value, watch, build, export")
			row("wms users ...", "list, create, reset, set-password, 2fa")
			row("wms backup", "ModernWMS backup   (wms lego backup: the collection)")
			row("dps  /  ll", "containers  /  long listing")
			row("cdw cdp cdb", "go to docker-server, partdb, backups")
			row("wms sys connect", "how to connect from another machine")
			row("F1 in the TUI", "key help; Ctrl-K or F2 is the command palette")
			row("guide", "this card")
			return nil
		},
	})
	return cmd
}

func newAuditLogger() *audit.Logger { return audit.New() }
