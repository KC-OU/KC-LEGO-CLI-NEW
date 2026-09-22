package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"slices"
	"sort"
	"strings"

	"github.com/spf13/cobra"
	"github.com/spf13/pflag"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// `wms access`: the permission policy (groups, per-user permissions, 2FA
// exemptions, sign-in limits and timings) from the shell. The policy file is
// readable by root only, so these commands are too.

func newAccessCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "access",
		Short: "Permissions: groups, per-user access, 2FA exemptions, sign-in limits and timeouts",
		Long: "A permission is area.action (e.g. lego.export). Groups allow or deny permissions; a user belongs to groups and\n" +
			"can override single permissions. Deny beats allow. A user with no entry keeps their ModernWMS role's access.\n" +
			"Run `wms access perms` for the full list.",
	}
	cmd.AddCommand(newAccessPermsCmd(), newAccessGroupsCmd(), newAccessUserCmd(), newAccessCheckCmd(), newAccessSettingsCmd(),
		newAccessExportCmd(), newAccessImportCmd())
	return cmd
}

// changePolicy applies change under the policy lock and audits it.
func changePolicy(what string, change func(p *access.Policy) error) (*access.Policy, error) {
	p, err := access.Update(change)
	if err != nil {
		return nil, withCode(exitFailure, fmt.Errorf("not changed: %w", err))
	}
	_ = audit.New().Log(cliUser(), "", "ACCESS_CHANGED", "SUCCESS", "cli: "+what)
	return p, nil
}

func cliUser() string {
	if u := os.Getenv("SUDO_USER"); u != "" {
		return "cli:" + u
	}
	return "cli:" + os.Getenv("USER")
}

func newAccessPermsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "perms",
		Short: "List every permission",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			var out []map[string]any
			for _, a := range access.Areas {
				say(ui.Fact(t, fmt.Sprintf("%-10s", a.Name), a.Label+": "+strings.Join(a.Actions, ", ")))
				out = append(out, map[string]any{"area": a.Name, "label": a.Label, "actions": a.Actions})
			}
			say(t.Muted.Render("   Never held by a 2FA-exempt account: " + strings.Join(access.Privileged, ", ")))
			return emit(out)
		},
	}
}

// ---- groups ----

func newAccessGroupsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "groups", Short: "List, show, create, change and delete groups"}
	list := &cobra.Command{
		Use: "list", Short: "Every group, its members and how many permissions it allows", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := access.Load()
			if err != nil {
				return err
			}
			t := ui.New()
			rows := [][]string{}
			out := []map[string]any{}
			for _, name := range sortedKeys(p.Groups) {
				g := p.Groups[name]
				members := groupMembers(p, name)
				r := "no"
				if g.Restricted {
					r = "yes"
				}
				rows = append(rows, []string{name, fmt.Sprint(countVal(g.Perms, access.Allow)), fmt.Sprint(countVal(g.Perms, access.Deny)), r, fmt.Sprint(len(members)), g.Description})
				out = append(out, map[string]any{"group": name, "allows": countVal(g.Perms, access.Allow), "denies": countVal(g.Perms, access.Deny), "restricted": g.Restricted, "members": members, "description": g.Description})
			}
			say(ui.RenderColumns(t, []string{"Group", "Allow", "Deny", "No-2FA ok", "Users", "Description"}, rows, fmt.Sprintf("%d group(s)", len(rows))))
			return emit(out)
		},
	}
	show := &cobra.Command{
		Use: "show <group>", Short: "A group's permission grid", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := access.Load()
			if err != nil {
				return err
			}
			g := p.Groups[args[0]]
			if g == nil {
				return withCode(exitNotFound, fmt.Errorf("no group %q", args[0]))
			}
			say(permGrid(ui.New(), func(perm string) string { return g.Perms[perm] }))
			return emit(map[string]any{"group": args[0], "description": g.Description, "restricted": g.Restricted, "perms": g.Perms, "members": groupMembers(p, args[0])})
		},
	}
	var from, desc string
	create := &cobra.Command{
		Use: "create <group>", Short: "A new, empty group (or a copy with --from)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := strings.ToLower(args[0])
			_, err := changePolicy("group create "+name, func(p *access.Policy) error {
				if p.Groups[name] != nil {
					return fmt.Errorf("group %s already exists", name)
				}
				g := &access.Group{Description: desc, Perms: map[string]string{}}
				if from != "" {
					src := p.Groups[from]
					if src == nil {
						return fmt.Errorf("no group %q to copy", from)
					}
					for k, v := range src.Perms {
						g.Perms[k] = v
					}
					g.Restricted = src.Restricted
					if desc == "" {
						g.Description = src.Description
					}
				}
				p.Groups[name] = g
				return nil
			})
			if err == nil {
				say(ui.Status(ui.New(), true, "Created group "+name))
			}
			return err
		},
	}
	create.Flags().StringVar(&from, "from", "", "copy this group's permissions")
	create.Flags().StringVar(&desc, "description", "", "what the group is for")
	del := &cobra.Command{
		Use: "delete <group>", Short: "Delete a group (refused while users are in it)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			_, err := changePolicy("group delete "+args[0], func(p *access.Policy) error {
				if p.Groups[args[0]] == nil {
					return fmt.Errorf("no group %q", args[0])
				}
				if m := groupMembers(p, args[0]); len(m) > 0 {
					return fmt.Errorf("group %s still has %d user(s): %s", args[0], len(m), strings.Join(m, ", "))
				}
				delete(p.Groups, args[0])
				return nil
			})
			if err == nil {
				say(ui.Status(ui.New(), true, "Deleted group "+args[0]))
			}
			return err
		},
	}
	set := &cobra.Command{
		Use: "set <group> <area.action|area.*> <allow|deny|inherit>", Short: "Allow, deny or clear a permission for a group", Args: cobra.ExactArgs(3),
		Example: "  wms access groups set exporter lego.export allow\n  wms access groups set operator ops.* deny",
		RunE: func(cmd *cobra.Command, args []string) error {
			perms, err := expandPerm(args[1])
			if err != nil {
				return err
			}
			val, err := permValue(args[2])
			if err != nil {
				return err
			}
			_, err = changePolicy(fmt.Sprintf("group %s %s=%s", args[0], args[1], args[2]), func(p *access.Policy) error {
				g := p.Groups[args[0]]
				if g == nil {
					return fmt.Errorf("no group %q", args[0])
				}
				if g.Perms == nil {
					g.Perms = map[string]string{}
				}
				setPerms(g.Perms, perms, val)
				return nil
			})
			if err == nil {
				say(ui.Status(ui.New(), true, fmt.Sprintf("%s: %s = %s", args[0], args[1], args[2])))
			}
			return err
		},
	}
	restricted := &cobra.Command{
		Use: "restricted <group> <on|off>", Short: "Whether 2FA-exempt accounts may be in the group (it then may not hold admin-level permissions)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			on, err := onOff(args[1])
			if err != nil {
				return err
			}
			_, err = changePolicy("group "+args[0]+" restricted="+args[1], func(p *access.Policy) error {
				g := p.Groups[args[0]]
				if g == nil {
					return fmt.Errorf("no group %q", args[0])
				}
				g.Restricted = on
				return nil
			})
			if err == nil {
				say(ui.Status(ui.New(), true, "Updated "+args[0]))
			}
			return err
		},
	}
	cmd.AddCommand(list, show, create, del, set, restricted)
	return cmd
}

// ---- users ----

var userSource string

// userKey resolves a user argument: "partdb:bob", an existing entry "bob", or
// "bob" with --source (default modernwms, like `wms users 2fa`).
func userKey(p *access.Policy, name string) string {
	if strings.Contains(name, ":") {
		return strings.ToLower(name)
	}
	if k, ok := p.FindKey(name); ok {
		return k
	}
	return access.Key(userSource, name)
}

func newAccessUserCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "user", Short: "A user's groups, permission overrides, 2FA policy, sign-in limits and timeouts"}
	cmd.PersistentFlags().StringVar(&userSource, "source", "modernwms", "account source for a new entry: modernwms or partdb (or write partdb:name)")

	// edit changes one user's entry, creating it if needed.
	edit := func(name, what string, f func(u *access.User) error) error {
		var key string
		_, err := changePolicy("user "+name+" "+what, func(p *access.Policy) error {
			key = userKey(p, name)
			u := p.Users[key]
			if u == nil {
				u = &access.User{}
				p.Users[key] = u
			}
			return f(u)
		})
		if err == nil {
			say(ui.Status(ui.New(), true, key+": "+what))
		}
		return err
	}
	list := &cobra.Command{
		Use: "list", Short: "Every user with a policy entry", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := access.Load()
			if err != nil {
				return err
			}
			rows := [][]string{}
			out := []map[string]any{}
			for _, k := range sortedKeys(p.Users) {
				u := p.Users[k]
				rows = append(rows, []string{k, strings.Join(u.Groups, ","), orDash(u.TwoFA), orDash(strings.Join(u.Channels, ",")), orDash(u.Expires), fmt.Sprint(len(u.Perms))})
				out = append(out, map[string]any{"user": k, "entry": u})
			}
			say(ui.RenderColumns(ui.New(), []string{"User", "Groups", "2FA", "Channels", "Expires", "Overrides"}, rows,
				fmt.Sprintf("%d user(s) governed by the policy; everyone else keeps their ModernWMS role's access", len(rows))))
			return emit(out)
		},
	}
	show := &cobra.Command{
		Use: "show <user>", Short: "A user's entry and effective permissions (with the reason for each)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := access.Load()
			if err != nil {
				return err
			}
			key := userKey(p, args[0])
			u := p.Users[key]
			t := ui.New()
			if u == nil {
				say(ui.Warn(t, key+" has no policy entry: they keep their ModernWMS role's access and today's 2FA rules."))
				return emit(map[string]any{"user": key, "governed": false})
			}
			src, name, _ := strings.Cut(key, ":")
			eff := p.Effective(src, name)
			say(ui.Fact(t, "User", key))
			say(ui.Fact(t, "Groups", orDash(strings.Join(u.Groups, ", "))))
			say(ui.Fact(t, "2FA", map[string]string{"": "default (required over telnet/web)", "required": "always required", "exempt": "exempt"}[u.TwoFA]+cidrNote(u)))
			say(ui.Fact(t, "Channels", orDash(strings.Join(u.Channels, ", "))+" (none listed = all)"))
			say(ui.Fact(t, "Expires", orDash(u.Expires)))
			say(ui.Fact(t, "Timeouts", fmt.Sprintf("2FA remember %s, idle %s, max session %s",
				intOr(u.GraceMin, " min", "global"), intOr(u.IdleMin, " min", "global"), intOr(u.MaxHours, " h", "global"))))
			if u.Note != "" {
				say(ui.Fact(t, "Note", u.Note))
			}
			say(permGrid(t, func(perm string) string {
				if eff.Can(perm) {
					return access.Allow
				}
				if strings.HasPrefix(eff.Why(perm), "denied") {
					return access.Deny
				}
				return ""
			}))
			why := map[string]string{}
			for _, perm := range access.All() {
				why[perm] = eff.Why(perm)
			}
			return emit(map[string]any{"user": key, "governed": true, "entry": u, "effective": why})
		},
	}
	setGroups := &cobra.Command{
		Use: "set-groups <user> <group,...|none>", Short: "Put a user in these groups (this also brings them under the policy)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			groups := splitList(args[1])
			return edit(args[0], "groups="+strings.Join(groups, ","), func(u *access.User) error { u.Groups = groups; return nil })
		},
	}
	permCmd := func(verb string) *cobra.Command {
		return &cobra.Command{
			Use: verb + " <user> <area.action|area.*>", Short: map[string]string{"allow": "Allow a permission for this user alone", "deny": "Deny a permission for this user alone (beats every group)", "inherit": "Clear a user override (the groups decide)"}[verb],
			Args: cobra.ExactArgs(2),
			RunE: func(cmd *cobra.Command, args []string) error {
				perms, err := expandPerm(args[1])
				if err != nil {
					return err
				}
				val, _ := permValue(verb)
				return edit(args[0], args[1]+"="+verb, func(u *access.User) error {
					if u.Perms == nil {
						u.Perms = map[string]string{}
					}
					setPerms(u.Perms, perms, val)
					return nil
				})
			},
		}
	}
	var cidrs []string
	twofaCmd := &cobra.Command{
		Use: "2fa <user> <required|exempt|default>", Short: "The user's 2FA policy; exempt accounts may only be in restricted groups",
		Example: "  wms access user 2fa exportbot exempt --cidr 192.168.1.0/24\n  wms access user 2fa alex required",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			mode := strings.ToLower(args[1])
			if mode == "default" {
				mode = ""
			}
			if mode != "" && mode != access.TwoFARequired && mode != access.TwoFAExempt {
				return usageError("2FA must be required, exempt or default")
			}
			return edit(args[0], "2fa="+args[1]+" "+strings.Join(cidrs, ","), func(u *access.User) error {
				u.TwoFA = mode
				if mode == access.TwoFAExempt || cmd.Flags().Changed("cidr") {
					u.ExemptCIDRs = cidrs
				}
				if mode != access.TwoFAExempt {
					u.ExemptCIDRs = nil
				}
				return nil
			})
		},
	}
	twofaCmd.Flags().StringSliceVar(&cidrs, "cidr", nil, "exempt only from these networks (repeat or comma-separate); none = from anywhere")
	channels := &cobra.Command{
		Use: "channels <user> <telnet,web,local|all>", Short: "Which ways in the user may use", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			list := splitList(args[1])
			if slices.Contains(list, "all") {
				list = nil
			}
			return edit(args[0], "channels="+args[1], func(u *access.User) error { u.Channels = list; return nil })
		},
	}
	expires := &cobra.Command{
		Use: "expires <user> <YYYY-MM-DD|none>", Short: "The day the user's access ends", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			d := args[1]
			if d == "none" {
				d = ""
			}
			return edit(args[0], "expires="+args[1], func(u *access.User) error { u.Expires = d; return nil })
		},
	}
	var grace, idle, maxH string
	timeouts := &cobra.Command{
		Use: "timeouts <user>", Short: "Per-user 2FA remember window, idle lock and maximum session (\"global\" clears one)",
		Example: "  wms access user timeouts alex --grace 120 --idle 30 --max-hours 12\n  wms access user timeouts alex --idle global",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return edit(args[0], fmt.Sprintf("timeouts grace=%s idle=%s max=%s", grace, idle, maxH), func(u *access.User) error {
				for _, f := range []struct {
					flag, v string
					dst     **int
				}{{"grace", grace, &u.GraceMin}, {"idle", idle, &u.IdleMin}, {"max-hours", maxH, &u.MaxHours}} {
					if !cmd.Flags().Changed(f.flag) {
						continue
					}
					if f.v == "global" {
						*f.dst = nil
						continue
					}
					var n int
					if _, err := fmt.Sscan(f.v, &n); err != nil {
						return fmt.Errorf("--%s: %q is not a number or \"global\"", f.flag, f.v)
					}
					*f.dst = access.Int(n)
				}
				return nil
			})
		},
	}
	timeouts.Flags().StringVar(&grace, "grace", "", "minutes a verified 2FA code is remembered for the same address")
	timeouts.Flags().StringVar(&idle, "idle", "", "idle minutes before the session locks (0 = never)")
	timeouts.Flags().StringVar(&maxH, "max-hours", "", "hours before the session ends however active (0 = no limit)")
	var note string
	noteCmd := &cobra.Command{
		Use: "note <user> <text>", Short: "A note on the entry (what the account is for)", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			note = args[1]
			return edit(args[0], "note", func(u *access.User) error { u.Note = note; return nil })
		},
	}
	remove := &cobra.Command{
		Use: "remove <user>", Short: "Delete the user's entry (they go back to their ModernWMS role's access)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			var key string
			_, err := changePolicy("user remove "+args[0], func(p *access.Policy) error {
				key = userKey(p, args[0])
				if p.Users[key] == nil {
					return fmt.Errorf("%s has no entry", key)
				}
				delete(p.Users, key)
				return nil
			})
			if err == nil {
				say(ui.Status(ui.New(), true, "Removed "+key))
			}
			return err
		},
	}
	cmd.AddCommand(list, show, setGroups, permCmd("allow"), permCmd("deny"), permCmd("inherit"), twofaCmd, channels, expires, timeouts, noteCmd, remove)
	return cmd
}

func newAccessCheckCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use: "check <user> <area.action>", Short: "Would this user be allowed? And why", Args: cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			if !access.Valid(args[1]) {
				return usageError("%q is not a permission (see `wms access perms`)", args[1])
			}
			p, err := access.Load()
			if err != nil {
				return err
			}
			key := userKey(p, args[0])
			t := ui.New()
			if p.Users[key] == nil {
				say(ui.Warn(t, key+" has no policy entry: their ModernWMS role decides (admins can do everything)."))
				return emit(map[string]any{"user": key, "permission": args[1], "governed": false})
			}
			src, name, _ := strings.Cut(key, ":")
			eff := p.Effective(src, name)
			ok := eff.Can(args[1])
			say(ui.Status(t, ok, fmt.Sprintf("%s %s: %s", key, args[1], eff.Why(args[1]))))
			if err := emit(map[string]any{"user": key, "permission": args[1], "allowed": ok, "why": eff.Why(args[1])}); err != nil {
				return err
			}
			if !ok {
				return withCode(exitFailure, errors.New("denied"))
			}
			return nil
		},
	}
	cmd.PersistentFlags().StringVar(&userSource, "source", "modernwms", "account source: modernwms or partdb")
	return cmd
}

func newAccessSettingsCmd() *cobra.Command {
	var grace, idle, maxH, days, link int
	cmd := &cobra.Command{
		Use:   "settings",
		Short: "Global timings: 2FA remember window, idle lock, maximum session, export expiry, download link lifetime",
		Example: "  wms access settings                     # show them\n" +
			"  wms access settings --export-days 14 --link-minutes 30\n  wms access settings --idle-minutes 10 --max-session-hours 8",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			f := cmd.Flags()
			changed := f.Changed("grace-minutes") || f.Changed("idle-minutes") || f.Changed("max-session-hours") || f.Changed("export-days") || f.Changed("link-minutes")
			p, err := access.Load()
			if changed {
				var what []string
				f.Visit(func(fl *pflag.Flag) { what = append(what, fl.Name+"="+fl.Value.String()) })
				p, err = changePolicy("settings "+strings.Join(what, " "), func(p *access.Policy) error {
					s := &p.Settings
					for _, x := range []struct {
						flag string
						v    int
						dst  **int
					}{{"grace-minutes", grace, &s.GraceMin}, {"idle-minutes", idle, &s.IdleMin}, {"max-session-hours", maxH, &s.MaxHours}, {"export-days", days, &s.ExportDays}, {"link-minutes", link, &s.LinkMinutes}} {
						if f.Changed(x.flag) {
							*x.dst = access.Int(x.v)
						}
					}
					return nil
				})
			}
			if err != nil {
				return err
			}
			t := ui.New()
			g := p.GraceMinutes(nil, configMinutes(config.TwoFAGraceMinutes))
			i := p.IdleMinutes(nil, configMinutes(config.IdleLockMinutes))
			say(ui.Fact(t, "2FA remember window", fmt.Sprintf("%d min (same address signs in again without a code)", g)))
			say(ui.Fact(t, "Idle lock", fmt.Sprintf("%d min (0 = never)", i)))
			say(ui.Fact(t, "Maximum session", fmt.Sprintf("%d h (0 = no limit)", p.MaxSessionHours(nil))))
			say(ui.Fact(t, "Exports kept", fmt.Sprintf("%d day(s) in %s", p.ExportDays(), config.Get(config.ExportDir))))
			say(ui.Fact(t, "Download links last", fmt.Sprintf("%d min", p.LinkMinutes())))
			say(t.Muted.Render("   Per-user overrides: wms access user timeouts <user> --grace/--idle/--max-hours"))
			return emit(map[string]any{"grace_minutes": g, "idle_minutes": i, "max_session_hours": p.MaxSessionHours(nil), "export_days": p.ExportDays(), "link_minutes": p.LinkMinutes()})
		},
	}
	cmd.Flags().IntVar(&grace, "grace-minutes", 0, "minutes a verified 2FA code is remembered for the same address (0 = always ask)")
	cmd.Flags().IntVar(&idle, "idle-minutes", 0, "idle minutes before a session locks (0 = never)")
	cmd.Flags().IntVar(&maxH, "max-session-hours", 0, "hours before a session ends however active (0 = no limit)")
	cmd.Flags().IntVar(&days, "export-days", 0, "days exports are kept in the export folder (1-365)")
	cmd.Flags().IntVar(&link, "link-minutes", 0, "minutes a QR download link works (1-1440)")
	return cmd
}

