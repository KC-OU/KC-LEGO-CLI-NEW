package uiapp

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/access"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/auth"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/plugin"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/users"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

const (
	scrLogin         = "login"
	scrTwoFACode     = "twofa_code"
	scrForcedChange  = "forced_change"
	scrHub           = "hub"
	scrQuickAdd      = "quickadd"
	scrOverview      = "overview"
	scrPartDBHub     = "partdb_hub"
	scrPartDBBrowse  = "partdb_browse"
	scrPartDBResults = "partdb_results"
	scrPartDBLookup  = "partdb_lookup"
	scrPartDBDetail  = "partdb_detail"
	scrPartDBCreate  = "partdb_create"
	scrPartDBAdjust  = "partdb_adjust"
	scrScripts       = "scripts"
	scrAuditLog      = "auditlog"
	scrASN           = "asn"
	scrInventory     = "inventory"
	scrMasterData    = "master_data"
	scrMasterDataSPU = "master_data_spu"
	scrMasterDataSup = "master_data_supplier"
	scrMasterDataCus = "master_data_customer"
	scrUsers         = "users"
	scrUsersCreate   = "users_create"
	scrUsersReset    = "users_reset"
	scrUsersModify   = "users_modify"
	scrUsersToggle   = "users_toggle"
	scrUsersDelete   = "users_delete"
	scrOutbound      = "outbound"
	scrContainers    = "containers"

	scrOpsHub              = "ops_hub"
	scrAdminHub            = "admin_hub"
	scrSettingsHub         = "settings_hub"
	scrSettingsRebrickable = "settings_rebrickable"
	scrSettingsSyncAdmin   = "settings_sync_admin"
	scrSettings2FA         = "settings_2fa"
	scrSettingsPartDB      = "settings_partdb"
	scrSettingsTheme       = "settings_theme"
	scrLegoStats           = "lego_stats"
	scrLegoBuild           = "lego_build"
	scrLegoHistory         = "lego_history"
	scrLegoDetailAsk       = "lego_detail_ask"
	scrLegoDetail          = "lego_detail"
	scrLegoMissingAsk      = "lego_missing_ask"
	scrLegoMissing         = "lego_missing"
)

type undoEntry struct {
	desc string
	fn   func() error
}

