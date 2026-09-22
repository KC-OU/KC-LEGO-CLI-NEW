package lego

import (
	"testing"
	"time"
)

func TestAchievementsFromCollection(t *testing.T) {
	d := buildDB(t)
	own(t, d, "3001", 4, "Red", 60)
	list, err := d.Achievements("N")
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]Achievement{}
	for _, a := range list {
		got[a.ID] = a
	}
	if a := got["first-brick"]; !a.Got() || a.Have != 60 {
		t.Errorf("first brick = %+v", a)
	}
	if a := got["pieces-1k"]; a.Got() || a.Have != 60 || a.Goal != 1000 {
		t.Errorf("1k pieces = %+v", a)
	}
	if got["first-set"].Got() {
		t.Error("no sets owned yet")
	}
}

func TestSetOfTheDayIsStablePerDay(t *testing.T) {
	d := buildDB(t)
	own(t, d, "3001", 4, "Red", 60)
	own(t, d, "3023", 1, "Blue", 25) // set 4-1 (25 red + 25 blue) is now fully covered
	day := time.Date(2026, 9, 22, 8, 0, 0, 0, time.UTC)
	a, err := d.SetOfTheDay(day)
	if err != nil || a == nil || a.Percent < 90 {
		t.Fatalf("set of the day = %+v, %v", a, err)
	}
	b, _ := d.SetOfTheDay(day.Add(10 * time.Hour))
	if b == nil || b.SetNum != a.SetNum {
		t.Errorf("same day, different pick: %v vs %v", a, b)
	}
}
