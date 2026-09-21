package uiapp

import (
	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/docker"
)

// containersScreen streams `docker ps` output as plain text, matching the
// original's Containers tab (admin-only via IsModuleAllowed's empty menu
// mapping, so this screen is only reachable through the hub's admin
// short-circuit).
type containersScreen struct {
	base
	output string
}

func (s *containersScreen) PanelID() string { return "DOCKER" }
func (s *containersScreen) Title() string   { return "Container Status" }

func (s *containersScreen) OnEnter(app *App) {
	ctx, cancel := app.dockerCtx()
	defer cancel()
	out, err := docker.Ps(ctx)
	if err != nil {
		app.setMsg(err.Error(), true)
		return
	}
	s.output = out
}

func (s *containersScreen) Body(app *App) string { return s.output }

func (s *containersScreen) HandleKey(app *App, msg tea.KeyMsg) {
	if msg.Type == tea.KeyEnter || (msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && (msg.Runes[0] == 'r' || msg.Runes[0] == 'R')) {
		s.OnEnter(app)
	}
}