// App is the single bubbletea root model; screens are plain data+closures
// registered once and dispatched to by (cur) — see screen.go.
type App struct {
	pendingCmd tea.Cmd // a command a screen asked the program to run (see the Script Hub); Update hands it to bubbletea
	theme      ui.Theme
	wms        *wmsdb.Client
	pdb        *partdb.DB
	pdbw       *partdb.Writer // creates/changes Part-DB parts through its REST API; reads use pdb
	legoDB     *lego.DB
	rebrick    *lego.Client
	audit      *audit.Logger
	users      *users.Service

	session       *auth.Session
	authed        bool // true only once login, 2FA and any forced password change are all done — global shortcuts (G, +, L) must not fire before that
	loginAttempts int
	requireTwoFA  bool   // true for gateway-spawned sessions (telnet/web) — see WMS_GATEWAY_SESSION in cmd/wms/tui.go
	touchMode     bool   // true only for the ttyd/web gateway path — see WMS_TOUCH_MODE in cmd/wms/tui.go
	graceOrigin   string // where this session came from ("local", "telnet:<addr>"); empty = unknown (web), which never gets a 2FA grace window
	lockedOut     bool   // the sign-on attempts ran out; the gateway counts such a session against the client's address
	transport     string // "telnet" when the session arrived over plaintext telnet (see SetTransport)
	remoteAddr    string
	lastInput     time.Time
	paletteOpen   bool // the Ctrl-K / F2 command palette is showing (see palette.go)
	paletteInput  string
	paletteSel    int
	helpOpen      bool             // the F1 / ? key cheat sheet is showing (see help.go)
	now           func() time.Time // the clock for idle checks; tests replace it

	screens map[string]screenModel
	stack   []string
	cur     string

	busy          string // label shown, spinning, on the message line while a background tea.Cmd runs; "" when idle
	busyFrame     int
	discordAsking bool // the export result screen is showing its "pick an expiry" sub-prompt (see discord.go)
	message       string
	messageErr    bool
	undo          []undoEntry
	quitting      bool

	// classic layout bookkeeping, set by View for touch-mode click mapping.
	activeTab string // hub option key whose tab is highlighted in the tab bar
	bodyRow   int    // terminal row the current screen's body starts on

	// scratch state a handful of screens share with a screen they navigate
	// to next (search term -> results, lookup id -> detail row cache).
	partdbSearchTerm  string
	partDetailRows    [][]string
	legoSearchTerm    string
	legoSetDetailRows [][]string
	legoFound         []foundSet // last set-search results, for the add-to-collection step
	legoSetDraft      *setDraft  // set being added/updated, between the number prompt and the confirm screen
	partFlow          *partFlow  // the part being added/updated, between the number prompt and the save
	pick              *pickState
	plugins           *plugin.Manager // your plugins, told about events (see internal/plugin)
	hooksSync         bool            // run hooks in the foreground (tests)
	height            int             // terminal rows, from the client (default 25)
	images            *img.Fetcher    // part and set pictures (cached; see internal/ui/img)
	detail            *detailReq      // what the detail screen is showing
	blRows            [][]string      // the last BrickLink lookup, for its result screen
	legoMissing       *missingView    // the last "missing parts for a set" result, for its table screen
	exportJob         *exportJob      // what the X key is exporting (see export.go)
	exportRes         *exportResult   // the saved file and its download link, once exported
	dayPick           *dayPick        // today's Set of the Day, worked out once a day (see extras.go)
	splashFrame       int             // the sign-on animation's frame, 0 when not showing (see splash.go)

	policy     *access.Policy // the access policy as of sign-on (see access.go)
	pUser      *access.User   // this user's policy entry; nil = the legacy role rules
	perms      access.Perms   // their effective permissions, when governed
	signedOnAt time.Time      // for the maximum session length
	accessEdit *accessEdit    // the group or user the Access Control screens are editing
	checking   *checkState    // the parts check in progress (see set_check.go)
	workshop   *workshopState // the Set Workshop's focus: set, order, line (see workshop.go)
	labelSets  []string       // the sets the label screen is printing

	sys       *sysStatus  // the control room's system status (controlroom.go)
	coll      *collection // cached collection figures
	logoFrame int         // the sign-on logo's sweep
	clockOn   bool
}

func NewApp(wms *wmsdb.Client, pdb *partdb.DB, legoDB *lego.DB, logger *audit.Logger, requireTwoFA, touchMode bool) *App {
	a := &App{
		theme:        ui.New().WithWidth(ui.Width),
		wms:          wms,
		pdb:          pdb,
		pdbw:         partdb.NewWriter(pdb),
		legoDB:       legoDB,
		rebrick:      lego.NewClientFor(legoDB),
		audit:        logger,
		users:        users.New(wms, pdb),
		cur:          scrLogin,
		requireTwoFA: requireTwoFA,
		touchMode:    touchMode,
		now:          time.Now,
		images:       img.NewFetcher(config.Get(config.ImageDir)),
	}
	a.plugins = plugin.Default(func(action, status, details string) {
		user, role := "plugin", ""
		if a.session != nil {
			user, role = a.session.Username, a.session.Role
		}
		a.audit.Log(user, role, action, status, details)
	})
	a.lastInput = a.now()
	a.logoFrame = logoFrames
	a.screens = buildScreens(a)
	a.screens[scrLogin].OnEnter(a)
	return a
}

// SetGraceOrigin tells the app where the session came from, which is what a
// 2FA grace window is pinned to (see twofa.MarkVerified). Left unset, the
// session never skips the code prompt.
func (a *App) SetGraceOrigin(origin string) { a.graceOrigin = origin }

func (a *App) Init() tea.Cmd {
	a.logoFrame = 0 // the sign-on logo sweeps in once, when the program starts
	return tea.Batch(idleTick(), a.statusCmd(), logoTick(), clockTick())
}

// LockedOut reports that this session ended because the sign-on attempts ran out.
func (a *App) LockedOut() bool { return a.lockedOut }

