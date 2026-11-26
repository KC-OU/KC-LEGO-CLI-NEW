package lego

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui/img"
	"golang.org/x/image/draw"
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
		data.SetNum = s.Num
		data.Title = fmt.Sprintf("Set %s %s", s.Num, s.Name)
		data.Facts = [][2]string{{"Set #", s.Num}, {"Name", s.Name}, {"Theme", s.Theme}, {"Year", strconv.Itoa(s.Year)}, {"Pieces", strconv.Itoa(s.Pieces)}, {"Details from", lk.Source}}
		if s.ImgURL != "" {
			picURLs = append(picURLs, s.ImgURL)
		}
		if mine, err := d.GetSetByNum(strings.TrimSuffix(num, "-1")); err == nil && mine != nil {
			data.Facts = append(data.Facts, [2]string{"You own", "x" + strconv.Itoa(mine.Qty)})
		}
		inv := d.LookupSetInventory(ctx, rb, num)
		cats, rgbs := map[string]string{}, map[int]string{}
		for _, it := range inv.Items {
			cat, ok := cats[it.PartNum]
			if !ok {
				if cp, _ := d.CatalogPart(it.PartNum); cp != nil {
					cat = cp.Category
				}
				cats[it.PartNum] = cat
			}
			rgb, ok := rgbs[it.ColorID]
			if !ok {
				if c, found := d.ColorByID(it.ColorID); found && hexRGB.MatchString(c.RGB) {
					rgb = strings.ToUpper(c.RGB)
				}
				rgbs[it.ColorID] = rgb
			}
			data.Rows = append(data.Rows, ExportRow{PartNum: it.PartNum, Name: it.PartName, Category: cat, ColorID: it.ColorID, ColorName: it.ColorName, Qty: it.Qty, BLColor: it.BLColor,
				Element: d.ElementFor(it.PartNum, it.ColorID), ColorRGB: rgb})
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
	d.EmbedPartImages(ctx, data, f)
	return data, nil
}

// PrepareSortingSheet makes any export's rows ready for the sorting sheet: category, element id and colour
// swatch where they are missing, and embedded pictures (see EmbedPartImages) unless every row already has
// one. Safe to call on rows ExportDetail has already filled.
func (d *DB) PrepareSortingSheet(ctx context.Context, data *ExportData, f *img.Fetcher) {
	if data.SetNum != "" && data.Borrowed == nil {
		data.Borrowed, _ = d.BorrowedForSet(data.SetNum)
	}
	cats, rgbs := map[string]string{}, map[int]string{}
	embedded := len(data.Rows) > 0
	for i := range data.Rows {
		r := &data.Rows[i]
		embedded = embedded && r.Image != nil && r.Image.Base64 != ""
		if r.Category == "" {
			cat, ok := cats[r.PartNum]
			if !ok {
				if cp, _ := d.CatalogPart(r.PartNum); cp != nil {
					cat = cp.Category
				}
				cats[r.PartNum] = cat
			}
			r.Category = cat
		}
		if r.ColorID < 0 {
			continue
		}
		if r.Element == "" {
			r.Element = d.ElementFor(r.PartNum, r.ColorID)
		}
		if r.ColorRGB == "" {
			rgb, ok := rgbs[r.ColorID]
			if !ok {
				if c, found := d.ColorByID(r.ColorID); found && hexRGB.MatchString(c.RGB) {
					rgb = strings.ToUpper(c.RGB)
				}
				rgbs[r.ColorID] = rgb
			}
			r.ColorRGB = rgb
		}
	}
	if !embedded {
		d.EmbedPartImages(ctx, data, f)
	}
}

var hexRGB = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

// EmbedPartImages gives every row a picture and, unlike AddPartImages, downloads the ones that
// are not cached yet (8 at a time, 45 seconds in all), so a saved or printed sheet shows every
// part instead of leaving the browser to fetch each one later and miss some.
//
// Per row the pictures tried are, best first: the fixed picture, the element photo for this colour,
// the colour render, then the part's element photos and renders in other colours. The first that
// answers is embedded. A row where none answers keeps the first URL, so the page can still try.
func (d *DB) EmbedPartImages(ctx context.Context, data *ExportData, f *img.Fetcher) {
	if f == nil || f.Offline {
		data.AddPartImages(f)
		return
	}
	type key struct {
		part  string
		color int
	}
	urls := map[key][]string{}
	for _, r := range data.Rows {
		k := key{r.PartNum, r.ColorID}
		if _, done := urls[k]; done {
			continue
		}
		var list []string
		if o := d.ImageOverrideFor(r.PartNum, r.ColorID); o != "" {
			list = append(list, o)
		}
		if r.Element != "" {
			list = append(list, "https://cdn.rebrickable.com/media/parts/elements/"+r.Element+".jpg")
		}
		list = append(list, img.PartURLs(r.PartNum, r.ColorID, nil)...)
		list = append(list, d.PartImageURLs(r.PartNum, r.ColorID)...)
		urls[k] = list
	}
	ctx, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	got := map[key]*Image{}
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	for k, list := range urls {
		wg.Add(1)
		sem <- struct{}{}
		go func(k key, list []string) {
			defer wg.Done()
			defer func() { <-sem }()
			var im *Image
			tried := map[string]bool{}
			for _, u := range list {
				if tried[u] || ctx.Err() != nil {
					continue
				}
				tried[u] = true
				c, cc := context.WithTimeout(ctx, 8*time.Second)
				b, err := f.Get(c, u)
				cc()
				if err == nil {
					im = NewImage(u, thumbnail(b))
					break
				}
			}
			if im == nil && len(list) > 0 {
				im = NewImage(list[0], nil)
			}
			mu.Lock()
			got[k] = im
			mu.Unlock()
		}(k, list)
	}
	wg.Wait()
	for i := range data.Rows {
		data.Rows[i].Image = got[key{data.Rows[i].PartNum, data.Rows[i].ColorID}]
	}
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

// thumbnail shrinks a row picture to at most 120x90 pixels (a sheet shows it about 36 px wide, so
// this is still sharp on paper) and re-encodes it, which cuts the file to a fraction. A picture that is
// already small, or that cannot be decoded, is returned as it was.
func thumbnail(b []byte) []byte {
	const maxW, maxH = 120, 90
	cfg, format, err := image.DecodeConfig(bytes.NewReader(b))
	if err != nil || (cfg.Width <= maxW && cfg.Height <= maxH) {
		return b
	}
	src, err := img.Decode(b)
	if err != nil {
		return b
	}
	w, h := maxW, cfg.Height*maxW/cfg.Width
	if h > maxH {
		w, h = cfg.Width*maxH/cfg.Height, maxH
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	var out bytes.Buffer
	if format == "png" {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, dst)
	} else {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 80})
	}
	if err != nil || out.Len() >= len(b) {
		return b
	}
	return out.Bytes()
}
