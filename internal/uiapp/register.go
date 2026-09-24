package uiapp

func buildScreens(app *App) map[string]screenModel {
	m := buildCoreScreens(app)
	for _, more := range []map[string]screenModel{accessScreens(), workshopScreens(), notifyScreens(), {scrLabels: &labelsScreen{}}} {
		for id, s := range more {
			m[id] = s
		}
	}
	return m
}

func buildCoreScreens(app *App) map[string]screenModel {
	return map[string]screenModel{
		scrLogin:        loginScreen(app),
		scrTwoFACode:    twoFAScreen(app),
		scrForcedChange: forcedChangeScreen(app),

		scrHub:      hubScreen(),
		scrQuickAdd: quickAddScreen(),
		scrOverview: &overviewScreen{},

		scrPartDBHub:     partDBHubScreen(),
		scrPartDBBrowse:  partDBBrowseScreen(),
		scrPartDBResults: partDBResultsScreen(),
		scrPartDBLookup:  partDBLookupScreen(),
		scrPartDBDetail:  partDBDetailScreen(),
		scrPartDBCreate:  partAddScreen(scrPartDBCreate),
		scrPick:          pickScreen(),
		scrPartConfirm:   partConfirmScreen(),
		scrPartDBAdjust:  partDBAdjustScreen(),

		scrScripts:  scriptsHubScreen(),
		scrAuditLog: auditLogScreen(),

		scrASN: asnScreen(),

		scrInventory:        inventoryScreen(),
		scrInventoryResults: inventoryResultsScreen(),

		scrMasterData:    masterDataScreen(),
		scrMasterDataSPU: masterDataSPUScreen(),
		scrMasterDataSup: masterDataSupplierScreen(),
		scrMasterDataCus: masterDataCustomerScreen(),

		scrUsers:       usersHubScreen(),
		"users_list":   usersListScreen(),
		scrUsersCreate: usersCreateScreen(),
		scrUsersReset:  usersResetScreen(),
		scrUsersModify: usersModifyScreen(),
		scrUsersToggle: usersToggleScreen(),
		scrUsersDelete: usersDeleteScreen(),

		scrOutbound:   outboundScreen(),
		scrContainers: &containersScreen{},

		scrOpsHub:   opsHubScreen(),
		scrAdminHub: adminHubScreen(),

		scrSettingsHub:         settingsHubScreen(),
		scrSettingsRebrickable: settingsRebrickableScreen(),
		scrSettingsSyncAdmin:   settingsSyncAdminScreen(),
		scrSettings2FA:         settings2FAScreen(),
		scrSettingsPartDB:      settingsPartDBScreen(),
		scrSettingsTheme:       &themeScreen{admin: true},
		scrMyTheme:             &themeScreen{},
		scrSettingsBrickLink:   settingsBrickLinkScreen(),
		scrLegoBLAsk:           legoBLAskScreen(),
		scrLegoBLResult:        legoBLResultScreen(),

		scrLegoHub:          legoHubScreen(),
		scrLegoSetSearch:    legoSetSearchScreen(),
		scrLegoSetResults:   legoSetResultsScreen(),
		scrLegoSetLookup:    legoSetLookupScreen(),
		scrLegoSetDetail:    legoSetDetailScreen(),
		scrLegoSetAdd:       legoSetAddScreen(),
		scrLegoSetConfirm:   legoSetConfirmScreen(),
		scrLegoSetFind:      legoSetFindScreen(),
		scrLegoSetFound:     legoSetFoundScreen(),
		scrLegoSetFoundAdd:  legoSetFoundAddScreen(),
		scrLegoPartSearch:   legoPartSearchScreen(),
		scrLegoPartResults:  legoPartResultsScreen(),
		scrLegoPartAdd:      partAddScreen(scrLegoPartAdd),
		scrLegoPartOwned:    legoPartOwnedScreen(),
		scrLegoStats:        legoStatsScreen(),
		scrLegoBuild:        legoBuildScreen(),
		scrLegoHistory:      legoHistoryScreen(),
		scrLegoDetailAsk:    legoDetailAskScreen(),
		scrLegoDetail:       &detailScreen{},
		scrLegoMissingAsk:   legoMissingAskScreen(),
		scrTour:             tourScreen(),
		scrLegoMissing:      legoMissingScreen(),
		scrExport:           &exportScreen{},
		scrSetCheck:         &setCheckScreen{},
		scrLegoAchievements: &achievementsScreen{},
	}
}