// SetTransport records how the session arrived, so the sign-on screen can warn
// that plaintext telnet exposes passwords and codes to anyone on the path.
func (a *App) SetTransport(transport, remoteAddr string) {
	a.transport, a.remoteAddr = transport, remoteAddr
}

// signOnTimeout is how long a gateway session may sit at sign-on before the
// connection is closed: scanners and forgotten terminals otherwise hold a
// TUI process (and one of the address's session slots) forever.
const signOnTimeout = 5 * time.Minute

type idleTickMsg struct{}

func idleTick() tea.Cmd {
	return tea.Tick(15*time.Second, func(time.Time) tea.Msg { return idleTickMsg{} })
}

// busy is a label shown, spinning, on the message line while a slow network
// operation (a BrickLink/BrickOwl price fetch, say) runs in the background —
// so the screen stays responsive instead of freezing for the duration.
var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

type spinnerTickMsg struct{}

func spinnerTick() tea.Cmd {
	return tea.Tick(120*time.Millisecond, func(time.Time) tea.Msg { return spinnerTickMsg{} })
}

// startBusy shows label on the message line while work runs on bubbletea's
// own goroutine; work's returned tea.Msg reaches Update() like any other
// message, so its handler is where the result gets applied and busy cleared.
func (a *App) startBusy(label string, work tea.Cmd) {
	a.busy = label
	a.busyFrame = 0
	a.pendingCmd = tea.Batch(work, spinnerTick())
}

func (a *App) stopBusy() { a.busy = "" }

// displayMessage is what the message line shows: the spinner while busy,
// otherwise the normal status/error message.
func (a *App) displayMessage() (string, bool) {
	if a.busy != "" {
		return spinnerFrames[a.busyFrame%len(spinnerFrames)] + " " + a.busy, false
	}
	return a.message, a.messageErr
}

// checkIdle locks a signed-in session that has seen no key or tap for the idle
// limit, and closes a gateway session that has sat at sign-on too long.
func (a *App) checkIdle() {
	idle := a.now().Sub(a.lastInput)
	switch {
	case a.authed && a.maxSession() > 0 && a.now().Sub(a.signedOnAt) >= a.maxSession():
		hours := int(a.maxSession() / time.Hour)
		a.audit.Log(a.session.Username, a.session.Role, "SESSION_MAX_AGE", "SUCCESS", fmt.Sprintf("signed out after %d h", hours))
		a.lock()
		a.setMsg(fmt.Sprintf("Signed out: sessions last at most %d hour(s). Sign in again.", hours), true)
	case a.authed:
		if limit := a.idleLimit(); limit > 0 && idle >= limit {
			a.audit.Log(a.session.Username, a.session.Role, "SESSION_IDLE_LOCK", "SUCCESS", fmt.Sprintf("idle %d min", int(idle/time.Minute)))
			a.lock()
			mins := int(limit / time.Minute)
			unit := "minutes"
			if mins == 1 {
				unit = "minute"
			}
			a.setMsg(fmt.Sprintf("Session locked after %d %s idle.", mins, unit), true)
		}
	case a.requireTwoFA && idle >= signOnTimeout:
		a.quitting = true
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(idleTickMsg); ok {
		a.checkIdle()
		if a.quitting {
			return a, tea.Quit
		}
		return a, idleTick()
	}
	if _, ok := msg.(spinnerTickMsg); ok {
		a.busyFrame++
		if a.busy != "" {
			return a, spinnerTick()
		}
		return a, nil
	}
	switch msg.(type) {
	case tea.KeyMsg, tea.MouseMsg:
		a.lastInput = a.now()
	}
	if used, cmd := a.splashUpdate(msg); used {
		return a, cmd
	}
	if used, cmd := a.controlRoomUpdate(msg); used {
		return a, cmd
	}
	if _, ok := msg.(tea.KeyMsg); ok && a.logoFrame < logoFrames {
		a.logoFrame = logoFrames // a key finishes the logo sweep (and is still typed)
	}
	if a.closeHelpOn(msg) {
		return a, nil
	}
	if km, ok := msg.(tea.KeyMsg); ok && a.paletteOpen && a.authed {
		a.paletteKey(km)
		return a, nil
	}
	if ws, ok := msg.(tea.WindowSizeMsg); ok {
		a.theme.Width = max(ws.Width, minWidth)
		a.height = ws.Height
		return a, nil
	}
	if mouseMsg, ok := msg.(tea.MouseMsg); ok {
		a.handleMouse(mouseMsg)
		if a.quitting {
			return a, tea.Quit
		}
		return a, nil
	}
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if a.handleGlobalKey(keyMsg) {
			if a.quitting {
				return a, tea.Quit
			}
			return a, nil
		}
		if scr, ok := a.screens[a.cur]; ok {
			scr.HandleKey(a, keyMsg)
		}
	}
	if done, ok := msg.(toolDoneMsg); ok {
		a.toolFinished(done)
	}
	if done, ok := msg.(pricesFetchedMsg); ok {
		a.pricesFetched(done)
	}
	if done, ok := msg.(discordSentMsg); ok {
		a.discordSent(done)
	}
	if a.quitting {
		return a, tea.Quit
	}
	if cmd := a.pendingCmd; cmd != nil {
		a.pendingCmd = nil
		return a, cmd
	}
	return a, nil
}

