package uiapp

import "testing"

// smokeTestScreens is every screen an admin session can reach, walked by
// both TestScreensDoNotPanic (default, classic layout) and
// TestScreensDoNotPanicLegacy (MODERNWMS_TUI_CLASSIC=0, the 5250 frame) —
// kept as one list so a screen added to one pass is never silently missing
// from the other.
var smokeTestScreens = []string{
	scrHub, scrQuickAdd, scrOverview,
	scrPartDBHub, scrPartDBBrowse, scrPartDBCreate, scrPartDBAdjust,
	scrScripts, scrAuditLog,
	scrASN, scrInventory,
	scrMasterData, scrMasterDataSPU, scrMasterDataSup, scrMasterDataCus,
	scrUsers, "users_list", scrUsersCreate, scrUsersReset, scrUsersModify, scrUsersToggle, scrUsersDelete,
	scrOutbound, scrContainers,
	scrLegoHub, scrLegoSetSearch, scrLegoSetResults, scrLegoSetLookup, scrLegoSetDetail, scrLegoSetAdd, scrLegoSetConfirm, scrPick, scrPartConfirm,
	scrLegoSetFind, scrLegoSetFound, scrLegoSetFoundAdd,
	scrLegoPartSearch, scrLegoPartResults, scrLegoPartAdd, scrLegoPartOwned, scrLegoStats, scrLegoBuild, scrLegoHistory, scrLegoDetailAsk, scrLegoDetail, scrLegoMissingAsk, scrLegoMissing,
	scrOpsHub, scrAdminHub,
	scrSettingsHub, scrSettingsRebrickable, scrSettingsSyncAdmin, scrSettings2FA, scrSettingsPartDB, scrSettingsTheme, scrSettingsBrickLink, scrLegoBLAsk, scrLegoBLResult,
}

func newSmokeTestApp(t *testing.T) *App {
	t.Helper()
	app, _ := newTestEnv(t)
	return app
}

// walkSmokeTestScreens drives OnEnter + Body for every screen in
// smokeTestScreens without a live ModernWMS container — wmsdb calls are
// expected to fail and surface an error on the message line, not panic.
// Part-DB screens exercise the real bind-mounted file on this box.
func walkSmokeTestScreens(t *testing.T, app *App) {
	t.Helper()
	for _, id := range smokeTestScreens {
		t.Run(id, func(t *testing.T) {
			app.cur = scrHub
			app.stack = nil
			app.goTo(id)
			_ = app.screens[id].Body(app)
			_ = app.View()
		})
	}
}

func TestScreensDoNotPanic(t *testing.T) {
	walkSmokeTestScreens(t, newSmokeTestApp(t))
}

// TestScreensDoNotPanicLegacy is the same walk on the 5250 frame that
// MODERNWMS_TUI_CLASSIC=0 keeps available as a rollback.
func TestScreensDoNotPanicLegacy(t *testing.T) {
	t.Setenv("MODERNWMS_TUI_CLASSIC", "0")
	walkSmokeTestScreens(t, newSmokeTestApp(t))
}
