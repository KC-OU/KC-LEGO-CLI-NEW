package uiapp

import "testing"

func TestPicturesHiddenByDefaultAndToggleable(t *testing.T) {
	newTestApp(t) // sets up config.UserPrefsFile in a scratch dir
	if picturesEnabled("alex") {
		t.Fatal("with no prefs file yet, pictures must default to hidden")
	}
	if picturesEnabled("") {
		t.Fatal("before sign-in (no username yet) it must always read as hidden")
	}
	if err := savePrefField("alex", func(p *userPref) { p.ShowPictures = true }); err != nil {
		t.Fatal(err)
	}
	if !picturesEnabled("alex") {
		t.Fatal("showing it should stick")
	}
	if picturesEnabled("sam") {
		t.Fatal("the preference is per user — showing it for alex must not affect sam")
	}
}

func TestColorSwatchBlankForFreeTextAndUnknownColors(t *testing.T) {
	app := newTestApp(t)
	t.Setenv("NO_COLOR", "")
	if sw := colorSwatch(app, -1); sw != "" {
		t.Errorf("free-text colour (-1) should render no swatch, got %q", sw)
	}
	if sw := colorSwatch(app, 99999); sw != "" {
		t.Errorf("an unknown colour id should render no swatch, got %q", sw)
	}
}

func TestColorSwatchOffWithNoColor(t *testing.T) {
	app := newTestApp(t)
	if _, err := app.legoDB.Exec(`INSERT INTO cat_colors (id, name, rgb, is_trans) VALUES (4, 'Red', 'C91A09', 0)`); err != nil {
		t.Fatal(err)
	}
	t.Setenv("NO_COLOR", "1")
	if sw := colorSwatch(app, 4); sw != "" {
		t.Errorf("NO_COLOR must turn the swatch off entirely, got %q", sw)
	}
}
