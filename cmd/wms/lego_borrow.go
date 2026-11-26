package main

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// applyBorrowed records "--borrowed part:colour:qty:fromset" on a check that is still being filled in: each
// named line takes the borrowed quantity (so it is not missing), and a Borrowed list entry is written.
func applyBorrowed(db *lego.DB, c *lego.SetCheck, spec, by string) error {
	for _, item := range strings.Split(spec, ",") {
		item = strings.TrimSpace(item)
		if item == "" {
			continue
		}
		f := strings.Split(item, ":")
		if len(f) != 4 {
			return usageError("%q should be part:colour:qty:fromset", item)
		}
		n, err := strconv.Atoi(f[2])
		if err != nil || n < 1 {
			return usageError("%q: quantity must be a whole number of at least 1", item)
		}
		if strings.TrimSpace(f[3]) == "" {
			return usageError("%q: say which set it was borrowed from", item)
		}
		line := findCheckLine(c, f[0], f[1])
		if line == nil {
			return usageError("set %s has no %s in colour %s", c.SetNum, f[0], f[1])
		}
		line.Have = min(line.Have, max(0, line.Need-line.Borrowed-n)) // what was borrowed was short, so it was not there to count
		if _, err := db.AddBorrowedForCheck(c, lego.BorrowedPart{NeedPartNum: line.PartNum, ColorID: line.ColorID, ColorName: line.ColorName,
			Qty: n, FromSetNum: catalogNum(f[3]), CreatedBy: by}); err != nil {
			return err
		}
	}
	return nil
}

func findCheckLine(c *lego.SetCheck, part, colour string) *lego.CheckLine {
	for i := range c.Lines {
		l := &c.Lines[i]
		if !strings.EqualFold(l.PartNum, part) {
			continue
		}
		if id, err := strconv.Atoi(colour); (err == nil && id == l.ColorID) || strings.EqualFold(colour, l.ColorName) {
			return l
		}
	}
	return nil
}

func newLegoBorrowCmd() *cobra.Command {
	var part, colour, from, note, by string
	var qty int
	var list bool
	cmd := &cobra.Command{
		Use:   "borrow <set>",
		Short: "Record a part borrowed from another set to finish building this one",
		Long: "Marks part of a set's shortfall as borrowed from another set: the part counts as present (not missing) on\n" +
			"this set's last check, and the donor set shows it as lent until the replacement is back. Do this straight\n" +
			"after the check. The same list is the dashboard's Borrowed page. With --list, shows what was borrowed for the set.",
		Example: "  wms lego borrow 75192 --part 3001 --colour red --qty 2 --from 10696\n  wms lego borrow 75192 --list",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			num := catalogNum(args[0])
			if list {
				rows, err := db.BorrowedForSet(num)
				if err != nil {
					return err
				}
				for _, b := range rows {
					say(fmt.Sprintf("#%d  %s %s x%d  from %s  [%s]  %s", b.ID, b.NeedPartNum, b.ColorName, b.Qty, b.FromSetNum, b.Status, b.Note))
				}
				if len(rows) == 0 {
					say(t.Muted.Render("   nothing borrowed for " + num))
				}
				return emit(map[string]any{"set": num, "borrowed": rows})
			}
			if part == "" || from == "" {
				return usageError("--part and --from are required")
			}
			if by == "" {
				by = cliActor()
			}
			b := lego.BorrowedPart{BuildSetNum: num, NeedPartNum: part, ColorID: -1, Qty: qty, FromSetNum: catalogNum(from), Note: note, CreatedBy: by}
			if id, err := strconv.Atoi(colour); err == nil {
				b.ColorID = id
			} else {
				b.ColorName = colour
			}
			if c, _ := db.LastCheck(num); c != nil {
				if l := findCheckLine(c, part, colour); l != nil {
					b.ColorID, b.ColorName = l.ColorID, l.ColorName
				}
			}
			id, err := db.AddBorrowedPart(b)
			if err != nil {
				return err
			}
			_ = audit.New().Log(by, "", "BORROWED_PART_ADD", "SUCCESS", fmt.Sprintf("set=%s part=%s from=%s qty=%d via cli", num, part, b.FromSetNum, qty))
			say(ui.Fact(t, "Borrowed", fmt.Sprintf("#%d  %s x%d from %s for set %s", id, part, qty, b.FromSetNum, num)))
			return emit(map[string]any{"id": id, "set": num, "part": part, "qty": qty, "from": b.FromSetNum})
		},
	}
	cmd.Flags().StringVar(&part, "part", "", "the part that was needed (design number)")
	cmd.Flags().StringVar(&colour, "colour", "", "its colour (Rebrickable id or name)")
	cmd.Flags().IntVar(&qty, "qty", 1, "how many were borrowed")
	cmd.Flags().StringVar(&from, "from", "", "the set it was borrowed from")
	cmd.Flags().StringVar(&note, "note", "", "a note for the Borrowed list")
	cmd.Flags().StringVar(&by, "by", "", "who recorded it (default: your login)")
	cmd.Flags().BoolVar(&list, "list", false, "list what was borrowed for this set instead")
	return cmd
}