func newAccessExportCmd() *cobra.Command {
	return &cobra.Command{
		Use: "export", Short: "Print the whole policy as JSON (a backup, or to edit and import)", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p, err := access.Load()
			if err != nil {
				return err
			}
			b, _ := json.MarshalIndent(p, "", "  ")
			fmt.Println(string(b))
			return nil
		},
	}
}

func newAccessImportCmd() *cobra.Command {
	var yes bool
	cmd := &cobra.Command{
		Use: "import <file>", Short: "Replace the policy with a JSON file (checked before anything is written)", Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			b, err := os.ReadFile(args[0])
			if err != nil {
				return err
			}
			var in access.Policy
			if err := json.Unmarshal(b, &in); err != nil {
				return usageError("%s is not a policy: %v", args[0], err)
			}
			if err := in.Validate(); err != nil {
				return usageError("%s: %v", args[0], err)
			}
			if err := confirm(fmt.Sprintf("Replace the policy with %s (%d groups, %d users)?", args[0], len(in.Groups), len(in.Users)), yes); err != nil {
				return err
			}
			_, err = changePolicy("import "+args[0], func(p *access.Policy) error {
				*p = in
				p.Seeded = true
				if p.Groups == nil {
					p.Groups = map[string]*access.Group{}
				}
				if p.Users == nil {
					p.Users = map[string]*access.User{}
				}
				return nil
			})
			if err == nil {
				say(ui.Status(ui.New(), true, "Policy imported"))
			}
			return err
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "do not ask")
	return cmd
}

