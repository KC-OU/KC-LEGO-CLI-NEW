// Command wms is the single entrypoint for the ModernWMS/Part-DB suite —
// one Cobra binary with subcommands instead of the original Python suite's
// five separately-symlinked scripts.
package main

import (
	"os"
	"time"

	"github.com/getsentry/sentry-go"
	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/termguard" // must be imported: its init runs before bubbletea's
)

func newRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "wms",
		Short: "ModernWMS & Part-DB control suite — an IBM i (5250) style terminal application",
		Long: "wms is a CLI-primary front end for two dockerized apps, ModernWMS and Part-DB:\n" +
			"a 5250-styled interactive terminal (`wms` / `wms tui`), dual-database user\n" +
			"management, ASN stock receiving, backups, the Part-DB -> ModernWMS sync engine\n" +
			"with its REST API and Prometheus metrics, and a Telnet + web-terminal gateway.",
		RunE: runTUI,
	}

	root.AddCommand(newTUICmd())
	root.AddCommand(newUsersCmd())
	root.AddCommand(newAccessCmd())
	root.AddCommand(newMOTDCmd())
	root.AddCommand(newPublishCmd())
	root.AddCommand(newPromoteCmd())
	root.AddCommand(newReceiveCmd())
	root.AddCommand(newBackupCmd())
	root.AddCommand(newSyncCmd())
	root.AddCommand(newGatewayCmd())
	root.AddCommand(newLegoCmd())
	root.AddCommand(newAuditCmd())
	root.AddCommand(newOnboardingCmd())
	root.AddCommand(newDoctorCmd())
	root.AddCommand(newNotifyCmd())
	root.AddCommand(newSalesCmd())
	root.AddCommand(newAcquireCmd())
	root.AddCommand(newPublicStockCmd())
	root.AddCommand(newPublicBlogCmd())
	root.AddCommand(newBlogCmd())
	root.AddCommand(newBricklinkCmd())
	root.AddCommand(newPluginCmd())
	root.AddCommand(newVersionCmd())
	root.AddCommand(newUpdateCmd())
	root.AddCommand(newPreflightCmd())
	root.AddCommand(newSysCmd())
	root.AddCommand(newAttendanceCmd())
	root.AddCommand(newMenuCmd())
	root.AddCommand(newDocsGenCmd(root))
	root.AddCommand(newManGenCmd(root))
	addOutputFlags(root)
	root.SetFlagErrorFunc(func(_ *cobra.Command, err error) error { return withCode(exitUsage, err) })
	applyFuzzyGroupArgs(root)
	return root
}

// execute runs one command line: a plugin if the first word names one (built-in commands
// always win), otherwise the normal command tree.
func execute(root *cobra.Command, args []string) error {
	args = expandAlias(args)
	if handled, err := dispatchPlugin(root, args); handled {
		return err
	}
	root.SetArgs(args)
	return root.Execute()
}

// initSentry reports a panic in this goroutine (a plain CLI invocation
// crashing, or the gateway server's own synchronous startup) to Sentry
// before the process exits -- empty DSN (the default, see
// config.SentryDSN) makes sentry.Init a documented no-op, so this is safe
// to call unconditionally rather than gating it on being configured.
// NOTE: this only covers the main goroutine. `wms gateway serve` is a
// long-running HTTP server whose per-request handlers run in their own
// goroutines, which this recover() cannot reach -- panics there need
// per-handler sentryhttp middleware, not added here (narrower, separate
// task if request-handler panics turn out to matter).
func initSentry() func() {
	_ = sentry.Init(sentry.ClientOptions{Dsn: config.Get(config.SentryDSN)})
	return func() { sentry.Flush(2 * time.Second) }
}

func main() {
	defer initSentry()()
	defer func() {
		// Re-panics after reporting -- flushing and exiting non-zero on an
		// unrecovered panic is already Go's own default behavior; this
		// only adds the report, not a change to that behavior.
		if r := recover(); r != nil {
			sentry.CurrentHub().Recover(r)
			panic(r)
		}
	}()

	termguard.Restore()
	root := newRootCmd()
	root.SilenceErrors = true // reportError prints it, plain or as JSON
	root.SilenceUsage = true  // a screen of help around every error would corrupt --json output
	if err := execute(root, os.Args[1:]); err != nil {
		os.Exit(reportError(err))
	}
}
