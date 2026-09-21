package uiapp

import (
	"regexp"
	"strings"
	"testing"
	"unicode/utf8"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newTestApp(t *testing.T) *App {
	t.Helper()
	app, _ := newTestEnv(t)
	return app
}

// plain strips ANSI styling so layout assertions read the same whether or
// not the test environment gave lipgloss a color profile.
var ansiRE = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]`)

func plain(s string) string { return ansiRE.ReplaceAllString(s, "") }

// TestToggleLegoJumpsBothWays covers the new `G` global key
// (handleGlobalKey -> toggleLego): from the hub it must land on the LEGO
// hub, and from anywhere inside LEGO it must land back on the main hub —
// in both directions without touching a.session (no reconnect/re-login) and
// with a status message set on the destination screen.
func TestToggleLegoJumpsBothWays(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrHub
	sessionBefore := app.session

	app.toggleLego()
	if app.cur != scrLegoHub {
		t.Fatalf("toggleLego from hub: got screen %q, want %q", app.cur, scrLegoHub)
	}
	if app.session != sessionBefore {
		t.Fatal("toggleLego must not touch the session")
	}
	if app.message == "" || app.messageErr {
		t.Fatalf("expected a non-error status message after switching to LEGO, got %q (err=%v)", app.message, app.messageErr)
	}

	// Simulate being deep inside a LEGO sub-screen (not just the LEGO hub)
	// to confirm the toggle is a global jump, not tied to being exactly on
	// scrLegoHub.
	app.cur = scrLegoSetDetail
	app.toggleLego()
	if app.cur != scrHub {
		t.Fatalf("toggleLego from a nested LEGO screen: got screen %q, want %q", app.cur, scrHub)
	}
	if app.session != sessionBefore {
		t.Fatal("toggleLego must not touch the session")
	}
	if app.message == "" || app.messageErr {
		t.Fatalf("expected a non-error status message after switching back to the hub, got %q (err=%v)", app.message, app.messageErr)
	}
}

// TestBackPreservesMessage is the regression guard for a real bug found
// while adding the Settings screens: back() used to clear a.message
// unconditionally, which silently wiped every "X created/updated" success
// confirmation and every checkWrite/checkAdmin denial the instant a screen
// did setMsg(...); app.onBack() with a populated navigation stack — the
// dominant pattern across every Users/Lego/Settings write screen.
func TestBackPreservesMessage(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrHub
	app.goTo(scrUsers)
	app.setMsg(`User "picker9" created. Temp password: abc123`, false)
	app.onBack()

	if app.cur != scrHub {
		t.Fatalf("onBack from a goTo-populated stack: got screen %q, want %q", app.cur, scrHub)
	}
	if app.message == "" {
		t.Fatal("onBack wiped the success message before it could ever be rendered")
	}
}

// TestLoginScreenMatchesOldPythonLook pins the bare classic sign-on screen
// (the default) to the user's reference screenshot: a full-width card with
// the title inside its top border, the quit line directly under it, one
// inline prompt — and none of the 5250 chrome.
func TestLoginScreenMatchesOldPythonLook(t *testing.T) {
	app := newTestApp(t)
	app.session, app.authed = nil, false
	app.theme.Width = 100
	app.cur = scrLogin
	app.screens[scrLogin].OnEnter(app)

	lines := strings.Split(plain(app.View()), "\n")
	if len(lines) < 5 {
		t.Fatalf("login screen too short: %q", lines)
	}
	if !strings.HasPrefix(lines[0], "┌") || !strings.Contains(lines[0], " ModernWMS & PartDB Terminal Suite - Secure Login ") || !strings.HasSuffix(lines[0], "┐") {
		t.Errorf("row 0 must be the card's top border with the title inside it, got %q", lines[0])
	}
	if got := len([]rune(lines[0])); got != 100 {
		t.Errorf("the card must span the terminal width (100), got %d", got)
	}
	if !strings.Contains(lines[1], "Login using ModernWMS or PartDB account credentials.") || !strings.HasPrefix(lines[2], "└") {
		t.Errorf("unexpected card body/bottom: %q / %q", lines[1], lines[2])
	}
	if want := "  [Q] Quit / Exit Suite (Press Q / ESC / tap Quit anytime to exit)"; lines[3] != want {
		t.Errorf("row 3 (directly under the card, no blank line) = %q, want %q", lines[3], want)
	}
	if !strings.HasPrefix(lines[4], "Username / User ID (or 'q' to Quit): ") {
		t.Errorf("row 4 must be the inline username prompt, got %q", lines[4])
	}
	if len(lines) != 5 {
		t.Errorf("the password prompt must not appear until the username is answered; got %d rows: %q", len(lines), lines)
	}
	for _, chrome := range []string{"KCPARTS", "F3=Exit", "Selection or command", "===>"} {
		if strings.Contains(strings.Join(lines, "\n"), chrome) {
			t.Errorf("classic login must not show the 5250 chrome %q", chrome)
		}
	}
}

// TestLoginPromptsRevealSequentially: like smart_input, the password prompt
// only appears once the username has been entered, and stays after it.
func TestLoginPromptsRevealSequentially(t *testing.T) {
	app := newTestApp(t)
	app.session, app.authed = nil, false
	app.cur = scrLogin
	app.screens[scrLogin].OnEnter(app)

	for _, r := range "kevin" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	app.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "pw" {
		app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	out := plain(app.View())
	if !strings.Contains(out, "Username / User ID (or 'q' to Quit): kevin") || !strings.Contains(out, "Password: **") {
		t.Errorf("expected the answered username and a masked password prompt:\n%s", out)
	}
}

func TestLoginScreenLegacyStyle(t *testing.T) {
	t.Setenv("MODERNWMS_TUI_CLASSIC", "0")
	app := newTestApp(t)
	app.session, app.authed = nil, false
	app.cur = scrLogin
	app.screens[scrLogin].OnEnter(app)

	if got, want := app.screens[scrLogin].Title(), "ModernWMS & Part-DB Sign On"; got != want {
		t.Fatalf("legacy login title: got %q, want %q", got, want)
	}
	out := plain(app.View())
	for _, want := range []string{"KCPARTS", "SIGNON", "F3=Exit", "Selection or command ===>", "Username / User ID:"} {
		if !strings.Contains(out, want) {
			t.Errorf("legacy (MODERNWMS_TUI_CLASSIC=0) login lost %q:\n%s", want, out)
		}
	}
}

// TestWindowSizeStretchesLayout: rules, the header's right-aligned date and
// the card all follow the real terminal width instead of a fixed 80.
func TestWindowSizeStretchesLayout(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrHub
	app.Update(tea.WindowSizeMsg{Width: 150, Height: 40})

	lines := strings.Split(plain(app.View()), "\n")
	if got := len([]rune(lines[0])); got != 150 {
		t.Errorf("header row should span 150 columns (date at the right edge), got %d: %q", got, lines[0])
	}
	if !strings.HasPrefix(lines[0], "KCPARTS") || !regexp.MustCompile(`\d{2}:\d{2}:\d{2}$`).MatchString(lines[0]) {
		t.Errorf("header row should start with KCPARTS and end with the HH:MM:SS timestamp flush right, got %q", lines[0])
	}
	app.Update(tea.WindowSizeMsg{Width: 10, Height: 5})
	if app.theme.Width != minWidth {
		t.Errorf("a tiny reported width must be clamped to %d, got %d", minWidth, app.theme.Width)
	}
}

// TestClassicHubLayout checks the post-login screen against the old app's
// structure and — the point of computing bodyRow at render time — that the
// recorded body row really is where the caption lands, and each option's
// tap row really is where its line was drawn.
func TestClassicHubLayout(t *testing.T) {
	app := newTestApp(t)
	app.cur = scrHub
	app.activeTab = "1"
	lines := strings.Split(plain(app.View()), "\n")

	if !strings.HasPrefix(lines[0], "KCPARTS") {
		t.Errorf("row 0 = %q, want the KCPARTS header", lines[0])
	}
	if !strings.Contains(lines[5], "1=Overview") || !strings.Contains(lines[5], "2=PartDB Hub") {
		t.Errorf("row 5 should be the tab bar, got %q", lines[5])
	}
	if !strings.Contains(lines[7], "F3=Exit") {
		t.Errorf("row 7 should be the F-key legend directly under the tab bar, got %q", lines[7])
	}
	if lines[app.bodyRow] != "Main Navigation Hub:" {
		t.Errorf("bodyRow=%d points at %q, want the menu caption", app.bodyRow, lines[app.bodyRow])
	}
	opts := hubOptions(app)
	for i, o := range opts {
		row := app.bodyRow + ui.MenuOptionBodyRow(app.theme, i)
		if !strings.Contains(lines[row], o.Label) {
			t.Errorf("option %q: tap row %d shows %q", o.Label, row, lines[row])
		}
	}
	if !strings.Contains(lines[app.bodyRow+1], "> ") && !strings.Contains(lines[app.bodyRow+1], ">") {
		t.Errorf("the active tab's list row should carry the > marker: %q", lines[app.bodyRow+1])
	}
	last := lines[len(lines)-1]
	if !strings.HasPrefix(last, "===> ") {
		t.Errorf("the hub ends with the inline ===> prompt, got %q", last)
	}
	if !strings.Contains(strings.Join(lines[len(lines)-4:], "\n"), "F3=Exit") {
		t.Errorf("the hub repeats the legend at the bottom: %q", lines[len(lines)-4:])
	}
}

func TestClassicTouchQuitTapOnLogin(t *testing.T) {
	app := newTestApp(t)
	app.session, app.authed = nil, false
	app.touchMode = true
	app.cur = scrLogin
	app.screens[scrLogin].OnEnter(app)

	app.Update(tea.MouseMsg{X: 5, Y: 4, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if app.quitting {
		t.Fatal("a tap on the username prompt row must not quit")
	}
	_, cmd := app.Update(tea.MouseMsg{X: 5, Y: 3, Action: tea.MouseActionPress, Button: tea.MouseButtonLeft})
	if !app.quitting || cmd == nil {
		t.Fatal("a tap on the [Q] Quit line must quit")
	}
}

// TestShortcutsBlockedUntilSignOnCompletes is the regression guard for a
// bypass found while wiring the sign-on screens: doLoginSubmit sets
// app.session before the 2FA / forced-password-change screens, and the
// G, + and L shortcuts only checked session != nil — so pressing G at the
// authenticator-code prompt jumped straight into the app without 2FA.
func TestShortcutsBlockedUntilSignOnCompletes(t *testing.T) {
	for _, pending := range []string{scrTwoFACode, scrForcedChange} {
		app := newTestApp(t)
		app.authed = false // password verified, sign-on not finished
		app.cur = scrLogin
		app.goTo(pending)

		for _, k := range []rune{'g', '+', 'l'} {
			app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{k}})
			if app.cur != pending {
				t.Fatalf("pressing %q at %s reached %q without finishing sign-on", k, pending, app.cur)
			}
		}
		app.Update(tea.KeyMsg{Type: tea.KeyF6})
		if app.cur != pending {
			t.Fatalf("F6 at %s reached %q without finishing sign-on", pending, app.cur)
		}

		// Backing out abandons the login and drops the half-authenticated session.
		app.Update(tea.KeyMsg{Type: tea.KeyEsc})
		if app.cur != scrLogin || app.session != nil || app.authed {
			t.Fatalf("Esc at %s must return to a signed-out login screen (cur=%q session=%v authed=%v)", pending, app.cur, app.session, app.authed)
		}
	}
}

// TestToggleLegoNoopWithoutSession guards against a nil-session panic if a
// stray 'g'/'G' keypress somehow reaches the handler before login.
func TestToggleLegoNoopWithoutSession(t *testing.T) {
	app := newTestApp(t)
	app.session = nil
	app.cur = scrLogin

	app.toggleLego()
	if app.cur != scrLogin {
		t.Fatalf("toggleLego with no session must be a no-op, got screen %q", app.cur)
	}
}

// Over telnet every key arrives on its own, so a password or search that starts with "q" must not
// be read as the Q shortcut.
func TestQIsAnOrdinaryLetterOnFormsAndQEnterStillCancels(t *testing.T) {
	app := newTestApp(t)
	typeRunes := func(s string) {
		for _, r := range s {
			app.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		}
	}
	app.session, app.authed = nil, false
	app.cur, app.stack = scrLogin, nil
	app.screens[scrLogin].OnEnter(app)
	typeRunes("qa")
	key(app, tea.KeyEnter)
	typeRunes("Qwerty!")
	f := app.screens[scrLogin].(*formScreen)
	if f.fl.Value(0) != "qa" || f.fl.Value(1) != "Qwerty!" {
		t.Fatalf("a leading q must be typed: user=%q password=%q", f.fl.Value(0), f.fl.Value(1))
	}
	if app.quitting {
		t.Fatal("typing q must not quit the sign-on")
	}

	f.fl.Fields[0].Value, f.fl.Active = "", 0
	typeRunes("Q")
	if app.quitting {
		t.Fatal("q alone only types a letter")
	}
	key(app, tea.KeyEnter)
	if !app.quitting {
		t.Errorf("q then Enter in the first field leaves, as the prompt says")
	}
}

func TestLongHintsWrapAndLongMessagesEndInAnEllipsisAtEightyColumns(t *testing.T) {
	app := newTestApp(t)
	app.Update(tea.WindowSizeMsg{Width: 80, Height: 25})
	app.cur, app.stack = scrHub, nil
	app.goTo(scrLegoHub)
	app.goTo(scrLegoDetailAsk)
	out := plain(app.View())
	if !strings.Contains(out, "is a set.") {
		t.Errorf("the whole hint must be readable, not clipped at the edge:\n%s", out)
	}
	app.setMsg(strings.Repeat("long message ", 12), true)
	var msgLine string
	for _, l := range strings.Split(plain(app.View()), "\n") {
		if strings.Contains(l, "long message") {
			msgLine = l
		}
	}
	if !strings.HasSuffix(msgLine, "…") || utf8.RuneCountInString(msgLine) > 80 {
		t.Errorf("one row, marked as cut: %q", msgLine)
	}

	if _, err := app.legoDB.Exec(`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3001', 'Brick 2 x 4', 11)`); err != nil {
		t.Fatal(err)
	}
	f := app.screens[scrLegoDetailAsk].(*formScreen)
	f.fl.Fields[0].Value = "3001"
	f.submit(app, f.fl.Values())
	if app.cur != scrLegoDetail {
		t.Fatalf("the lookup should have opened the page, on %q: %s", app.cur, app.message)
	}
	app.onBack()
	if v := app.screens[scrLegoDetailAsk].(*formScreen).fl.Value(0); v != "" {
		t.Errorf("the number must not stay in the field after the page: %q", v)
	}
}
