package lego

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
)

// ExportDetail is one part's or set's page as an export: its facts, its picture and,
// for a set, its whole parts list (so the file is a complete offline record of the set).
// The TUI replaces Facts with the richer rows its detail screen shows; the CLI uses these.
func (d *DB) ExportDetail(ctx context.Context, rb *Client, f *img.Fetcher, kind, num string, colorID int) (*ExportData, error) {
	num = strings.TrimSpace(num)
	data := &ExportData{When: time.Now()}
	var picURLs []string
	switch kind {
	case "set":
		lk := d.LookupSet(ctx, rb, num)
		if !lk.Found() {
			return nil, fmt.Errorf("set %s not found: %s", num, strings.Join(lk.Notes, " "))
		}
		s := lk.Set
		data.Title = fmt.Sprintf("Set %s %s", s.Num, s.Name)
		data.Facts = [][2]string{{"Set #", s.Num}, {"Name", s.Name}, {"Theme", s.Theme}, {"Year", strconv.Itoa(s.Year)}, {"Pieces", strconv.Itoa(s.Pieces)}, {"Details from", lk.Source}}
		if s.ImgURL != "" {
			picURLs = append(picURLs, s.ImgURL)
		}
		if mine, err := d.GetSetByNum(strings.TrimSuffix(num, "-1")); err == nil && mine != nil {
			data.Facts = append(data.Facts, [2]string{"You own", "x" + strconv.Itoa(mine.Qty)})
		}
		inv := d.LookupSetInventory(ctx, rb, num)
		for _, it := range inv.Items {
			data.Rows = append(data.Rows, ExportRow{PartNum: it.PartNum, Name: it.PartName, ColorID: it.ColorID, ColorName: it.ColorName, Qty: it.Qty, BLColor: it.BLColor})
		}
	default:
		info := d.LookupPart(ctx, rb, num)
		if info == nil || info.Name == "" {
			return nil, fmt.Errorf("part %s not found", num)
		}
		data.Title = fmt.Sprintf("Part %s %s", info.Num, info.Name)
		data.Facts = [][2]string{{"Part #", info.Num}, {"Name", info.Name}, {"Category", info.Category}, {"Details from", info.Source}}
		if c, ok := d.ColorByID(colorID); ok && colorID >= 0 {
			data.Facts = append(data.Facts, [2]string{"Colour", c.Name})
		}
		colors := make([]int, len(info.Colors))
		for i, c := range info.Colors {
			colors[i] = c.ID
		}
		picURLs = img.PartURLs(info.Num, colorID, colors)
		owned, _ := d.OwnedPartsOf(info.Num)
		for _, p := range owned {
			data.Rows = append(data.Rows, ExportRow{PartNum: p.PartNum, Name: p.Name, Category: p.Category, ColorID: p.ColorID, ColorName: p.ColorName, Qty: p.Qty, MinQty: p.MinQty, PartDBID: p.SyncedPartID})
		}
	}
	// The main picture is worth one download (it is what the page is about); the first
	// URL that answers wins.
	for _, u := range picURLs {
		if f == nil {
			data.Picture = NewImage(u, nil)
			break
		}
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		b, err := f.Get(c, u)
		cancel()
		if err == nil {
			data.Picture = NewImage(u, b)
			break
		}
	}
	if data.Picture == nil && len(picURLs) > 0 {
		data.Picture = NewImage(picURLs[0], nil)
	}
	data.AddPartImages(f)
	return data, nil
}

// AddPartImages links every part row to its Rebrickable picture, embedding the ones
// already in the picture cache (never downloading more).
func (d *ExportData) AddPartImages(f *img.Fetcher) {
	var cached func(string) []byte
	if f != nil {
		off := *f
		off.Offline = true
		cached = func(u string) []byte {
			b, _ := off.Get(context.Background(), u)
			return b
		}
	}
	d.AddImages(func(r ExportRow) string {
		if u := img.PartURLs(r.PartNum, r.ColorID, nil); len(u) > 0 {
			return u[0]
		}
		return ""
	}, cached)
}

// ExportBuild is a "what can I build" list as an export.
func ExportBuild(res []BuildResult) *ExportData {
	data := &ExportData{Title: "What can I build", When: time.Now(), Build: res}
	return data
}
