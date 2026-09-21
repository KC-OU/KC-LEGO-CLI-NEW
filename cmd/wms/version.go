package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/update"
)

// Set at build time: -ldflags "-X main.version=1.2.3 -X main.commit=abc -X main.date=2026-09-20".
var (
	version = "dev"
	commit  = ""
	date    = ""
)

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			say(ui.Fact(t, "wms", version))
			if commit != "" {
				say(ui.Fact(t, "Commit", commit))
			}
			if date != "" {
				say(ui.Fact(t, "Built", date))
			}
			say(ui.Fact(t, "Platform", runtime.GOOS+"/"+runtime.GOARCH+", "+runtime.Version()))
			if out.Quiet {
				fmt.Println(version) // --quiet still answers the one thing asked
			}
			return emit(map[string]any{"version": version, "commit": commit, "built": date, "os": runtime.GOOS, "arch": runtime.GOARCH, "go": runtime.Version()})
		},
	}
}

// updateTarget is the binary `wms update --apply` replaces; empty means the running one (tests set it).
var updateTarget string

func updateClient() *update.Client {
	c := update.NewClient()
	if base := os.Getenv("WMS_UPDATE_URL"); base != "" { // a mirror, or a test server
		c.API = base
	}
	return c
}

func newUpdateCmd() *cobra.Command {
	var apply, yes, force bool
	cmd := &cobra.Command{
		Use:   "update [--apply]",
		Short: "Check GitHub for a newer release, and install it (after verifying its SHA-256)",
		Long: "By default this only CHECKS. --apply downloads the release for this platform from GitHub, verifies it against the\n" +
			"release's checksums.txt, keeps the current binary as <name>.old, and swaps the new one in. It never restarts anything:\n" +
			"restart the gateway yourself (systemctl restart wms-gateway.service; sessions in progress are dropped).\n" +
			"It refuses a package-managed install (use apt/dnf/pacman) and a version that is not newer, unless --force.\n" +
			"Optional extra check of where a release came from: gh attestation verify <file> --repo KC-OU/KC-LEGO-CLI-NEW",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			c := updateClient()
			rel, err := c.Latest(ctx)
			if errors.Is(err, update.ErrNoReleases) {
				say(ui.Warn(t, "No release has been published yet."))
				return emit(map[string]any{"current": version, "latest": nil})
			}
			if err != nil {
				return withCode(exitNetwork, err)
			}
			cmp, comparable := update.Compare(rel.Tag, version)
			newer := !comparable || cmp > 0
			say(ui.Fact(t, "Installed", version))
			say(ui.Fact(t, "Latest", rel.Tag))
			switch {
			case comparable && cmp <= 0:
				say(ui.Status(t, true, "You are up to date."))
			case !apply:
				say(ui.Warn(t, "A newer release is available. Install it with: wms update --apply"))
			}
			if !apply || (!newer && !force) {
				return emit(map[string]any{"current": version, "latest": rel.Tag, "update_available": newer})
			}
			if err := confirm(fmt.Sprintf("Install %s over %s?", rel.Tag, version), yes); err != nil {
				return err
			}
			res, err := c.Apply(ctx, rel, version, update.Options{Force: force, Exe: updateTarget})
			if err != nil {
				return withCode(exitFailure, err)
			}
			say(ui.Status(t, true, fmt.Sprintf("Installed %s (checksum verified: %s…).", res.Version, res.SHA256[:16])))
			say(ui.Fact(t, "Previous binary", res.OldPath))
			say(ui.Warn(t, "Not restarted. To use it now: systemctl restart wms-gateway.service"))
			return emit(map[string]any{"installed": res.Version, "path": res.Path, "previous": res.OldPath, "sha256": res.SHA256})
		},
	}
	cmd.Flags().BoolVar(&apply, "apply", false, "download and install the newer release")
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask for confirmation")
	cmd.Flags().BoolVar(&force, "force", false, "allow a package-managed path, or reinstalling/downgrading")
	return cmd
}
