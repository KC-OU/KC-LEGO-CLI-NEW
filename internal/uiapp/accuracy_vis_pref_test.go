package uiapp

import (
	"testing"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
)

func TestMyAccuracyVisibleByDefaultAndToggleable(t *testing.T) {
	newTestApp(t) // sets up config.UserPrefsFile in a scratch dir
	if !myAccuracyVisible("alex") {
		t.Fatal("with no prefs file yet, My Accuracy must default to shown")
	}
	if !myAccuracyVisible("") {
		t.Fatal("before sign-in (no username yet) it must always read as shown")
	}
	if err := savePrefField("alex", func(p *userPref) { p.HideMyAccuracy = true }); err != nil {
		t.Fatal(err)
	}
	if myAccuracyVisible("alex") {
		t.Fatal("hiding it should stick")
	}
	if !myAccuracyVisible("sam") {
		t.Fatal("the preference is per user — hiding it for alex must not affect sam")
	}
}

// TestHidingMyAccuracyRemovesItFromThePickerHub confirms the preference
// actually changes what resolveAndRenderMenu produces, not just the stored
// flag — the real bug this feature fixes would be forgetting to wire the two
// together.
func TestHidingMyAccuracyRemovesItFromThePickerHub(t *testing.T) {
	app := newTestApp(t)
	app.session = &auth.Session{Username: "alex"}

	hasAccuracy := func() bool {
		for _, o := range resolveAndRenderMenu(app, scrPickerHub, pickerHubCatalog, defaultPickerHubKeys) {
			if o.Label == "My accuracy" {
				return true
			}
		}
		return false
	}
	if !hasAccuracy() {
		t.Fatal("My Accuracy should show by default")
	}
	if err := savePrefField("alex", func(p *userPref) { p.HideMyAccuracy = true }); err != nil {
		t.Fatal(err)
	}
	if hasAccuracy() {
		t.Fatal("after hiding it, the picker hub must not render My Accuracy")
	}
}
