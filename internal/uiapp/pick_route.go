package uiapp

import "github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"

// Pick-route ordering — the actual lookup now lives in internal/lego
// (pickroute.go) so the mobile API can share it too; these stay as thin
// App-bound wrappers so every existing call site in this package is
// unchanged.

func (a *App) locationFor(partNum string, colorID int, colorName string) string {
	return a.legoDB.LocationFor(a.pdb, partNum, colorID, colorName)
}

func (a *App) sortCheckLinesByLocation(lines []lego.CheckLine) {
	a.legoDB.SortCheckLinesByLocation(a.pdb, lines)
}

func (a *App) sortedOrderLines(lines []lego.OrderLine) []lego.OrderLine {
	return a.legoDB.SortedOrderLines(a.pdb, lines)
}
