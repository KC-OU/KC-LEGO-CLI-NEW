package lego

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/partdb"
)

// How an owned LEGO part becomes a Part-DB part. One Part-DB part per part AND
// colour, because stock differs by colour; the IPN (unique in Part-DB) makes
// it findable again, and it becomes the item code in ModernWMS (the sync uses
// the manufacturer number first, so that is deliberately left empty).

// legoRoot is the top-level Part-DB category all LEGO parts sit under.
const legoRoot = "Lego"

var slugRE = regexp.MustCompile(`[^a-z0-9]+`)

// IPNFor is "3001-4" for Rebrickable colour 4, "3001-x-teal" for a typed-in
// colour, and just "3001" when the colour is unknown.
func IPNFor(partNum string, colorID int, colorName string) string {
	switch {
	case colorID >= 0:
		return fmt.Sprintf("%s-%d", partNum, colorID)
	case strings.TrimSpace(colorName) != "":
		return partNum + "-x-" + strings.Trim(slugRE.ReplaceAllString(strings.ToLower(colorName), "-"), "-")
	}
	return partNum
}

// PartNameFor is "Brick 2 x 4 - Red".
func PartNameFor(name, colorName string) string {
	if strings.TrimSpace(colorName) == "" {
		return name
	}
	return name + " - " + colorName
}

// CategoryPath is Lego > <category>, or just Lego when the category is unknown.
func CategoryPath(category string) []string {
	if c := strings.TrimSpace(category); c != "" {
		return []string{legoRoot, c}
	}
	return []string{legoRoot}
}

// SpecFor builds the Part-DB part for an owned part in the given category.
func SpecFor(p OwnedPart, categoryID int) partdb.PartSpec {
	tags := []string{"lego"}
	if p.ColorName != "" {
		tags = append(tags, p.ColorName)
	}
	desc := "LEGO part " + p.PartNum
	if p.ColorID >= 0 {
		desc += fmt.Sprintf(", colour %s (Rebrickable colour %d)", p.ColorName, p.ColorID)
	} else if p.ColorName != "" {
		desc += ", colour " + p.ColorName
	}
	minAmount := float64(p.MinQty) // your minimum is Part-DB's minimum amount; 0 means "none"
	return partdb.PartSpec{
		Name:        PartNameFor(p.Name, p.ColorName),
		Description: desc,
		IPN:         IPNFor(p.PartNum, p.ColorID, p.ColorName),
		Tags:        strings.Join(tags, ","),
		CategoryID:  categoryID,
		MinAmount:   &minAmount,
	}
}