// toolFinished records how a Script Hub tool ended.
func (a *App) toolFinished(m toolDoneMsg) {
	user, role := "", ""
	if a.session != nil {
		user, role = a.session.Username, a.session.Role
	}
	if m.Err != nil {
		a.audit.Log(user, role, "TOOL_RUN", "FAILED", "tool="+m.ID+" via=script-hub")
		a.setMsg(m.ID+" did not run: "+m.Err.Error(), true)
		return
	}
	a.audit.Log(user, role, "TOOL_RUN", "SUCCESS", "tool="+m.ID+" via=script-hub")
	a.setMsg("Finished: "+m.ID, false)
}

// minWidth keeps a tiny or bogus reported size from collapsing every
// full-width rule and panel into something unreadable.
const minWidth = 40

func (a *App) View() string {
	if a.splashFrame > 0 {
		return a.splashView()
	}
	if a.helpOpen && a.authed {
		return a.helpView()
	}
	if a.paletteOpen && a.authed {
		return a.paletteView()
	}
	scr, ok := a.screens[a.cur]
	if !ok {
		return "unknown screen: " + a.cur
	}
	if a.theme.Classic {
		return a.viewClassic(scr)
	}
	a.bodyRow = ui.FrameBodyRow
	var user, role, badge, extra string
	if a.session != nil {
		user, role = a.session.Username, a.session.Role
		canWrite := a.session.Permissions != nil && a.session.Permissions.CanWrite
		badge = ui.RoleBadge(a.theme, role, canWrite)
		extra = "Source: " + a.session.Source
	}
	msg, msgErr := a.displayMessage()
	return ui.Frame(a.theme, scr.PanelID(), scr.Title(), user, role, extra, badge,
		scr.Body(a), msg, msgErr, scr.FKeys(), ui.DefaultMnemonics, "")
}

// viewClassic renders the old Python TUI's layout (see ui.ClassicFrame): the
// pre-login screens are bare (no header/legend/prompt line at all), every
// other screen gets the header band, tab bar and legend, and a prompt line
// that depends on what kind of screen it is.
func (a *App) viewClassic(scr screenModel) string {
	if fs, ok := scr.(*formScreen); ok && fs.bare {
		return scr.Body(a)
	}
	var user, role, badge, extra string
	if a.session != nil {
		user, role = a.session.Username, a.session.Role
		canWrite := a.session.Permissions != nil && a.session.Permissions.CanWrite
		badge = ui.RoleBadge(a.theme, role, canWrite)
		extra = "Source: " + srcLabel(a.session.Source)
	}
	var tabs [][2]string
	for _, o := range hubOptions(a) {
		tabs = append(tabs, [2]string{o.Key, o.Label})
	}

	var prompt string
	repeatLegend := false
	switch s := scr.(type) {
	case *menuScreen:
		if a.cur == scrHub {
			prompt = a.theme.Muted.Render("Selection or command") + "\n" + ui.RenderPrompt(a.theme, "===>", "", true)
			repeatLegend = true
		} else {
			prompt = ui.RenderPrompt(a.theme, s.selectLabel(a), "", true)
		}
	case *tableScreen, *overviewScreen:
		prompt = a.theme.Muted.Render("Press R to refresh, Q / ESC to go back")
	}

	msg, msgErr := a.displayMessage()
	out, row := ui.ClassicFrame(a.theme, scr.PanelID(), scr.Title(), user, role, extra, badge,
		tabs, a.activeTab, scr.FKeys(), scr.Body(a), msg, msgErr, repeatLegend, prompt)
	a.bodyRow = row
	return out
}

