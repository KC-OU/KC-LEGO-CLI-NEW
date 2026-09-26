package uiapp

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
)

func TestOwnedPartsListsColourThenCategoryOrder(t *testing.T) {
	app := newTestApp(t)
	for _, p := range []lego.OwnedPart{
		{PartNum: "3005", Name: "Brick 1x1", Category: "Bricks", ColorID: 2, ColorName: "Green", Qty: 5},
		{PartNum: "3023", Name: "Plate 1x2", Category: "Plates", ColorID: 0, ColorName: "Black", Qty: 40},
		{PartNum: "3001", Name: "Brick 2x4", Category: "Bricks", ColorID: 4, ColorName: "Red", Qty: 19},
		{PartNum: "3004", Name: "Brick 1x2", Category: "Bricks", ColorID: 1, ColorName: "Blue", Qty: 8},
	} {
		if err := app.legoDB.AddOwnedPart(p); err != nil {
			t.Fatal(err)
		}
	}
	app.goTo(scrLegoPartOwned)
	out := plain(app.View())

	// Colour order: black, red, blue, then alphabetical (green) — a flat list, no banners.
	for _, pair := range [][2]string{{"3023", "3001"}, {"3001", "3004"}, {"3004", "3005"}} {
		if strings.Index(out, pair[0]) > strings.Index(out, pair[1]) {
			t.Errorf("%s should come before %s:\n%s", pair[0], pair[1], out)
		}
	}
}

// buildCheckFixture is the fixture shared by the two tests below: a set with parts
// in two colours (red, blue) and two categories, so the sort order can be checked.
func buildCheckFixture(t *testing.T, app *App) {
	t.Helper()
	app.rebrick = &lego.Client{}
	seedOfflineCatalog(t, app)
	for _, q := range []string{
		`INSERT INTO cat_categories (id, name) VALUES (14, 'Plates')`,
		`INSERT INTO cat_parts (part_num, name, part_cat_id) VALUES ('3002', 'Brick 2 x 3', 11), ('3623', 'Plate 1 x 3', 14)`,
		`INSERT INTO cat_inventories (id, version, set_num) VALUES (1, 1, '75192-1')`,
		`INSERT INTO cat_inventory_parts (inventory_id, part_num, color_id, quantity) VALUES
			(1,'3001',4,10),(1,'3002',4,5),(1,'3001',1,6),(1,'3623',1,4)`,
	} {
		if _, err := app.legoDB.Exec(q); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSetCheckListsColourOrderNoBanners(t *testing.T) {
	app, _ := flowApp(t)
	buildCheckFixture(t, app)
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	out := plain(app.View())

	if strings.LastIndex(out, "Red") > strings.Index(out, "Blue") {
		t.Errorf("red rows should all come before blue rows:\n%s", out)
	}
	// No colour/category banners — just the plain flat table and its Colour column.
	for _, unwanted := range []string{"BLACK", "RED\n", "BLUE\n"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("unexpected banner %q in flat parts-check table:\n%s", unwanted, out)
		}
	}
	// The selection cursor is visible and unobstructed.
	if !strings.Contains(out, "▶") {
		t.Errorf("missing selection cursor:\n%s", out)
	}
}

func TestSetMissingListsColourOrderNoBanners(t *testing.T) {
	app, _ := flowApp(t)
	buildCheckFixture(t, app)
	app.cur, app.stack = scrLegoHub, nil
	startCheck(app, "75192-1", lego.CheckIntake)
	for i := range app.checking.check.Lines {
		app.checking.check.Lines[i].Have = 0
	}
	pressAndDrain(app, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'f'}})
	typeKeys(app, "q") // dismiss the missing-parts popup finishing short queues
	app.ws().set = "75192-1"
	app.goTo(scrSetMissing)
	out := plain(app.View())

	if strings.LastIndex(out, "Red") > strings.Index(out, "Blue") {
		t.Errorf("red rows should all come before blue rows:\n%s", out)
	}
	if strings.Contains(out, "Category") {
		t.Error("Category must not be a shown column")
	}
}
