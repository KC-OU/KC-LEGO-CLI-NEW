package main

import (
	"fmt"
	"strings"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/labels"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newLegoBagSizeCmd() *cobra.Command {
	return &cobra.Command{
		Use:               "bag-size <part_num> [small|main]",
		Short:             "Which bag a part goes in while picking/checking: easy-to-lose small parts, or the main bag (default)",
		Example:           "  wms lego bag-size 3024\n  wms lego bag-size 3024 small\n  wms lego bag-size 3024 main",
		Args:              cobra.RangeArgs(1, 2),
		ValidArgsFunction: completePartNum,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			partNum := args[0]
			if len(args) == 1 {
				size, err := db.PartBagSize(partNum)
				if err != nil {
					return err
				}
				say(fmt.Sprintf("%s is currently in the %s bag.", partNum, size))
				return emit(map[string]any{"part": partNum, "size": size})
			}
			size := args[1]
			if size != lego.BagSmall && size != lego.BagMain {
				return usageError("say `small` or `main`")
			}
			if err := db.SetBagSize(partNum, size); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%s is now in the %s bag.", partNum, size)))
			return emit(map[string]any{"part": partNum, "size": size})
		},
	}
}

func newLegoSmallBagLabelCmd() *cobra.Command {
	var size, format, outPath string
	cmd := &cobra.Command{
		Use:   "small-bag-label <set> [bag-code]",
		Short: "A label for the set's small-parts bag — the set number, name, and (if given) the bag's own barcode",
		Long: "Reuses the same label layout as `wms lego labels`, with the location line replaced by \"SMALL PARTS\".\n" +
			"Giving a bag code encodes it as the label's own barcode, so the exact same code this label carries is what\n" +
			"gets scanned back in at `wms lego check` finish time to confirm the bag (see docs/guides/set-checks.md).\n" +
			"Defaults to a small sticky-label size (50x30), not the full set-label sheet size.",
		Example: "  wms lego small-bag-label 75192\n  wms lego small-bag-label 75192 BAG-0042 -o bag.pdf",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			s, err := labels.SizeByID(size)
			if err != nil {
				return usageError("%v", err)
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			d := db.LabelData(args[0])
			d.Location = "SMALL PARTS"
			if len(args) == 2 {
				d.Barcode = args[1]
			}
			var body []byte
			switch strings.ToLower(format) {
			case "pdf":
				body = labels.PDF([]labels.Data{d}, s)
			case "html":
				body = labels.HTML([]labels.Data{d}, s)
			case "png":
				if body, err = labels.PNG([]labels.Data{d}, s); err != nil {
					return usageError("%v", err)
				}
			case "zpl":
				if body, err = labels.ZPL([]labels.Data{d}, s); err != nil {
					return usageError("%v", err)
				}
			default:
				return usageError("--format must be pdf, html, png or zpl")
			}
			if outPath == "" {
				_, _ = cmd.OutOrStdout().Write(body)
				return nil
			}
			abs, err := writeExportFile(outPath, body, true)
			if err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, "Wrote the small-bag label to "+abs))
			return emit(map[string]any{"file": abs, "set": d.SetNum})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "output file (default: print it)")
	cmd.Flags().StringVar(&size, "size", "50x30", "label size — see wms lego labels --help")
	cmd.Flags().StringVar(&format, "format", "pdf", "pdf, html, png or zpl")
	return cmd
}