// srcLabel is the display name for a session's auth source, as the Python
// TUI's "Source: ModernWMS" / "[PartDB]" labels show it.
func srcLabel(source string) string {
	if source == "partdb" {
		return "PartDB"
	}
	return "ModernWMS"
}

// handleGlobalKey intercepts Ctrl+C, Esc/F3/F12 (back), F9 (undo), F6
// (quick add) and F10 (lock; F19 too, for keyboards that have one) always, plus the Q/U/L/+ letter mnemonics only
// when the active screen has no form field currently holding text — the
// same "empty buffer" gate the original's smart_input used, so typing into
// a field never gets hijacked by a shortcut.
func (a *App) handleGlobalKey(msg tea.KeyMsg) bool {
	// A screen taking typed text (the "/" filter) gets letters and Esc itself.
	if c, ok := a.screens[a.cur].(interface{ capturing() bool }); ok && c.capturing() && (msg.Type == tea.KeyRunes || msg.Type == tea.KeyEsc) {
		return false
	}
	switch msg.Type {
	case tea.KeyCtrlC:
		a.quitting = true
		return true
	case tea.KeyEsc, tea.KeyF3, tea.KeyF12:
		a.onBack()
		return true
	case tea.KeyF1:
		a.toggleHelp()
		return true
	case tea.KeyCtrlK, tea.KeyF2:
		a.openPalette()
		return true
	case tea.KeyF9:
		a.doUndo()
		return true
	case tea.KeyF6:
		a.openQuickAdd()
		return true
	case tea.KeyF10, tea.KeyF19:
		a.lock()
		return true
	}
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return false
	}
	scr := a.screens[a.cur]
	formEmpty := scr == nil || scr.ActiveForm() == nil || scr.ActiveForm().ActiveEmpty()
	if !formEmpty {
		return false
	}
	// On a form every letter is text, q included: a password or a search that starts with "q" must be
	// typeable one key at a time (telnet sends each key as it is pressed). Q then Enter on the first
	// field, and Esc anywhere, still leave the form; see formScreen.HandleKey.
	if scr != nil && scr.ActiveForm() != nil {
		return false
	}
	switch msg.Runes[0] {
	case '?':
		if scr != nil && scr.ActiveForm() == nil { // on a form a ? is text
			a.toggleHelp()
			return true
		}
	case 'q', 'Q':
		a.onBack()
		return true
	case 'u', 'U':
		a.doUndo()
		return true
	case 'l', 'L':
		a.lock()
		return true
	case '+':
		a.openQuickAdd()
		return true
	case 'g', 'G':
		a.toggleLego()
		return true
	}
	return false
}

func (a *App) goTo(id string) {
	if a.governed() && !a.require(screenPerm[id], "open "+id) {
		return
	}
	a.stack = append(a.stack, a.cur)
	a.cur = id
	a.message = ""
	if scr, ok := a.screens[id]; ok {
		scr.OnEnter(a)
	}
}

// onBack mirrors the original's context-dependent Q: pop one level
// normally, log out at the hub, and quit outright from the login screen.
func (a *App) onBack() {
	switch a.cur {
	case scrLogin:
		a.quitting = true
	case scrHub:
		a.logout()
	case scrTwoFACode, scrForcedChange:
		// Backing out of a pending-auth screen abandons the login: the
		// password-verified session must not survive it.
		if a.session != nil {
			a.audit.Log(a.session.Username, a.session.Role, "LOGIN_ABORTED", "DENIED", "left "+a.cur+" before completing sign-on")
		}
		a.resetSession()
	default:
		a.back()
	}
}

