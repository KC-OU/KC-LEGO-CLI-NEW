package uiapp

import (
	"fmt"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func usersHubScreen() screenModel {
	return &menuScreen{
		panelID:      "USRMGT",
		title:        "User Security Maintenance",
		writeGated:   true,
		deniedAction: "USER_MANAGEMENT_ACCESS",
		options: func(app *App) []menuOption {
			return []menuOption{
				{Key: "1", Label: "List All Users", Go: func(app *App) { app.goTo("users_list") }},
				{Key: "2", Label: "Create New User", Go: func(app *App) { app.goTo(scrUsersCreate) }},
				{Key: "3", Label: "Reset Password", Go: func(app *App) { app.goTo(scrUsersReset) }},
				{Key: "4", Label: "Modify Role & Email", Go: func(app *App) { app.goTo(scrUsersModify) }},
				{Key: "5", Label: "Toggle Active/Disabled", Go: func(app *App) { app.goTo(scrUsersToggle) }},
				{Key: "6", Label: "Delete User", Go: func(app *App) { app.goTo(scrUsersDelete) }},
				{Key: "0", Label: "Return", Go: func(app *App) { app.onBack() }},
			}
		},
	}
}

func usersListScreen() screenModel {
	return &tableScreen{
		panelID: "USRLST",
		title:   "Registered Users",
		columns: []string{"System", "ID", "Username", "Role/Group", "Status", "Temp PW?", "Email"},
		fetch: func(app *App) ([][]string, string, error) {
			rows, errs := app.users.ListAll(app.ctx())
			if len(errs) > 0 {
				app.setMsg(errs[0].Error(), true)
			}
			out := make([][]string, len(rows))
			for i, r := range rows {
				out[i] = []string{r.System, r.ID, r.Username, r.RoleOrGroup, r.Status, r.TempPW, r.Email}
			}
			return out, fmt.Sprintf("%d user(s)", len(out)), nil
		},
	}
}

func usersCreateScreen() screenModel {
	return &formScreen{
		panelID: "USRNEW",
		title:   "Create New User",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Username"}, {Label: "Role (default Picker)"}, {Label: "Email"}}
		},
		submit: func(app *App, values []string) {
			username, role, email := values[0], values[1], values[2]
			if username == "" {
				app.setMsg("Username is required.", true)
				return
			}
			if role == "" {
				role = "Picker"
			}
			password := auth.GenerateTempPassword(10)
			if err := app.users.CreateUnified(app.ctx(), username, role, email, password); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "CREATE_USER", "SUCCESS", username)
			app.setMsg(fmt.Sprintf("User %q created. Temp password: %s (must change at next login)", username, password), false)
			app.onBack()
		},
	}
}

func usersResetScreen() screenModel {
	return &formScreen{
		panelID: "USRRST",
		title:   "Reset Password",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Username"}}
		},
		submit: func(app *App, values []string) {
			username := values[0]
			password := auth.GenerateTempPassword(10)
			if err := app.users.ResetUnified(app.ctx(), username, password, true); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "RESET_PASSWORD", "SUCCESS", username)
			app.setMsg(fmt.Sprintf("Password reset for %q. Temp password: %s", username, password), false)
			app.onBack()
		},
	}
}

func usersModifyScreen() screenModel {
	return &formScreen{
		panelID: "USRMOD",
		title:   "Modify Role & Email",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Username"}, {Label: "New Role (blank = keep)"}, {Label: "New Email (blank = keep)"}}
		},
		submit: func(app *App, values []string) {
			username := values[0]
			var role, email *string
			if values[1] != "" {
				role = &values[1]
			}
			if values[2] != "" {
				email = &values[2]
			}
			if err := app.users.ModifyBoth(app.ctx(), username, role, email); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "MODIFY_USER", "SUCCESS", username)
			app.setMsg(fmt.Sprintf("User %q updated.", username), false)
			app.onBack()
		},
	}
}

func usersToggleScreen() screenModel {
	return &formScreen{
		panelID: "USRTGL",
		title:   "Toggle Active / Disabled",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Username"}, {Label: "Enable? (yes/no)"}}
		},
		submit: func(app *App, values []string) {
			username := values[0]
			enable := values[1] == "yes" || values[1] == "y"
			if err := app.users.ToggleBoth(app.ctx(), username, enable); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "TOGGLE_USER", "SUCCESS", username)
			status := "disabled"
			if enable {
				status = "enabled"
			}
			app.setMsg(fmt.Sprintf("User %q is now %s.", username, status), false)
			app.onBack()
		},
	}
}

func usersDeleteScreen() screenModel {
	return &formScreen{
		panelID: "USRDEL",
		title:   "Delete User",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Username"}, {Label: `Type "yes" to confirm`}}
		},
		submit: func(app *App, values []string) {
			username, confirm := values[0], values[1]
			if confirm != "yes" {
				app.setMsg(`Deletion cancelled — type "yes" to confirm.`, true)
				return
			}
			if err := app.users.DeleteBoth(app.ctx(), username); err != nil {
				app.setMsg(err.Error(), true)
				return
			}
			app.audit.Log(app.session.Username, app.session.Role, "DELETE_USER", "SUCCESS", username)
			app.setMsg(fmt.Sprintf("User %q deleted.", username), false)
			app.onBack()
		},
	}
}