// ---- helpers ----

func expandPerm(s string) ([]string, error) {
	s = strings.ToLower(s)
	if area, ok := strings.CutSuffix(s, ".*"); ok {
		var out []string
		for _, p := range access.All() {
			if strings.HasPrefix(p, area+".") {
				out = append(out, p)
			}
		}
		if len(out) == 0 {
			return nil, usageError("no area %q (see `wms access perms`)", area)
		}
		return out, nil
	}
	if !access.Valid(s) {
		return nil, usageError("%q is not a permission (see `wms access perms`)", s)
	}
	return []string{s}, nil
}

func permValue(s string) (string, error) {
	switch strings.ToLower(s) {
	case "allow":
		return access.Allow, nil
	case "deny":
		return access.Deny, nil
	case "inherit", "clear", "none":
		return "", nil
	}
	return "", usageError("%q must be allow, deny or inherit", s)
}

func setPerms(m map[string]string, perms []string, val string) {
	for _, p := range perms {
		if val == "" {
			delete(m, p)
		} else {
			m[p] = val
		}
	}
}

func onOff(s string) (bool, error) {
	switch strings.ToLower(s) {
	case "on", "yes", "true":
		return true, nil
	case "off", "no", "false":
		return false, nil
	}
	return false, usageError("%q must be on or off", s)
}

func splitList(s string) []string {
	if s == "none" || s == "" {
		return nil
	}
	var out []string
	for _, f := range strings.Split(s, ",") {
		if f = strings.TrimSpace(strings.ToLower(f)); f != "" {
			out = append(out, f)
		}
	}
	return out
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func groupMembers(p *access.Policy, g string) []string {
	var out []string
	for _, k := range sortedKeys(p.Users) {
		if slices.Contains(p.Users[k].Groups, g) {
			out = append(out, k)
		}
	}
	return out
}

func countVal(m map[string]string, v string) int {
	n := 0
	for _, x := range m {
		if x == v {
			n++
		}
	}
	return n
}

func intOr(p *int, unit, dflt string) string {
	if p == nil {
		return dflt
	}
	return fmt.Sprint(*p) + unit
}

func cidrNote(u *access.User) string {
	if u.TwoFA == access.TwoFAExempt && len(u.ExemptCIDRs) > 0 {
		return " — only from " + strings.Join(u.ExemptCIDRs, ", ")
	}
	return ""
}

func configMinutes(key string) int {
	var n int
	_, _ = fmt.Sscan(config.Get(key), &n)
	return n
}

// permGrid draws the areas × actions grid: ✓ allow, ✗ deny, · not set.
func permGrid(t ui.Theme, val func(perm string) string) string {
	var b strings.Builder
	b.WriteString(t.Strong.Render(fmt.Sprintf("  %-15s", "Area")))
	for _, act := range access.Actions {
		b.WriteString(t.Strong.Render(fmt.Sprintf(" %-4.4s", act)))
	}
	b.WriteString("\n")
	for _, a := range access.Areas {
		b.WriteString(fmt.Sprintf("  %-15s", a.Label))
		for _, act := range access.Actions {
			cell := "    "
			if slices.Contains(a.Actions, act) {
				switch val(a.Name + "." + act) {
				case access.Allow:
					cell = t.Success.Render(" ✓  ")
				case access.Deny:
					cell = t.Danger.Render(" ✗  ")
				default:
					cell = t.Muted.Render(" ·  ")
				}
			}
			b.WriteString(" " + cell)
		}
		b.WriteString("\n")
	}
	b.WriteString(t.Muted.Render("  ✓ allow   ✗ deny   · not set"))
	return b.String()
}
