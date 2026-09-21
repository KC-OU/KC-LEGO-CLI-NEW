package uiapp

import (
	"strings"
	"testing"
)

func testPick(t *testing.T, items []pickItem, allowFree bool, blank string) (*App, *[]string) {
	t.Helper()
	app, _ := newTestEnv(t)
	var got []string
	startPick(app, &pickState{
		Items: items, Prompt: "Colour", AllowFree: allowFree, BlankLabel: blank,
		OnPick: func(_ *App, it pickItem) { got = append(got, "pick:"+it.Key) },
		OnFree: func(_ *App, s string) { got = append(got, "free:"+s) },
	})
	return app, &got
}

func TestPickerChoosesByNumberExactNameOrUniqueSubstring(t *testing.T) {
	items := []pickItem{{Key: "4", Label: "Red"}, {Key: "59", Label: "Dark Red"}, {Key: "1", Label: "Blue"}}
	app, got := testPick(t, items, true, "")
	for _, in := range []string{"2", "red", "RED", "blu"} {
		pickSubmit(app, in)
	}
	if want := "pick:59 pick:4 pick:4 pick:1"; strings.Join(*got, " ") != want {
		t.Errorf("got %v, want %s", *got, want) // "red" is exact for Red even though Dark Red contains it
	}
}

func TestPickerRejectsBadNumbersAndAmbiguity(t *testing.T) {
	items := []pickItem{{Key: "a", Label: "Dark Red"}, {Key: "b", Label: "Dark Blue"}}
	app, got := testPick(t, items, false, "")
	for _, in := range []string{"0", "3", "dark", "zzz", ""} {
		pickSubmit(app, in)
		if !app.messageErr {
			t.Errorf("%q should be rejected with a message", in)
		}
		app.setMsg("", false)
	}
	if len(*got) != 0 {
		t.Errorf("nothing may be picked: %v", *got)
	}
}

func TestPickerFreeTextAndBlank(t *testing.T) {
	app, got := testPick(t, []pickItem{{Key: "4", Label: "Red"}}, true, "no colour")
	pickSubmit(app, "Sparkly")
	pickSubmit(app, "")
	if want := "free:Sparkly free:"; strings.Join(*got, " ") != want {
		t.Errorf("got %v, want %s", *got, want)
	}
}

func TestPickerListNarrowsAsYouType(t *testing.T) {
	items := []pickItem{{Key: "4", Label: "Red", RGB: "C91A09"}, {Key: "1", Label: "Blue"}}
	app, _ := testPick(t, items, true, "")
	out := plain(pickList(app.theme, app.pick, "bl", pickVisibleMax))
	if !strings.Contains(out, "Blue") || strings.Contains(out, "Red") {
		t.Errorf("typing 'bl' should leave only Blue:\n%s", out)
	}
	if out := plain(pickList(app.theme, app.pick, "", pickVisibleMax)); !strings.Contains(out, "1.") || !strings.Contains(out, "2.") {
		t.Errorf("the full list is numbered:\n%s", out)
	}
	many := make([]pickItem, 30)
	for i := range many {
		many[i] = pickItem{Key: "k", Label: "Colour"}
	}
	app.pick.Items = many
	if out := plain(pickList(app.theme, app.pick, "", pickVisibleMax)); !strings.Contains(out, "and 18 more") {
		t.Errorf("long lists are capped with a hint:\n%s", out)
	}
}

func TestPickRowsKeepTheHeaderOnASmallScreen(t *testing.T) {
	for h, want := range map[int]int{0: pickVisibleMax, 25: 10, 24: 9, 40: pickVisibleMax, 10: 3} {
		if got := pickRows(h); got != want {
			t.Errorf("height %d: %d rows, want %d", h, got, want)
		}
	}
}
