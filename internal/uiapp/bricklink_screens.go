package uiapp

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

const (
	scrSettingsBrickLink = "settings_bricklink"
	scrLegoBLAsk         = "lego_bricklink_ask"
	scrLegoBLResult      = "lego_bricklink_result"
)

func (a *App) blClient() *bricklink.Client { return bricklink.FromConfig(a.legoDB) }

// blCtx bounds every BrickLink call the TUI makes; the screens run them synchronously.
func blCtx() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.Background(), 30*time.Second)
}

// settingsBrickLinkScreen stores the four values BrickLink issues. Nothing is shown
// as it is typed, blank keeps the current value, and saving tests them at once.
func settingsBrickLinkScreen() screenModel {
	return &formScreen{
		panelID: "SETBRL",
		title:   "BrickLink API",
		build: func(app *App) []ui.Field {
			return []ui.Field{
				{Label: "Consumer key (blank = keep)", Password: true},
				{Label: "Consumer secret (blank = keep)", Password: true},
				{Label: "Token value (blank = keep)", Password: true},
				{Label: "Token secret (blank = keep)", Password: true},
				{Label: "Price currency (blank = keep)", Value: ""},
			}
		},
		preamble: func(app *App) string {
			mask := func(key string) string { return maskSecret(config.Get(key)) }
			return app.theme.Muted.Render(fmt.Sprintf("Current: key %s, secret %s, token %s, token secret %s, prices in %s, region %s.\n"+
				"Register the API consumer at bricklink.com/v2/api/register_consumer.page for this server's IP (wms bricklink whoami shows it).\n"+
				"BrickLink looks things up BY NUMBER; it cannot search by name.",
				mask(config.BricklinkConsumerKey), mask(config.BricklinkConsumerSecret), mask(config.BricklinkToken), mask(config.BricklinkTokenSecret),
				config.Get(config.BricklinkCurrency), orDash(config.Get(config.BricklinkRegion))))
		},
		submit: func(app *App, values []string) {
			keys := []string{config.BricklinkConsumerKey, config.BricklinkConsumerSecret, config.BricklinkToken, config.BricklinkTokenSecret}
			changed := 0
			for i, k := range keys {
				if v := strings.TrimSpace(values[i]); v != "" {
					if err := config.SetOverride(k, v); err != nil {
						app.setMsg(err.Error(), true)
						return
					}
					changed++
				}
			}
			if cur := strings.ToUpper(strings.TrimSpace(values[4])); cur != "" {
				if len(cur) != 3 {
					app.setMsg("The currency is a three-letter code such as GBP, EUR or USD.", true)
					return
				}
				if err := config.SetOverride(config.BricklinkCurrency, cur); err != nil {
					app.setMsg(err.Error(), true)
					return
				}
				changed++
			}
			if changed > 0 {
				app.audit.Log(app.session.Username, app.session.Role, "SETTINGS_CHANGED", "SUCCESS", "bricklink_api")
			}
			c := app.blClient()
			if !c.Enabled() {
				app.setMsg("Saved, but BrickLink still needs all four values.", true)
				return
			}
			ctx, cancel := blCtx()
			defer cancel()
			if err := c.Ping(ctx); err != nil {
				app.setMsg("Saved, but BrickLink did not accept it: "+err.Error(), true)
				return
			}
			app.onBack()
			app.setMsg("BrickLink accepted the credentials.", false)
		},
	}
}

