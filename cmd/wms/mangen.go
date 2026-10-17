package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/cobra/doc"
)

// newManGenCmd is a hidden command that writes one real troff man page per
// command tree (not just the top-level `wms`), via cobra's own doc.GenManTree
// — the same command tree docs-gen already turns into Markdown, pointed at a
// different renderer, so `man wms` and `man wms-lego-check` (etc.) read the
// same as `--help`, never drift from it, and cost nothing to keep in sync.
func newManGenCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:    "man-gen <dir>",
		Short:  "Write real man pages for every command (used by deploy.sh)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := args[0]
			if err := os.MkdirAll(dir, 0o755); err != nil {
				return err
			}
			header := &doc.GenManHeader{
				Title:   "WMS",
				Section: "1",
				Source:  "KC-Parts WMS",
			}
			if err := doc.GenManTree(root, header, dir); err != nil {
				return err
			}
			entries, err := os.ReadDir(dir)
			if err != nil {
				return err
			}
			say(fmt.Sprintf("Wrote %d man page(s) to %s", len(entries), dir))
			return nil
		},
	}
}
