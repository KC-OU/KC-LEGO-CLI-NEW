package lego

import (
	"fmt"
	"hash/fnv"
	"time"
)

// Achievements are milestones worked out from the collection as it is now, so
// nothing new is stored and they can never disagree with the data.
type Achievement struct {
	ID, Name, Desc string
	Have, Goal     int // progress toward Goal; Got when Have >= Goal
}

func (a Achievement) Got() bool { return a.Have >= a.Goal }

// Achievements lists every milestone with your progress, in a fixed order.
func (d *DB) Achievements(cond string) ([]Achievement, error) {
	s, err := d.Stats()
	if err != nil {
		return nil, err
	}
	var colours, themes int
	if err := d.QueryRow(`SELECT COUNT(DISTINCT color_id) FROM owned_parts WHERE color_id >= 0 AND qty > 0`).Scan(&colours); err != nil {
		return nil, err
	}
	if err := d.QueryRow(`SELECT COUNT(DISTINCT theme) FROM sets WHERE theme != ''`).Scan(&themes); err != nil {
		return nil, err
	}
	buildable := 0
	if res, err := d.CanBuild(BuildOptions{MinPercent: 100, MinPieces: 20, Limit: 1}); err == nil {
		buildable = len(res)
	}
	worth := 0
	currency := ""
	if v, err := d.CollectionValue(cond); err == nil && v != nil {
		worth, currency = int(v.Total), v.Currency
	}
	pieces := s.LoosePieces + s.SetPieces
	list := []Achievement{
		{"first-brick", "First Brick", "Log your first loose part", s.LoosePieces, 1},
		{"first-set", "Box Opener", "Add your first set", s.SetTitles, 1},
		{"pieces-1k", "Bucket of Bricks", "Own 1,000 pieces (loose + in sets)", pieces, 1000},
		{"pieces-10k", "Brick Pit", "Own 10,000 pieces", pieces, 10000},
		{"pieces-50k", "Brick Mountain", "Own 50,000 pieces", pieces, 50000},
		{"sets-10", "Shelf Filler", "Own 10 different sets", s.SetTitles, 10},
		{"sets-50", "Collector", "Own 50 different sets", s.SetTitles, 50},
		{"rainbow", "Rainbow", "Hold loose parts in 25 colours", colours, 25},
		{"themes-5", "Theme Hopper", "Own sets from 5 themes", themes, 5},
		{"variety-500", "Sorter", "Hold 500 different part numbers", s.DistinctParts, 500},
		{"buildable", "Master Builder", "Hold every piece of a set (20+ pieces) as loose parts", buildable, 1},
		{"parted-out", "Parts Donor", "Part out a set into loose bricks", s.PartedOut, 1},
		{"worth-100", "Worth It", fmt.Sprintf("Collection worth 100 %s at stored BrickLink prices", currencyOr(currency)), worth, 100},
		{"worth-1000", "Brick Bank", fmt.Sprintf("Collection worth 1,000 %s", currencyOr(currency)), worth, 1000},
	}
	return list, nil
}

func currencyOr(c string) string {
	if c == "" {
		return "(your currency)"
	}
	return c
}

// SetOfTheDay picks, the same for everyone on a given day, one catalog set your loose
// parts nearly cover (90%+): a small build challenge. nil when none qualifies.
func (d *DB) SetOfTheDay(day time.Time) (*BuildResult, error) {
	res, err := d.CanBuild(BuildOptions{MinPercent: 90, MinPieces: 20, Limit: 50})
	if err != nil || len(res) == 0 {
		return nil, err
	}
	h := fnv.New32a()
	_, _ = h.Write([]byte(day.Format("2006-01-02")))
	pick := res[int(h.Sum32())%len(res)]
	return &pick, nil
}
