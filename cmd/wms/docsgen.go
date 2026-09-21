package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// newDocsGenCmd is a hidden command that writes one Markdown page per command (the CLI
// reference for the documentation site) from the command tree itself, so the reference can
// never disagree with `--help`.
func newDocsGenCmd(root *cobra.Command) *cobra.Command {
	return &cobra.Command{
		Use:    "docs-gen <dir>",
		Short:  "Write the CLI reference as Markdown (used by `make docs-generate`)",
		Hidden: true,
		Args:   cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			n, err := writeCLIReference(root, args[0])
			if err != nil {
				return err
			}
			say(fmt.Sprintf("Wrote %d command page(s) to %s", n, args[0]))
			return nil
		},
	}
}

func slug(c *cobra.Command) string {
	return strings.ReplaceAll(strings.TrimPrefix(c.CommandPath(), "wms "), " ", "-")
}

// writeCLIReference writes index.md and one page per visible command into dir.
func writeCLIReference(root *cobra.Command, dir string) (int, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return 0, err
	}
	var all []*cobra.Command
	var walk func(c *cobra.Command)
	walk = func(c *cobra.Command) {
		for _, sub := range c.Commands() {
			if sub.Hidden || sub.Name() == "help" || sub.Name() == "completion" {
				continue
			}
			all = append(all, sub)
			walk(sub)
		}
	}
	walk(root)
	sort.Slice(all, func(i, j int) bool { return all[i].CommandPath() < all[j].CommandPath() })

	var index strings.Builder
	index.WriteString("# Command reference\n\nGenerated from `--help`, so it always matches the program. Every command accepts `--json` and `--quiet`.\n" +
		"Exit codes: `0` ok, `1` failure, `2` usage, `3` authentication or setup, `4` network or service, `5` not found.\n\n")
	for _, c := range all {
		index.WriteString(fmt.Sprintf("- [`%s`](%s.md) — %s\n", c.CommandPath(), slug(c), c.Short))
		var b strings.Builder
		b.WriteString("# `" + c.CommandPath() + "`\n\n")
		if c.Short != "" {
			b.WriteString(c.Short + "\n\n")
		}
		if c.Long != "" && c.Long != c.Short {
			b.WriteString(c.Long + "\n\n")
		}
		b.WriteString("```text\n" + c.UseLine() + "\n```\n\n")
		if c.Example != "" {
			b.WriteString("## Examples\n\n```bash\n" + strings.TrimSpace(c.Example) + "\n```\n\n")
		}
		if fl := c.LocalFlags().FlagUsages(); strings.TrimSpace(fl) != "" {
			b.WriteString("## Options\n\n```text\n" + strings.TrimRight(fl, "\n") + "\n```\n\n")
		}
		var subs []string
		for _, sub := range c.Commands() {
			if !sub.Hidden && sub.Name() != "help" && sub.Name() != "completion" {
				subs = append(subs, fmt.Sprintf("- [`%s`](%s.md) — %s", sub.CommandPath(), slug(sub), sub.Short))
			}
		}
		if len(subs) > 0 {
			b.WriteString("## Subcommands\n\n" + strings.Join(subs, "\n") + "\n")
		}
		if err := os.WriteFile(filepath.Join(dir, slug(c)+".md"), []byte(b.String()), 0o644); err != nil {
			return 0, err
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "index.md"), []byte(index.String()), 0o644); err != nil {
		return 0, err
	}
	return len(all), nil
}
