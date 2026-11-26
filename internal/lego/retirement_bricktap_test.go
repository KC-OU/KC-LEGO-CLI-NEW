package lego

import (
	"strings"
	"testing"
)

// The Brick Tap export has a second header ("Old Date"/"New Date") for its
// changes table; those rows must use New Date and the header must not be a set.
func TestParseRetirementCSVHandlesChangesSection(t *testing.T) {
	csv := "Last update,\nTheme,Subtheme,Set #,Set Name,Age,Piece Count,Retirement Date,Notes\n" +
		"DC,Batman,67009,Arkham Batmobile,9+,317,\"Dec 31, 2027\",\n" +
		"CHANGES THIS MONTH:\nTheme,Subtheme,Set #,Set Name,Age,Piece Count,Old Date,New Date,Change\n" +
		"Star Wars,-,75443,Grogu's Homestead,4+,107,\"Dec 31, 2027\",\"Jul 31, 2027\",-5 months\n"
	got, err := ParseRetirementCSV(strings.NewReader(csv))
	if err != nil || len(got) != 2 {
		t.Fatalf("got %d rows, err %v: %+v", len(got), err, got)
	}
	if d := got[1].RetiresAt.Format("2006-01-02"); got[1].SetNum != "75443" || d != "2027-07-31" {
		t.Fatalf("changes row = %s %s", got[1].SetNum, d)
	}
}
