package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// Cobra's default Args validator (legacyArgs) only does "unknown command, did you
// mean this?" checking at the root — every deeper group command (`wms lego`,
// `wms lego report`, ...) silently swallows a typo'd subcommand name and just prints
// its own help instead, which is worse for exactly the case a typo is most likely:
// a mistyped subcommand, not a mistyped top-level one. Worse still, cobra's
// Command.execute() never even calls a custom Args validator on a command that has
// no Run/RunE of its own (`if !c.Runnable() { return flag.ErrHelp }` fires first) —
// so a group command needs both a RunE (to become "runnable" at all) and this Args
// validator; applyFuzzyGroupArgs gives every such group both.
func applyFuzzyGroupArgs(cmd *cobra.Command) {
	if !cmd.Runnable() && cmd.HasSubCommands() {
		if cmd.Args == nil {
			cmd.Args = fuzzyGroupArgs
		}
		cmd.RunE = func(cmd *cobra.Command, args []string) error { return cmd.Help() }
	}
	for _, c := range cmd.Commands() {
		applyFuzzyGroupArgs(c)
	}
}

func fuzzyGroupArgs(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}
	var sb strings.Builder
	if !cmd.DisableSuggestions {
		// SuggestionsFor uses this directly with no fallback — cobra only ever
		// defaults it to 2 as a side effect of its own unexported findSuggestions,
		// which legacyArgs (the root's default Args validator) calls but we don't.
		if cmd.SuggestionsMinimumDistance <= 0 {
			cmd.SuggestionsMinimumDistance = 2
		}
		if sugg := cmd.SuggestionsFor(args[0]); len(sugg) > 0 {
			sb.WriteString("\n\nDid you mean this?\n")
			for _, s := range sugg {
				fmt.Fprintf(&sb, "\t%v\n", s)
			}
		}
	}
	return fmt.Errorf("unknown command %q for %q%s", args[0], cmd.CommandPath(), sb.String())
}