// back does not clear a.message: goTo already clears it on every forward
// navigation, and the dominant pattern across this package is
// setMsg(...); app.onBack() to show a result (a create/update confirmation,
// or a checkWrite/checkAdmin denial) on the screen being returned to.
// Clearing it here used to wipe that message before it was ever rendered,
// silently swallowing every such confirmation and denial app-wide.
func (a *App) back() {
	if len(a.stack) == 0 {
		a.cur = scrHub
		return
	}
	a.cur = a.stack[len(a.stack)-1]
	a.stack = a.stack[:len(a.stack)-1]
}

// popTo unwinds the navigation stack back to screen id (a multi-step flow's
// starting point), rather than one step at a time.
func (a *App) popTo(id string) {
	for a.cur != id && len(a.stack) > 0 {
		a.back()
	}
	if a.cur != id {
		a.cur = id
	}
}

func (a *App) setMsg(msg string, isErr bool) {
	a.message = msg
	a.messageErr = isErr
}

func (a *App) logout() {
	if a.session != nil {
		a.audit.Log(a.session.Username, a.session.Role, "LOGOUT", "SUCCESS", "")
	}
	a.resetSession()
}

func (a *App) lock() {
	if a.session == nil || !a.authed {
		return
	}
	a.audit.Log(a.session.Username, a.session.Role, "SESSION_LOCK", "SUCCESS", "")
	a.resetSession()
	a.setMsg("Session locked.", true)
}

func (a *App) resetSession() {
	a.session = nil
	a.authed = false
	a.pUser, a.perms = nil, access.Perms{}
	a.stack = nil
	a.cur = scrLogin
	a.loginAttempts = 0
	a.applyTheme(config.Get(config.TUITheme)) // the next person signs on in the default theme, not the last user's
	a.screens[scrLogin].OnEnter(a)
}

// enterHub is the single way past sign-on: only here does the session count
// as authenticated (a.authed), after password, 2FA and any forced password
// change have all succeeded. Classic style then shows the Python TUI's
// "Login Successful!" line on the hub.
func (a *App) enterHub() {
	a.authed = true
	a.signedOnAt = a.now()
	if a.policy == nil {
		a.loadPolicy()
	}
	if a.session != nil {
		a.legoDB.SetActor(a.session.Username) // the journal records who changed what
	}
	a.applyUserTheme()
	a.startSplash()
	a.activeTab = "1"
	a.goTo(scrHub)
	if a.session == nil {
		return
	}
	last := a.lastSignInNote()
	if a.theme.Classic {
		a.setMsg(fmt.Sprintf("Login Successful! Welcome, %s [%s] (%s). %s", a.session.Username, srcLabel(a.session.Source), a.session.Role, last), strings.Contains(last, "FAILED"))
	} else if last != "" {
		a.setMsg(last, strings.Contains(last, "FAILED"))
	}
}

func (a *App) pushUndo(desc string, fn func() error) {
	a.undo = append(a.undo, undoEntry{desc, fn})
}

func (a *App) doUndo() {
	if len(a.undo) == 0 {
		a.setMsg("Nothing to undo.", true)
		return
	}
	e := a.undo[len(a.undo)-1]
	a.undo = a.undo[:len(a.undo)-1]
	if err := e.fn(); err != nil {
		a.setMsg("Undo failed: "+err.Error(), true)
		return
	}
	a.setMsg("Undone: "+e.desc, false)
}

func (a *App) openQuickAdd() {
	if a.session == nil || !a.authed {
		return
	}
	a.goTo(scrQuickAdd)
}

// toggleLego is a single-key jump between the LEGO Collection area and the
// main hub from anywhere (not just the hub menu), so switching modules never
// needs backing out screen-by-screen or reconnecting/re-authenticating — it
// only changes a.cur/a.stack, same as goTo, just without pushing a stack
// frame, since this is a top-level toggle rather than normal drill-down nav.
func (a *App) toggleLego() {
	if a.session == nil || !a.authed || !a.moduleAllowed("lego") {
		return
	}
	if strings.HasPrefix(a.cur, "lego_") {
		a.stack = nil
		a.cur = scrHub
		a.activeTab = "1"
		a.setMsg("Switching to ModernWMS & Part-DB...", false)
	} else {
		a.stack = nil
		a.cur = scrLegoHub
		a.activeTab = "e"
		a.setMsg("Switching to LEGO Collection...", false)
	}
	if scr, ok := a.screens[a.cur]; ok {
		scr.OnEnter(a)
	}
}

