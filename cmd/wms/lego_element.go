package main

import (
	"fmt"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// newLegoElementIDCmd shows (or sets/corrects) a part+colour's LEGO element
// id -- the catalog's own value normally comes from Rebrickable's sync, but
// a brand-new set is often checked before the catalog has caught up, and
// the printed element id is right there in the instructions anyway (see
// DB.SetElementID/ElementFor).
func newLegoElementIDCmd() *cobra.Command {
	var clear bool
	var elementID string
	cmd := &cobra.Command{
		Use:     "element-id <part_num> <colour>",
		Short:   "A part+colour's LEGO element id — show it, or set/correct it yourself",
		Example: "  wms lego element-id 3001 red\n  wms lego element-id 3001 red --set 6644777\n  wms lego element-id 3001 red --clear",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			partNum := args[0]
			colors, err := db.CatalogColors()
			if err != nil {
				return err
			}
			c, ok := lego.MatchColor(colors, args[1])
			if !ok {
				return usageError("colour %q doesn't match any known colour", args[1])
			}
			if cmd.Flags().Changed("set") || clear {
				by := cliActor()
				val := elementID
				if clear {
					val = ""
				}
				if err := db.SetElementID(partNum, c.ID, val, by); err != nil {
					return usageError("%s", err)
				}
				if clear {
					say(ui.Status(t, true, fmt.Sprintf("Cleared the element id override for %s (%s) — back to whatever the catalog has, if anything", partNum, c.Name)))
				} else {
					say(ui.Status(t, true, fmt.Sprintf("Set element id %s for %s (%s)", val, partNum, c.Name)))
				}
			}
			id := db.ElementFor(partNum, c.ID)
			if id == "" {
				say(ui.Status(t, false, fmt.Sprintf("No element id known for %s (%s)", partNum, c.Name)))
			} else {
				say(ui.Fact(t, "Element ID", id))
			}
			return emit(map[string]any{"part": partNum, "color_id": c.ID, "color": c.Name, "element_id": id})
		},
	}
	cmd.Flags().StringVar(&elementID, "set", "", "record this element id for this part+colour (digits only)")
	cmd.Flags().BoolVar(&clear, "clear", false, "clear your own override, back to whatever the catalog has")
	return cmd
}
