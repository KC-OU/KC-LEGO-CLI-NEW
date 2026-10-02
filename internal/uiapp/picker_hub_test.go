package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
)

// TestPickerHubIncludesMySettings is the direct fix for this session's
// reported gap: a picker/checker had no menu path at all to My Settings
// (and therefore to Message an Admin / Request a feature inside it).
func TestPickerHubIncludesMySettings(t *testing.T) {
	app := newTestApp(t)
	setPolicy(t, func(p *access.Policy) {
		p.Users["partdb:checker3"] = &access.User{Groups: []string{"checker"}, TwoFA: access.TwoFAExempt}
	})
	signOn(app, "checker3", "")

	scr := app.screens[scrPickerHub]
	app.cur = scrPickerHub
	scr.OnEnter(app)
	var found *menuOption
	opts := scr.(*menuScreen).shown(app)
	for i := range opts {
		if opts[i].Label == "My Settings" {
			found = &opts[i]
		}
	}
	if found == nil {
		t.Fatalf("picker hub options missing My Settings: %+v", opts)
	}
	found.Go(app)
	if app.cur != scrMySettings {
		t.Fatalf("My Settings should reach scrMySettings, got %q", app.cur)
	}
}

// TestPickerHubIsCustomizableLikeTheMainHub confirms the picker hub went
// through the same registry/resolver as the main hub, not a one-off.
func TestPickerHubIsCustomizableLikeTheMainHub(t *testing.T) {
	app := newTestApp(t)
	setGlobalLayout(t, scrPickerHub, []string{"my_accuracy", "overview"})
	app.loadPolicy()

	opts := resolveAndRenderMenu(app, scrPickerHub, pickerHubCatalog, defaultPickerHubKeys)
	if len(opts) != 2 || opts[0].Label != "My accuracy" || opts[1].Label != "Overview" {
		t.Fatalf("opts = %+v, want My accuracy then Overview", opts)
	}
}

// TestPickerHubAlwaysPinsLogOutAndExit confirms those two can never be
// hidden, reordered away, or moved into a sub-menu by a layout.
func TestPickerHubAlwaysPinsLogOutAndExit(t *testing.T) {
	app := newTestApp(t)
	setGlobalLayout(t, scrPickerHub, []string{"overview"})
	app.loadPolicy()

	scr := pickerHubScreen().(*menuScreen)
	opts := scr.options(app)
	last := opts[len(opts)-1]
	secondLast := opts[len(opts)-2]
	if secondLast.Label != "Log out" || last.Label != "Exit" {
		t.Fatalf("Log out/Exit should always be pinned last, got %+v", opts)
	}
}
