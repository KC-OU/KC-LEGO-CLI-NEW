package main

import (
	"context"
	"fmt"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/users"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

func newUsersCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "users", Short: "Create/list/reset/delete users across both ModernWMS and Part-DB"}
	cmd.AddCommand(
		newUsersListCmd(), newUsersCreateCmd(), newUsersResetCmd(),
		newUsersSetPasswordCmd(), newUsersDeleteCmd(), newUsersToggleCmd(),
		newUsers2FACmd(),
	)
	return cmd
}

func openUsersService() (*users.Service, error) {
	db, err := partdb.Open("")
	if err != nil {
		return nil, err
	}
	return users.New(wmsdb.NewClient(), db), nil
}

func newUsersListCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "list",
		Aliases: []string{"l"},
		Short:   "List all users across both systems",
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			svc, err := openUsersService()
			if err != nil {
				return err
			}
			rows, errs := svc.ListAll(context.Background())
			var warnings []string
			for _, e := range errs {
				warnings = append(warnings, e.Error())
				say(ui.Warn(t, e.Error()))
			}
			var table [][]string
			list := []map[string]string{}
			for _, r := range rows {
				table = append(table, []string{r.System, r.ID, r.Username, r.RoleOrGroup, r.Status, r.TempPW, r.Email})
				list = append(list, map[string]string{"system": r.System, "id": r.ID, "username": r.Username, "role_or_group": r.RoleOrGroup, "status": r.Status, "temp_password": r.TempPW, "email": r.Email})
			}
			if out.JSON {
				return emit(map[string]any{"users": list, "warnings": warnings})
			}
			say(ui.RenderColumns(t, []string{"System", "ID", "Username", "Role/Group", "Status", "Temp PW?", "Email"}, table,
				fmt.Sprintf("Registered Users (%d)", len(table))))
			return nil
		},
	}
}

func newUsersCreateCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:     "create <username> [role] [email]",
		Aliases: []string{"c"},
		Short:   "Create a user with a random temp password (default role: Picker)",
		Args:    cobra.RangeArgs(1, 3),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			username := args[0]
			role := "Picker"
			if len(args) > 1 {
				role = args[1]
			}
			email := ""
			if len(args) > 2 {
				email = args[2]
			}
			if dryRun {
				return planned(fmt.Sprintf("create user %q with role %s in ModernWMS and Part-DB", username, role), map[string]any{"username": username, "role": role, "email": email})
			}

			svc, err := openUsersService()
			if err != nil {
				return err
			}
			password := auth.GenerateTempPassword(10)
			if err := svc.CreateUnified(context.Background(), username, role, email, password); err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}

			say(ui.Status(t, true, fmt.Sprintf("User %q created", username)))
			say(ui.Fact(t, "Generated Temporary Password", password))
			say(ui.Warn(t, "This password must be changed at next login."))
			return emit(map[string]any{"username": username, "role": role, "temporary_password": password, "must_change_password": true})
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be created and stop")
	return cmd
}

func newUsersResetCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "reset <username>",
		Aliases:           []string{"r"},
		Short:             "Reset a user's password to a new random temp password",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeUser(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return resetUnifiedPassword(args[0], auth.GenerateTempPassword(10), true, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change and stop")
	return cmd
}

func newUsersSetPasswordCmd() *cobra.Command {
	var dryRun bool
	cmd := &cobra.Command{
		Use:               "set-password <username> <new_pwd>",
		Short:             "Set a specific password (no forced change)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeUser(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			return resetUnifiedPassword(args[0], args[1], false, dryRun)
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change and stop")
	return cmd
}

func resetUnifiedPassword(username, password string, isTemp, dryRun bool) error {
	t := ui.New()
	if dryRun {
		return planned(fmt.Sprintf("set a new password for %q in both systems", username), map[string]any{"username": username, "temporary": isTemp})
	}
	svc, err := openUsersService()
	if err != nil {
		return err
	}
	if err := svc.ResetUnified(context.Background(), username, password, isTemp); err != nil {
		say(ui.Status(t, false, err.Error()))
		return err
	}

	say(ui.Status(t, true, fmt.Sprintf("Password updated for %q", username)))
	res := map[string]any{"username": username, "temporary": isTemp}
	if isTemp {
		say(ui.Fact(t, "Generated Temporary Password", password))
		say(ui.Warn(t, "This password must be changed at next login."))
		res["temporary_password"] = password
	} else {
		say(ui.Fact(t, "Password set to", password))
	}
	return emit(res)
}

func newUsersDeleteCmd() *cobra.Command {
	var yes, dryRun bool
	cmd := &cobra.Command{
		Use:               "delete <system: wms|partdb> <username>",
		Aliases:           []string{"d"},
		Short:             "Delete a user from one system (asks first; --yes to skip the question)",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeSystemThenUser,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			what := fmt.Sprintf("delete user %q from %s", args[1], args[0])
			if dryRun {
				return planned(what, map[string]any{"system": args[0], "username": args[1]})
			}
			if err := confirm("Really "+what+"?", yes); err != nil {
				return err
			}
			svc, err := openUsersService()
			if err != nil {
				return err
			}
			if err := svc.DeleteOne(context.Background(), args[0], args[1]); err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("Deleted %q from %s", args[1], args[0])))
			return emit(map[string]any{"deleted": args[1], "system": args[0]})
		},
	}
	cmd.Flags().BoolVar(&yes, "yes", false, "don't ask for confirmation")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would be deleted and stop")
	return cmd
}

func newUsersToggleCmd() *cobra.Command {
	var enable, disable, dryRun bool
	cmd := &cobra.Command{
		Use:               "toggle <system: wms|partdb> <username>",
		Short:             "Enable or disable a user account",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: completeSystemThenUser,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			if enable == disable {
				return usageError("pass exactly one of --enable or --disable")
			}
			isValid := enable
			if dryRun {
				return planned(fmt.Sprintf("set %q in %s to %s", args[1], args[0], validLabel(isValid)), map[string]any{"system": args[0], "username": args[1], "status": validLabel(isValid)})
			}

			svc, err := openUsersService()
			if err != nil {
				return err
			}
			if err := svc.ToggleOne(context.Background(), args[0], args[1], isValid); err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("%s is now %s in %s", args[1], validLabel(isValid), args[0])))
			return emit(map[string]any{"username": args[1], "system": args[0], "status": validLabel(isValid)})
		},
	}
	cmd.Flags().BoolVar(&enable, "enable", false, "activate the account")
	cmd.Flags().BoolVar(&disable, "disable", false, "deactivate the account")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show what would change and stop")
	return cmd
}

func validLabel(v bool) string {
	if v {
		return "ACTIVE"
	}
	return "DISABLED"
}
