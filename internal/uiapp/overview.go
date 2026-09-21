package uiapp

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/docker"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

type overviewScreen struct {
	base
	body string
}

func (s *overviewScreen) PanelID() string { return "OVERVW" }
func (s *overviewScreen) Title() string   { return "Executive Dual-System Overview" }

func (s *overviewScreen) OnEnter(app *App) {
	ctx := app.ctx()
	var rows [][]string

	m, err := app.wms.DashboardMetrics(ctx)
	if err != nil {
		app.setMsg("ModernWMS: "+err.Error(), true)
	} else {
		rows = append(rows,
			[]string{"WMS Active SPUs", fmt.Sprintf("%d", m.SPUCount), ""},
			[]string{"WMS Stock On-Hand", fmt.Sprintf("%d", m.StockSum), sparkline(m.StockSum, 5000, 20)},
			[]string{"WMS QA-Hold SKUs", fmt.Sprintf("%d", m.FrozenCount), ""},
			[]string{"WMS Inbound ASNs", fmt.Sprintf("%d", m.ASNCount), ""},
			[]string{"WMS Dispatches", fmt.Sprintf("%d", m.DispatchCount), ""},
			[]string{"WMS Registered Users", fmt.Sprintf("%d", m.UserCount), ""},
		)
		if m.LowStockCount > 0 {
			app.setMsg(fmt.Sprintf("WARNING: %d SKU(s) at or below low-stock threshold.", m.LowStockCount), true)
		}
	}

	var partCount, catCount, locCount int
	var lotSum float64
	if parts, e := app.pdb.AllParts(); e == nil {
		partCount = len(parts)
	}
	if cats, e := app.pdb.AllCategories(); e == nil {
		catCount = len(cats)
	}
	if locs, e := app.pdb.AllStoreLocations(); e == nil {
		locCount = len(locs)
	}
	if lots, e := app.pdb.AllPartLots(); e == nil {
		for _, l := range lots {
			lotSum += l.Amount
		}
	}
	rows = append(rows,
		[]string{"Part-DB Parts", fmt.Sprintf("%d", partCount), ""},
		[]string{"Part-DB Stock On-Hand", fmt.Sprintf("%g", lotSum), sparkline(int(lotSum), 5000, 20)},
		[]string{"Part-DB Categories", fmt.Sprintf("%d", catCount), ""},
		[]string{"Part-DB Storage Locations", fmt.Sprintf("%d", locCount), ""},
	)

	if low, err := app.legoDB.LowStock(); err == nil && len(low) > 0 {
		rows = append(rows, []string{"LEGO Parts Below Minimum", fmt.Sprintf("%d LOW", len(low)), ""})
	}

	var b strings.Builder
	if app.theme.Classic {
		b.WriteString(ui.RenderGrid(app.theme, []string{"Metric", "Value", ""}, rows, "ModernWMS & PartDB KPI Analytics"))
	} else {
		b.WriteString(ui.RenderColumns(app.theme, []string{"Metric", "Value", ""}, rows, "System KPIs"))
	}
	b.WriteString("\n\n")

	dctx, cancel := app.dockerCtx()
	defer cancel()
	wmsStatus, _ := docker.InspectStatus(dctx, config.Get(config.ModernWMSContainer))
	pdbStatus, _ := docker.InspectStatus(dctx, config.Get(config.PartDBContainer))
	b.WriteString(ui.Status(app.theme, wmsStatus == "running", "ModernWMS container: "+orUnreachable(wmsStatus)))
	b.WriteString("\n")
	b.WriteString(ui.Status(app.theme, pdbStatus == "running", "Part-DB container: "+orUnreachable(pdbStatus)))

	s.body = b.String()
}

// orUnreachable is a container status, where an empty answer means docker could not be asked.
func orUnreachable(s string) string {
	if s == "" {
		return "unreachable"
	}
	return s
}

// orDash stands in for a value that is empty.
func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func sparkline(val, max, width int) string {
	if max <= 0 {
		max = 1
	}
	filled := val * width / max
	if filled > width {
		filled = width
	}
	if filled < 0 {
		filled = 0
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func (s *overviewScreen) Body(app *App) string               { return s.body }
func (s *overviewScreen) HandleKey(app *App, msg tea.KeyMsg) {}
