package main

import (
	"context"
	"sort"
	"strings"

	"github.com/spf13/cobra"
)

// Dynamic shell completion (`source <(wms completion bash)`): user names,
// systems, part numbers and the colours a part comes in. Completion must be
// quiet and quick, so any failure just offers nothing.

func withPrefix(all []string, prefix string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range all {
		if s != "" && !seen[s] && strings.HasPrefix(strings.ToLower(s), strings.ToLower(prefix)) {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

func usernames() []string {
	svc, err := openUsersService()
	if err != nil {
		return nil
	}
	rows, _ := svc.ListAll(context.Background())
	var names []string
	for _, r := range rows {
		names = append(names, r.Username)
	}
	return names
}

// completeUser completes a user name at argument position pos.
func completeUser(pos int) func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
	return func(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
		if len(args) != pos {
			return nil, cobra.ShellCompDirectiveNoFileComp
		}
		return withPrefix(usernames(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
}

// completeSystemThenUser completes `<system> <user>`.
func completeSystemThenUser(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	switch len(args) {
	case 0:
		return withPrefix([]string{"wms", "partdb"}, toComplete), cobra.ShellCompDirectiveNoFileComp
	case 1:
		return withPrefix(usernames(), toComplete), cobra.ShellCompDirectiveNoFileComp
	}
	return nil, cobra.ShellCompDirectiveNoFileComp
}

// completePartNum offers parts you already own first, then catalog matches.
func completePartNum(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	db, err := openLego()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()
	var nums []string
	if owned, err := db.ListOwnedParts(); err == nil {
		for _, p := range owned {
			nums = append(nums, p.PartNum)
		}
	}
	nums = withPrefix(nums, toComplete)
	if cat, err := db.CatalogPartNums(toComplete, 40); err == nil {
		for _, n := range cat {
			if len(nums) >= 60 {
				break
			}
			nums = append(nums, n)
		}
	}
	return nums, cobra.ShellCompDirectiveNoFileComp
}

// completeColor offers the colours of the part already typed as the first argument.
func completeColor(_ *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) != 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	db, err := openLego()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	defer db.Close()
	cols, err := db.CatalogColorsFor(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var names []string
	for _, c := range cols {
		names = append(names, c.Name)
	}
	return withPrefix(names, toComplete), cobra.ShellCompDirectiveNoFileComp
}