// checkWrite mirrors check_write_permission: shows the denial on the
// message line and logs it, returning whether the action may proceed.
func (a *App) checkWrite(action string) bool {
	if a.governed() {
		return a.require(actionPerm[action], action)
	}
	if auth.CheckWritePermission(a.session) {
		return true
	}
	role := ""
	user := ""
	if a.session != nil {
		role, user = a.session.Role, a.session.Username
	}
	a.audit.Log(user, role, "DENIED_VIEWONLY_RESTRICTION", "DENIED", action)
	a.setMsg("ACCESS DENIED — your role does not permit this action.", true)
	return false
}

// checkAdmin mirrors checkWrite but requires the stricter IsAdmin flag
// rather than the CanWrite bit — the Settings screen changes API keys and
// login credentials, which CanWrite (e.g. a Picker's stock-quantity write
// access) was never meant to gate.
func (a *App) checkAdmin(action string) bool {
	if a.governed() {
		p, ok := actionPerm[action]
		if strings.HasPrefix(action, "script hub:") {
			p, ok = "scripts.run", true
		}
		if !ok {
			p = "settings.edit" // an admin-only action with no finer permission
		}
		return a.require(p, action)
	}
	if a.session != nil && a.session.Permissions != nil && a.session.Permissions.IsAdmin {
		return true
	}
	role, user := "", ""
	if a.session != nil {
		role, user = a.session.Role, a.session.Username
	}
	a.audit.Log(user, role, "DENIED_ADMIN_RESTRICTION", "DENIED", action)
	a.setMsg("ACCESS DENIED — admin role required.", true)
	return false
}

// clickable is implemented by screens that accept touch-mode taps; bodyRow
// is 0-indexed relative to the frame body (see ui.FrameBodyRow).
type clickable interface {
	HandleClick(app *App, bodyRow int)
}

// handleMouse maps a touch-mode left-button press to whatever the active
// screen renders on that terminal row. Mouse reporting is only enabled at
// all for touchMode sessions (the ttyd/web gateway — see WMS_TOUCH_MODE in
// cmd/wms/tui.go), so a raw telnet connection never sends MouseMsg here in
// the first place; the touchMode check below is defense in depth.
// ponytail: only menu screens are tap targets this pass (see
// menuScreen.HandleClick in screen.go) — table-row taps and touch overlays
// (on-screen numpad, a tappable Quick Add drawer) are still keyboard/F-key
// only; add them when a tablet user actually needs them.
func (a *App) handleMouse(m tea.MouseMsg) {
	if !a.touchMode || m.Action != tea.MouseActionPress || m.Button != tea.MouseButtonLeft {
		return
	}
	if a.theme.Classic && a.cur == scrLogin {
		// The login card's quit line sits directly under the panel; the
		// original registers a full-width tap target on it.
		if m.Y == strings.Count(loginPanel(a), "\n")+1 {
			a.quitting = true
		}
		return
	}
	if scr, ok := a.screens[a.cur].(clickable); ok {
		scr.HandleClick(a, m.Y-a.bodyRow)
	}
}

func (a *App) ctx() context.Context { return context.Background() }

// dockerCtx bounds calls to the docker CLI so a hung/overloaded docker
// daemon can't freeze the whole TUI event loop, which runs these calls
// synchronously on the single bubbletea Update goroutine.
func (a *App) dockerCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 5*time.Second)
}

// emit tells enabled plugins about an event without making the user wait for them.
func (a *App) emit(event string, payload any) {
	if a.plugins == nil {
		return
	}
	if a.hooksSync {
		a.plugins.Emit(context.Background(), event, payload)
		return
	}
	go a.plugins.Emit(context.Background(), event, payload)
}
