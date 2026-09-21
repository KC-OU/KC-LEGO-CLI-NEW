package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/bricklink"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

// newBLClient builds a configured client that shares lego.db's budget and cache.
func newBLClient(db *lego.DB) *bricklink.Client { return bricklink.FromConfig(db) }

// blFetcher lets the lego package price things through BrickLink without importing it.
type blFetcher struct{ c *bricklink.Client }

func (f blFetcher) AvgPrice(ctx context.Context, itemType, no string, blColor int, cond string) (float64, string, bool, error) {
	pr, err := f.c.PriceGuide(ctx, bricklink.ItemType(itemType), no, blColor, bricklink.Sold, cond)
	switch {
	case errors.Is(err, bricklink.ErrNotFound):
		return 0, "", false, nil
	case err != nil:
		return 0, "", false, err
	case pr.TotalQuantity == 0 || pr.Avg == 0: // no sales in the guide's window: unpriced, not free
		return 0, pr.CurrencyCode, false, nil
	}
	return float64(pr.Avg), pr.CurrencyCode, true, nil
}

// blExit maps BrickLink errors to the documented exit codes.
func blExit(err error) error {
	var coded *exitError
	switch {
	case err == nil:
		return nil
	case errors.As(err, &coded): // already has a deliberate exit code (usage, not found): keep it
		return err
	case errors.Is(err, bricklink.ErrNotConfigured), errors.Is(err, bricklink.ErrAuth), errors.Is(err, bricklink.ErrIPMismatch):
		return withCode(exitAuth, err)
	case errors.Is(err, bricklink.ErrNotFound):
		return withCode(exitNotFound, err)
	case errors.Is(err, bricklink.ErrRateLimited), errors.Is(err, bricklink.ErrBudget):
		return withCode(exitNetwork, err)
	}
	return withCode(exitNetwork, err)
}

func maskValue(s string) string {
	if s == "" {
		return "(not set)"
	}
	if len(s) <= 4 {
		return strings.Repeat("*", len(s))
	}
	return strings.Repeat("*", len(s)-4) + s[len(s)-4:]
}

func parseItemType(s string) (bricklink.ItemType, error) {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "part", "p":
		return bricklink.Part, nil
	case "set", "s":
		return bricklink.Set, nil
	case "minifig", "minifigure", "m":
		return bricklink.Minifig, nil
	}
	return "", usageError("the item type must be part, set or minifig, not %q", s)
}

// blColorFlag turns --color (a name or Rebrickable colour id) or --bl-color into a BrickLink colour number.
func blColorFlag(db *lego.DB, color string, blColor int) (int, error) {
	if blColor > 0 {
		return blColor, nil
	}
	if strings.TrimSpace(color) == "" {
		return 0, nil
	}
	cols, _ := db.CatalogColors()
	c, ok := lego.MatchColor(cols, color)
	if !ok {
		return 0, usageError("no colour %q in the catalog (use a Rebrickable colour name or id, or --bl-color <number>)", color)
	}
	if id, ok := db.BLColorFor(c.ID); ok {
		return id, nil
	}
	return 0, usageError("no BrickLink number is known for %s yet: run `wms bricklink colors sync`, or pass --bl-color <number>", c.Name)
}

func newBricklinkCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "bricklink",
		Short: "Live BrickLink lookups by number, prices and where-used (needs your API credentials)",
		Long: "BrickLink's API looks things up BY NUMBER (parts, sets, minifigures); it has no search by name — use\n" +
			"`wms lego search` for that. It also gives the price guide and which sets contain a part. You need a BrickLink\n" +
			"account and an API consumer registered at https://www.bricklink.com/v2/api/register_consumer.page for THIS\n" +
			"server's public IP address (`wms bricklink whoami` shows it). Then run `wms bricklink configure`.\n" +
			"Calls are capped at a daily budget (default 4,500 of BrickLink's 5,000) and cached.",
	}
	cmd.AddCommand(newBLStatusCmd(), newBLConfigureCmd(), newBLTestCmd(), newBLWhoamiCmd(), newBLItemCmd(), newBLPriceCmd(), newBLWhereUsedCmd(), newBLColorsCmd())
	return cmd
}

func withBL(fn func(ctx context.Context, db *lego.DB, c *bricklink.Client, args []string) error) func(cmd *cobra.Command, args []string) error {
	return func(cmd *cobra.Command, args []string) error {
		cmd.SilenceUsage = true
		db, err := openLego()
		if err != nil {
			return err
		}
		defer db.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
		defer cancel()
		return blExit(fn(ctx, db, newBLClient(db), args))
	}
}

func newBLStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show what is configured and today's call count (never the secrets)",
		Args:  cobra.NoArgs,
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, args []string) error {
			t := ui.New()
			cr := c.Creds
			say(ui.Fact(t, "Consumer key", maskValue(cr.ConsumerKey)))
			say(ui.Fact(t, "Consumer secret", maskValue(cr.ConsumerSecret)))
			say(ui.Fact(t, "Token", maskValue(cr.Token)))
			say(ui.Fact(t, "Token secret", maskValue(cr.TokenSecret)))
			say(ui.Fact(t, "Prices", c.Currency+", region "+orDash(c.Region)))
			used := db.APIUsage("bricklink", time.Now())
			say(ui.Fact(t, "Calls today", fmt.Sprintf("%d of %d (BrickLink allows 5,000)", used, c.DailyBudget)))
			say(ui.Fact(t, "Colour numbers", fmt.Sprintf("%d mapped", db.BLColorCount())))
			if !c.Enabled() {
				say(ui.Warn(t, "Not configured: run `wms bricklink configure`."))
			}
			return emit(map[string]any{"configured": c.Enabled(), "currency": c.Currency, "region": c.Region, "calls_today": used, "daily_budget": c.DailyBudget, "colours_mapped": db.BLColorCount()})
		}),
	}
}

func readSecret(prompt string) (string, error) {
	fmt.Fprint(os.Stderr, prompt)
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return strings.TrimSpace(string(b)), err
}

func newBLConfigureCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "configure",
		Short: "Enter the four BrickLink API values (hidden as you type) and test them",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			if !term.IsTerminal(int(os.Stdin.Fd())) {
				return usageError("configure needs a terminal so the values are not echoed; or set BRICKLINK_CONSUMER_KEY, BRICKLINK_CONSUMER_SECRET, BRICKLINK_TOKEN and BRICKLINK_TOKEN_SECRET in the environment")
			}
			t := ui.New()
			fmt.Fprintln(os.Stderr, "Paste each value from your BrickLink API consumer page (nothing is shown as you type; blank keeps the current value).")
			keys := []struct{ label, key string }{
				{"Consumer key: ", config.BricklinkConsumerKey}, {"Consumer secret: ", config.BricklinkConsumerSecret},
				{"Token value: ", config.BricklinkToken}, {"Token secret: ", config.BricklinkTokenSecret},
			}
			for _, k := range keys {
				v, err := readSecret(k.label)
				if err != nil {
					return err
				}
				if v == "" {
					continue
				}
				if err := config.SetOverride(k.key, v); err != nil {
					return err
				}
			}
			say(ui.Status(t, true, "Saved."))
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			if err := newBLClient(db).Ping(ctx); err != nil {
				say(ui.Status(t, false, err.Error()))
				return blExit(err)
			}
			say(ui.Status(t, true, "BrickLink accepted the credentials."))
			return nil
		},
	}
}

func newBLTestCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "test",
		Short: "Check the credentials and the IP binding with one cheap call",
		Args:  cobra.NoArgs,
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, args []string) error {
			t := ui.New()
			if err := c.Ping(ctx); err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.Status(t, true, "BrickLink accepted the credentials from this server."))
			return emit(map[string]any{"ok": true})
		}),
	}
}

func newBLWhoamiCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "whoami",
		Short: "Show this server's public IP address — the one to register with BrickLink",
		Long:  "Asks api.ipify.org (a public 'what is my IP' service) for the address the internet sees. That is the address BrickLink ties your token to. This is the only command that contacts a service other than Rebrickable or BrickLink.",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			req, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.ipify.org", nil)
			req.Header.Set("User-Agent", "wms-go")
			resp, err := http.DefaultClient.Do(req)
			if err != nil {
				return withCode(exitNetwork, fmt.Errorf("could not find out: %v (from another machine on the same connection, try: curl https://api.ipify.org)", err))
			}
			defer resp.Body.Close()
			b, _ := io.ReadAll(io.LimitReader(resp.Body, 64))
			ip := strings.TrimSpace(string(b))
			if resp.StatusCode != 200 || len(ip) < 7 || strings.ContainsAny(ip, "<> \n") {
				return withCode(exitNetwork, errors.New("the IP service gave an unexpected answer"))
			}
			say(ui.Fact(t, "Public IP", ip))
			say(t.Muted.Render("   Register this address at https://www.bricklink.com/v2/api/register_consumer.page (a static address is safest)."))
			return emit(map[string]any{"ip": ip})
		},
	}
}

func newBLItemCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "item <part|set|minifig> <number>",
		Short:   "Look an item up by its BrickLink number",
		Example: "  wms bricklink item part 3001\n  wms bricklink item set 75192",
		Args:    cobra.ExactArgs(2),
		ValidArgsFunction: func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
			return []string{"part", "set", "minifig"}, cobra.ShellCompDirectiveNoFileComp
		},
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, args []string) error {
			t := ui.New()
			typ, err := parseItemType(args[0])
			if err != nil {
				return err
			}
			it, err := c.GetItem(ctx, typ, args[1])
			if err != nil {
				return err
			}
			say(ui.Fact(t, "Number", it.No))
			say(ui.Fact(t, "Name", it.Name))
			say(ui.Fact(t, "Type", it.Type))
			say(ui.Fact(t, "Year", strconv.Itoa(it.YearReleased)))
			if it.IsObsolete {
				say(ui.Warn(t, "BrickLink marks this item obsolete."))
			}
			say(ui.Fact(t, "Picture", orDash(it.ImageURL)))
			return emit(it)
		}),
	}
}