func legoBLAskScreen() screenModel {
	return &formScreen{
		panelID: "LEGBRL",
		title:   "BrickLink Lookup (by number)",
		build: func(app *App) []ui.Field {
			return []ui.Field{{Label: "Type (part / set / minifig)", Value: "part", Fresh: true}, {Label: "BrickLink number (e.g. 3001, 75192)"}, {Label: "Colour (parts, optional: name)"}}
		},
		preamble: func(app *App) string {
			return app.theme.Muted.Render("Live from BrickLink: the item, and its price guide (average sold, in " + config.Get(config.BricklinkCurrency) + "). Counts against your daily call budget; repeats come from the cache.")
		},
		submit: func(app *App, v []string) {
			kind := strings.ToLower(strings.TrimSpace(v[0]))
			typ := map[string]bricklink.ItemType{"part": bricklink.Part, "p": bricklink.Part, "set": bricklink.Set, "s": bricklink.Set, "minifig": bricklink.Minifig, "m": bricklink.Minifig}[kind]
			no := strings.TrimSpace(v[1])
			if typ == "" || no == "" {
				app.setMsg("Choose part, set or minifig and enter a number.", true)
				return
			}
			c := app.blClient()
			if !c.Enabled() {
				app.setMsg(bricklink.ErrNotConfigured.Error(), true)
				return
			}
			blColor := 0
			if col := strings.TrimSpace(v[2]); col != "" && typ == bricklink.Part {
				cols, _ := app.legoDB.CatalogColors()
				m, ok := matchColorName(cols, col)
				if !ok {
					app.setMsg(fmt.Sprintf("No colour %q in the catalog.", col), true)
					return
				}
				id, ok := app.legoDB.BLColorFor(m)
				if !ok {
					app.setMsg("No BrickLink colour number is known yet: run `wms bricklink colors sync`.", true)
					return
				}
				blColor = id
			}
			ctx, cancel := blCtx()
			defer cancel()
			it, err := c.GetItem(ctx, typ, no)
			if err != nil {
				app.setMsg(blMessage(err, kind, no), true)
				return
			}
			rows := [][]string{{"Number", it.No}, {"Name", it.Name}, {"Type", it.Type}}
			if it.YearReleased > 0 {
				rows = append(rows, []string{"Year", strconv.Itoa(it.YearReleased)})
			}
			if it.IsObsolete {
				rows = append(rows, []string{"Status", "obsolete on BrickLink"})
			}
			if typ != bricklink.Minifig {
				cond := strings.ToUpper(config.Get(config.BricklinkCondition))
				if p, perr := c.PriceGuide(ctx, typ, no, blColor, bricklink.Sold, cond); perr == nil {
					if p.TotalQuantity == 0 {
						rows = append(rows, []string{"Price", "no sales in the window (not free: no data)"})
					} else {
						rows = append(rows,
							[]string{"Avg sold price", fmt.Sprintf("%.4f %s (%s)", float64(p.Avg), p.CurrencyCode, map[string]string{"N": "new", "U": "used"}[p.NewOrUsed])},
							[]string{"Low / high", fmt.Sprintf("%.4f / %.4f", float64(p.Min), float64(p.Max))},
							[]string{"Based on", fmt.Sprintf("%d lot(s), %d piece(s)", p.UnitQuantity, p.TotalQuantity)})
					}
				} else {
					rows = append(rows, []string{"Price", "unavailable: " + blMessage(perr, kind, no)})
				}
			}
			app.blRows = rows
			app.goTo(scrLegoBLResult)
		},
	}
}

func legoBLResultScreen() screenModel {
	return &tableScreen{
		panelID: "LEGBRR",
		title:   "BrickLink Item",
		columns: []string{"Field", "Value"},
		boxed:   true,
		fetch: func(app *App) ([][]string, string, error) {
			return app.blRows, "BrickLink — live (cached for repeats)", nil
		},
	}
}

// blMessage turns a BrickLink error into a line for the message row.
func blMessage(err error, kind, no string) string {
	switch {
	case errors.Is(err, bricklink.ErrNotFound):
		return fmt.Sprintf("BrickLink has no %s %q.", kind, no)
	case errors.Is(err, bricklink.ErrBudget):
		return err.Error()
	}
	return err.Error()
}

// matchColorName finds a catalog colour by (case-insensitive) name or Rebrickable id and returns its id.
func matchColorName(cols []lego.Color, input string) (int, bool) {
	in := strings.ToLower(strings.TrimSpace(input))
	if n, err := strconv.Atoi(in); err == nil {
		for _, c := range cols {
			if c.ID == n {
				return c.ID, true
			}
		}
	}
	for _, c := range cols {
		if strings.ToLower(c.Name) == in {
			return c.ID, true
		}
	}
	return 0, false
}
