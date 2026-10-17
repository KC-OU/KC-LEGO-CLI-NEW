package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/audit"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/notify"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func timeNowDate() string { return time.Now().Format("2006-01-02") }

// newAttendanceCmd is clock-in/out and the rota: who's scheduled, who's
// actually on the clock right now, and the admin override for emergency
// cover. Run from a shell by whoever's at the desk (there's no CLI login
// session the way the TUI/mobile have one), so every command takes the
// username explicitly rather than assuming "whoever's running this".
func newAttendanceCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "attendance", Short: "Clock in/out and the rota — see docs/guides/assigned-work.md"}
	cmd.AddCommand(newAttendanceClockInCmd(), newAttendanceClockOutCmd(), newAttendanceStatusCmd(), newAttendanceRotaCmd(),
		newAttendanceBreakStartCmd(), newAttendanceBreakEndCmd(), newAttendanceGateCmd(),
		newAttendanceCardScanCmd(), newAttendanceCardCmd())
	return cmd
}

// newAttendanceGateCmd is the on/off switch for the whole clock-in/rota
// system (config.RequireClockIn) — every piece of it (RequireClockedIn's
// gate on claiming work, NoShowCandidates' alerting, the TUI's "My accuracy"
// rota section) already no-ops cleanly while this is off, so flipping it is
// the one decision that turns the feature on or off, reversible at any time
// without redeploying: `wms attendance gate on|off|status`.
func newAttendanceGateCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "gate [on|off|status]",
		Short:     "Turn the clock-in requirement on or off (default: show current state)",
		Example:   "  wms attendance gate on\n  wms attendance gate off\n  wms attendance gate",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"on", "off", "status"},
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			action := "status"
			if len(args) > 0 {
				action = args[0]
			}
			switch action {
			case "on", "off":
				val := map[string]string{"on": "1", "off": "0"}[action]
				if err := config.SetOverride(config.RequireClockIn, val); err != nil {
					return err
				}
				_ = audit.New().Log(cliUser(), "", "ATTENDANCE_GATE_SET", "SUCCESS", action)
			case "status":
			default:
				return usageError("unknown argument %q: expected on, off, or nothing", action)
			}
			on := config.Get(config.RequireClockIn) == "1"
			say(ui.Status(t, on, "Clock-in requirement is "+map[bool]string{true: "ON", false: "OFF"}[on]))
			if on && action == "on" {
				say(ui.Warn(t, "Make sure today's rota is populated first (wms attendance rota) — RequireClockedIn blocks claiming work for anyone with no rota entry at all."))
			}
			return emit(map[string]any{"clock_in_required": on})
		},
	}
}

func newAttendanceClockInCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "clock-in <username>",
		Short:   "Start a shift",
		Example: "  wms attendance clock-in dave",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			ev, err := db.ClockIn(args[0])
			if err != nil {
				switch {
				case errors.Is(err, lego.ErrAlreadyClockedIn):
					return usageError("%s is already clocked in", args[0])
				case errors.Is(err, lego.ErrNotScheduled):
					return usageError("%s isn't on today's rota — run: wms attendance rota override %s", args[0], args[0])
				}
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%s clocked in at %s", args[0], ev.ClockInAt.Format("15:04"))))
			return emit(map[string]any{"username": args[0], "clocked_in_at": ev.ClockInAt})
		},
	}
}

func newAttendanceClockOutCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "clock-out <username>",
		Short:   "End a shift",
		Example: "  wms attendance clock-out dave",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.ClockOut(args[0]); err != nil {
				if errors.Is(err, lego.ErrNotClockedIn) {
					return usageError("%s isn't clocked in", args[0])
				}
				return err
			}
			say(ui.Status(ui.New(), true, args[0]+" clocked out"))
			warnIfStillHoldingATicket(db, args[0])
			return emit(map[string]any{"username": args[0]})
		},
	}
}

func newAttendanceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "status <username>",
		Short:   "Clocked in? Scheduled today?",
		Example: "  wms attendance status dave",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			t := ui.New()
			shift, err := db.CurrentShift(args[0])
			if err != nil {
				return err
			}
			if shift != nil {
				say(ui.Fact(t, "Clocked in", "since "+shift.ClockInAt.Format("15:04")))
				if on, err := db.OnBreak(args[0]); err == nil && on {
					say(ui.Fact(t, "On a break", "yes"))
				}
				if c, err := db.ActiveStatusCard(args[0]); err == nil && c != nil {
					say(ui.Fact(t, "Card", lego.CardReasonName(c.Reason)))
				}
			} else {
				say(ui.Fact(t, "Clocked in", "no"))
			}
			today := timeNowDate()
			rota, err := db.GetRota(args[0], today)
			if err != nil {
				return err
			}
			switch {
			case rota == nil:
				say(ui.Fact(t, "Scheduled today", "no (NS)"))
			case rota.EmergencyOverride:
				say(ui.Fact(t, "Scheduled today", "emergency cover only"))
			case rota.StartTime != "" || rota.EndTime != "":
				say(ui.Fact(t, "Scheduled today", rota.StartTime+" - "+rota.EndTime))
			default:
				say(ui.Fact(t, "Scheduled today", "yes"))
			}
			return emit(map[string]any{"username": args[0], "clocked_in": shift != nil, "rota": rota})
		},
	}
}

func newAttendanceRotaCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "rota", Short: "Who's scheduled when"}

	var note string
	set := &cobra.Command{
		Use:     "set <username> <date YYYY-MM-DD> [start] [end]",
		Short:   "Schedule someone to work a day (updates any existing entry for that day)",
		Example: "  wms attendance rota set dave 2026-10-10 09:00 17:00",
		Args:    cobra.RangeArgs(2, 4),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			start, end := "", ""
			if len(args) > 2 {
				start = args[2]
			}
			if len(args) > 3 {
				end = args[3]
			}
			if err := db.SetRota(args[0], args[1], start, end, note, cliActor()); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%s scheduled on %s", args[0], args[1])))
			return emit(map[string]any{"username": args[0], "date": args[1], "start": start, "end": end})
		},
	}
	set.Flags().StringVar(&note, "note", "", "a short note shown alongside the entry")
	cmd.AddCommand(set)

	cmd.AddCommand(&cobra.Command{
		Use:     "clear <username> <date YYYY-MM-DD>",
		Short:   "Remove someone's schedule for a day",
		Example: "  wms attendance rota clear dave 2026-10-10",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.ClearRota(args[0], args[1]); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%s's schedule for %s cleared", args[0], args[1])))
			return emit(map[string]any{"username": args[0], "date": args[1]})
		},
	})

	var rotaShowWatch time.Duration
	rotaShow := &cobra.Command{
		Use:     "show [date YYYY-MM-DD]",
		Short:   "Who's scheduled on a day (default: today)",
		Example: "  wms attendance rota show\n  wms attendance rota show --watch 30s",
		Args:    cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			date := timeNowDate()
			if len(args) == 1 {
				date = args[0]
			}
			return runWatchable(rotaShowWatch, func() error {
				db, err := openLego()
				if err != nil {
					return err
				}
				defer db.Close()
				entries, err := db.RotaForDate(date)
				if err != nil {
					return err
				}
				if len(entries) == 0 {
					say("Nobody scheduled on " + date + ".")
					return emit(map[string]any{"date": date, "entries": entries})
				}
				t := ui.New()
				for _, e := range entries {
					when := e.StartTime + " - " + e.EndTime
					if e.StartTime == "" && e.EndTime == "" {
						when = "(no times given)"
					}
					if e.EmergencyOverride {
						when += "  [emergency cover]"
					}
					say(ui.Fact(t, e.Username, when))
				}
				return emit(map[string]any{"date": date, "entries": entries})
			})
		},
	}
	addWatchFlag(rotaShow, &rotaShowWatch)
	cmd.AddCommand(rotaShow)

	cmd.AddCommand(&cobra.Command{
		Use:     "override <username> [date YYYY-MM-DD]",
		Short:   "Quick NS override: let someone work a day they're not on the rota for (default: today)",
		Example: "  wms attendance rota override dave\n  wms attendance rota override dave 2026-10-10",
		Args:    cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			date := timeNowDate()
			if len(args) == 2 {
				date = args[1]
			}
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.QuickNSOverride(args[0], date, cliActor()); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%s covered for %s (emergency cover, not a planned shift)", args[0], date)))
			return emit(map[string]any{"username": args[0], "date": date})
		},
	})

	return cmd
}

// warnIfStillHoldingATicket is the "clocked-out-but-still-holding-a-ticket"
// alert: best effort, never fails the clock-out itself over it.
func warnIfStillHoldingATicket(db *lego.DB, username string) {
	t, err := db.CurrentTicket(username)
	if err != nil || t == nil {
		return
	}
	label := t.Label
	if label == "" {
		label = t.Target
	}
	say(ui.New().Warning.Render(fmt.Sprintf("Note: %s is still holding an open %s ticket (%s) — it wasn't released.", username, t.Kind, label)))
	notify.Dispatch("attendance_clocked_out_with_ticket",
		fmt.Sprintf("%s clocked out while still holding a ticket", username),
		fmt.Sprintf("%s clocked out but is still assigned an open %s ticket: %s. It wasn't released — reassign it or check in with them.", username, t.Kind, label))
}

func newAttendanceBreakStartCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "break-start <username>",
		Short:   "Start a paid break (stays clocked in; pauses pick/check work while it's on)",
		Example: "  wms attendance break-start dave",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.StartBreak(args[0]); err != nil {
				switch {
				case errors.Is(err, lego.ErrNotClockedIn):
					return usageError("%s isn't clocked in", args[0])
				case errors.Is(err, lego.ErrAlreadyOnBreak):
					return usageError("%s is already on a break", args[0])
				}
				return err
			}
			say(ui.Status(ui.New(), true, args[0]+" is now on a break"))
			return emit(map[string]any{"username": args[0]})
		},
	}
}

func newAttendanceBreakEndCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "break-end <username>",
		Short:   "End a paid break",
		Example: "  wms attendance break-end dave",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.EndBreak(args[0]); err != nil {
				switch {
				case errors.Is(err, lego.ErrNotClockedIn):
					return usageError("%s isn't clocked in", args[0])
				case errors.Is(err, lego.ErrNotOnBreak):
					return usageError("%s isn't on a break", args[0])
				}
				return err
			}
			say(ui.Status(ui.New(), true, args[0]+"'s break has ended"))
			return emit(map[string]any{"username": args[0]})
		},
	}
}

// newAttendanceCardScanCmd is the self-serve half of status cards: scanning
// the one printed "break card" toggles the scanning user's own break on/off
// (see DB.ScanStatusCard) -- no admin, no menu navigation, just a scan.
// Delay/No Work have no scan code; they're admin-applied (see
// newAttendanceCardCmd below).
func newAttendanceCardScanCmd() *cobra.Command {
	return &cobra.Command{
		Use:     "card-scan <username> <code>",
		Short:   "Scan a status card (today: the break card) to toggle it for yourself",
		Example: "  wms attendance card-scan dave CARD-BREAK",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			label, err := db.ScanStatusCard(args[0], args[1])
			if err != nil {
				return usageError("%v", err)
			}
			say(ui.Status(ui.New(), true, args[0]+": "+label))
			return emit(map[string]any{"username": args[0], "result": label})
		},
	}
}

// newAttendanceCardCmd is the admin-applied half of status cards: Delay and
// No Work Available, set on someone's behalf (never self-scanned, unlike
// the break card) -- a quick, visible "why this person looks idle right
// now" instead of a supervisor having to guess or get nagged by the
// idle-ticket nudge about something already explained. See status_cards.go.
func newAttendanceCardCmd() *cobra.Command {
	cmd := &cobra.Command{Use: "card", Short: "Delay / No Work Available cards — admin-applied, suppress the idle-ticket nudge while active"}

	var note string
	set := &cobra.Command{
		Use:     "set <username> <delay|no_work>",
		Short:   "Put a card on someone, closing any card they already have",
		Example: "  wms attendance card set dave delay --note \"waiting on a parts delivery\"",
		Args:    cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			c, err := db.SetStatusCard(args[0], args[1], note, cliActor())
			if err != nil {
				return usageError("%v", err)
			}
			say(ui.Status(ui.New(), true, fmt.Sprintf("%s: %s", args[0], lego.CardReasonName(c.Reason))))
			return emit(c)
		},
	}
	set.Flags().StringVar(&note, "note", "", "a short note shown alongside the card")
	cmd.AddCommand(set)

	cmd.AddCommand(&cobra.Command{
		Use:     "clear <username>",
		Short:   "Clear someone's active card, if any",
		Example: "  wms attendance card clear dave",
		Args:    cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			if err := db.ClearStatusCard(args[0], cliActor()); err != nil {
				return err
			}
			say(ui.Status(ui.New(), true, args[0]+"'s card cleared"))
			return emit(map[string]any{"username": args[0]})
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use: "list", Short: "Everyone with a card on right now", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			cards, err := db.ActiveStatusCards()
			if err != nil {
				return err
			}
			t := ui.New()
			for _, c := range cards {
				note := ""
				if c.Note != "" {
					note = " — " + c.Note
				}
				say(ui.Fact(t, c.Username, lego.CardReasonName(c.Reason)+note))
			}
			if len(cards) == 0 {
				say(ui.Status(t, true, "Nobody has a card on right now."))
			}
			return emit(cards)
		},
	})
	return cmd
}
