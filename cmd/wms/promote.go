package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/spf13/cobra"
)

// newPromoteCmd is `wms promote`: scripts/promote-dev.sh (dev -> main -> live Go service, then the dashboard) as a
// command, so it runs from any folder. The script owns every safeguard; this only finds it and hands over the terminal.
func newPromoteCmd() *cobra.Command {
	var yes, setPin bool
	cmd := &cobra.Command{
		Use:   "promote",
		Short: "Ship dev to live: the Go service, then the dashboard (runs scripts/promote-dev.sh)",
		Long: "Lists what is on dev but not on main, runs the preflight, and after you type CONFIRM merges it into main and\n" +
			"deploys it, then does the same for the dashboard. Run it inside the repository.\n" +
			"With -y the typed CONFIRM is replaced by your promote PIN (sudo-style); choose the PIN once with --set-pin.",
		Example: "  wms promote\n  wms promote -y\n  wms promote --set-pin",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			cwd, _ := os.Getwd()
			dir, err := gitTop(cwd)
			if err != nil {
				return usageError("run this inside the wms-go repository")
			}
			// Run by relative path from the repository's top folder (no user-supplied path reaches exec).
			argv := []string{filepath.Join("scripts", "promote-dev.sh")}
			switch {
			case setPin:
				argv = append(argv, "--set-pin")
			case yes:
				argv = append(argv, "-y")
			}
			c := exec.CommandContext(cmd.Context(), "bash", argv...)
			c.Dir, c.Stdin, c.Stdout, c.Stderr = dir, os.Stdin, os.Stdout, os.Stderr
			if err := c.Run(); err != nil {
				var ee *exec.ExitError
				if errors.As(err, &ee) {
					return withCode(exitFailure, errors.New("the promote did not finish: read the message above"))
				}
				return err
			}
			return nil
		},
	}
	cmd.Flags().BoolVarP(&yes, "yes", "y", false, "skip the typed CONFIRM after entering your promote PIN")
	cmd.Flags().BoolVar(&yes, "confirm", false, "same as --yes")
	cmd.Flags().BoolVar(&setPin, "set-pin", false, "choose the PIN that -y asks for")
	return cmd
}
