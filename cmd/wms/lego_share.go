package main

import (
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/exports"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newLegoShareCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "share",
		Short: "A read-only page for someone without the CLI (a wishlist or your collection), viewable until it expires",
	}
	cmd.AddCommand(newLegoShareWishlistCmd(), newLegoShareCollectionCmd())
	return cmd
}

// writeShare saves body, makes a multi-use share link for it, and prints the result —
// the shared logic behind every `lego share` subcommand.
func writeShare(t ui.Theme, kind, what string, body []byte, expires time.Duration) error {
	abs, err := exports.Save(exports.Dir(), cliActor(), kind, "", "html", body)
	if err != nil {
		return err
	}
	if exports.URL("x") == "" {
		return usageError("sharing needs WMS_PUBLIC_URL set, so the link it makes is reachable")
	}
	token, err := exports.NewShareLink(exports.Dir(), abs, cliActor(), expires)
	if err != nil {
		return err
	}
	link := exports.ShareURL(token)
	until := time.Now().Add(expires).Format("2 Jan 2006 15:04")
	say(ui.Status(t, true, fmt.Sprintf("Share link for %s (viewable until %s):", what, until)))
	say(t.Accent.Render(link))
	return emit(map[string]any{"file": abs, "url": link, "expires": until})
}

func newLegoShareWishlistCmd() *cobra.Command {
	var expires time.Duration
	cmd := &cobra.Command{
		Use:     "wishlist",
		Short:   "Share your BrickLink watch list (wms lego watch) as a plain page — for family before a birthday",
		Example: "  wms lego share wishlist --expires 168h   # a week",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			watches, err := db.ListWatches()
			if err != nil {
				return err
			}
			body, err := lego.WishlistHTML(db, "Wishlist", watches)
			if err != nil {
				return err
			}
			return writeShare(t, "wishlist", "your wishlist", body, expires)
		},
	}
	cmd.Flags().DurationVar(&expires, "expires", 7*24*time.Hour, "how long the link stays viewable")
	return cmd
}

func newLegoShareCollectionCmd() *cobra.Command {
	var expires time.Duration
	cmd := &cobra.Command{
		Use:     "collection",
		Short:   "Share your full collection as a read-only page",
		Example: "  wms lego share collection --expires 48h",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			data, err := db.CollectionReport()
			if err != nil {
				return err
			}
			body, err := lego.ReportHTML(data)
			if err != nil {
				return err
			}
			return writeShare(t, "collection", "your collection", body, expires)
		},
	}
	cmd.Flags().DurationVar(&expires, "expires", 7*24*time.Hour, "how long the link stays viewable")
	return cmd
}
