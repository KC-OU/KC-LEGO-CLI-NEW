package uiapp

import (
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
)

func TestLoadingIndicatorIsOnByDefaultAndToggleable(t *testing.T) {
	newTestApp(t) // sets up config.UserPrefsFile in a scratch dir
	if !loadingEnabled("alex") {
		t.Fatal("with no prefs file yet, the loading indicator must default to on")
	}
	if !loadingEnabled("") {
		t.Fatal("before sign-in (no username yet) it must always read as on")
	}
	if err := setLoadingPrefFor(t, "alex", false); err != nil {
		t.Fatal(err)
	}
	if loadingEnabled("alex") {
		t.Fatal("turning it off should stick")
	}
	if !loadingEnabled("sam") {
		t.Fatal("the preference is per user — turning it off for alex must not affect sam")
	}
	if err := setLoadingPrefFor(t, "alex", true); err != nil {
		t.Fatal(err)
	}
	if !loadingEnabled("alex") {
		t.Fatal("turning it back on should stick")
	}
}

func TestSetLoadingPrefPreservesTheme(t *testing.T) {
	newTestApp(t)
	if err := savePref("alex", "amber"); err != nil {
		t.Fatal(err)
	}
	if err := setLoadingPrefFor(t, "alex", false); err != nil {
		t.Fatal(err)
	}
	p := loadPrefs()["alex"]
	if !p.LoadingOff || p.Theme != "amber" {
		t.Fatalf("turning off loading must not lose the theme choice: %+v", p)
	}
}

// TestStartBusyAlwaysRunsWorkButOnlyShowsTheSpinnerWhenEnabled is the core freeze-fix
// guarantee: turning the loading indicator off must never bring back a frozen-looking
// screen — only the spinner text is conditional, the background execution is not.
func TestStartBusyAlwaysRunsWorkButOnlyShowsTheSpinnerWhenEnabled(t *testing.T) {
	app := newTestApp(t)
	app.session = nil // pre-login: always shows

	ran := false
	app.startBusy("Working…", func() tea.Msg { ran = true; return nil })
	if app.busy != "Working…" {
		t.Fatal("pre-login (no session yet) must always show the spinner")
	}
	if app.pendingCmd == nil {
		t.Fatal("work must still be handed to bubbletea")
	}
	flattenBatch(app.pendingCmd)
	if !ran {
		t.Fatal("work must run")
	}

	app.busy = ""
	app.session = &auth.Session{Username: "alex"}
	if err := setLoadingPrefFor(t, "alex", false); err != nil {
		t.Fatal(err)
	}
	ran = false
	app.startBusy("Working…", func() tea.Msg { ran = true; return nil })
	if app.busy != "" {
		t.Error("with the preference off, no spinner text should show")
	}
	if app.pendingCmd == nil {
		t.Fatal("work must still run even with the spinner off — turning it off must never bring back a frozen screen")
	}
}

func setLoadingPrefFor(t *testing.T, user string, on bool) error {
	t.Helper()
	return savePrefField(user, func(p *userPref) { p.LoadingOff = !on })
}