func newBLPriceCmd() *cobra.Command {
	var color, cond string
	var blColor int
	var stock bool
	cmd := &cobra.Command{
		Use:     "price <part|set> <number>",
		Short:   "BrickLink price guide: what it sold for (default) or is listed for (--stock)",
		Example: "  wms bricklink price part 3001 --color red\n  wms bricklink price set 75192 --condition N",
		Args:    cobra.ExactArgs(2),
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, args []string) error {
			t := ui.New()
			typ, err := parseItemType(args[0])
			if err != nil || typ == bricklink.Minifig {
				return usageError("prices are for a part or a set")
			}
			if cond == "" {
				cond = config.Get(config.BricklinkCondition)
			}
			cond = strings.ToUpper(cond)
			bl, err := blColorFlag(db, color, blColor)
			if err != nil {
				return err
			}
			g := bricklink.Sold
			if stock {
				g = bricklink.Stock
			}
			p, err := c.PriceGuide(ctx, typ, args[1], bl, g, cond)
			if err != nil {
				return err
			}
			label := map[string]string{"N": "new", "U": "used"}[p.NewOrUsed]
			say(ui.Fact(t, "Price guide", fmt.Sprintf("%s, %s (%s)", label, g, p.CurrencyCode)))
			say(ui.Fact(t, "Average", fmt.Sprintf("%.4f", float64(p.Avg))))
			say(ui.Fact(t, "Weighted by quantity", fmt.Sprintf("%.4f", float64(p.QtyAvg))))
			say(ui.Fact(t, "Lowest / highest", fmt.Sprintf("%.4f / %.4f", float64(p.Min), float64(p.Max))))
			say(ui.Fact(t, "Based on", fmt.Sprintf("%d lot(s), %d piece(s)", p.UnitQuantity, p.TotalQuantity)))
			if p.TotalQuantity == 0 {
				say(ui.Warn(t, "No sales or listings in this window: the numbers are zero because there is no data, not because it is free."))
			}
			return emit(p)
		}),
	}
	cmd.Flags().StringVar(&color, "color", "", "colour name or Rebrickable colour id (parts)")
	cmd.Flags().IntVar(&blColor, "bl-color", 0, "BrickLink colour number, instead of --color")
	cmd.Flags().StringVar(&cond, "condition", "", "N (new) or U (used); default from BRICKLINK_CONDITION (U)")
	cmd.Flags().BoolVar(&stock, "stock", false, "prices of current listings instead of past sales")
	return cmd
}

func newBLWhereUsedCmd() *cobra.Command {
	var color string
	var blColor int
	cmd := &cobra.Command{
		Use:   "where-used <part>",
		Short: "Which sets contain a part (BrickLink 'supersets')",
		Args:  cobra.ExactArgs(1),
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, args []string) error {
			t := ui.New()
			bl, err := blColorFlag(db, color, blColor)
			if err != nil {
				return err
			}
			sup, err := c.Supersets(ctx, bricklink.Part, args[0], bl)
			if err != nil {
				return err
			}
			var rows [][]string
			for _, group := range sup {
				for _, e := range group.Entries {
					rows = append(rows, []string{e.Item.No, e.Item.Name, strconv.Itoa(group.ColorID), strconv.Itoa(e.Quantity)})
				}
			}
			if len(rows) == 0 {
				say(ui.Warn(t, "BrickLink lists no sets containing this part."))
			} else {
				shown := rows[:min(len(rows), 40)]
				say(ui.RenderColumns(t, []string{"Set / item", "Name", "BL colour", "Qty"}, shown, fmt.Sprintf("%d place(s) used", len(rows))))
			}
			return emit(sup)
		}),
	}
	cmd.Flags().StringVar(&color, "color", "", "limit to one colour (name or Rebrickable id)")
	cmd.Flags().IntVar(&blColor, "bl-color", 0, "BrickLink colour number")
	return cmd
}

func newBLColorsCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "colors", Short: "BrickLink colour numbers"}
	cmd.AddCommand(&cobra.Command{
		Use:   "sync",
		Short: "Map Rebrickable colours to BrickLink's (one call; needed for prices by colour)",
		Args:  cobra.NoArgs,
		RunE: withBL(func(ctx context.Context, db *lego.DB, c *bricklink.Client, args []string) error {
			t := ui.New()
			cols, err := c.Colors(ctx)
			if err != nil {
				return err
			}
			names := map[int]string{}
			for _, col := range cols {
				names[col.ColorID] = col.ColorName
			}
			rows, unmatched, err := db.MatchBLColorsByName(names)
			if err != nil {
				return err
			}
			if len(rows) == 0 {
				return withCode(exitNotFound, errors.New("no colours matched: load the offline catalog first (`wms lego catalog refresh`)"))
			}
			if err := db.SetBLColors(rows); err != nil {
				return err
			}
			say(ui.Status(t, true, fmt.Sprintf("%d colours mapped, %d BrickLink colours have no match in the catalog.", len(rows), len(unmatched))))
			return emit(map[string]any{"mapped": len(rows), "unmatched": unmatched})
		}),
	})
	return cmd
}
