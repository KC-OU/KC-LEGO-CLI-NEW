package main

import (
	"errors"
	"fmt"
	"time"

	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/lego"
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
	cmd.AddCommand(newAttendanceClockInCmd(), newAttendanceClockOutCmd(), newAttendanceStatusCmd(), newAttendanceRotaCmd())
	return cmd
}

func newAttendanceClockInCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "clock-in <username>",
		Short: "Start a shift",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			db, err := openLego()
			if err != nil {
				return err
			}
			defer db.Close()
			ev, err := db.ClockIn(args[0])
			if err != nil {
				if errors.Is(err, lego.ErrAlreadyClockedIn) {
					return usageError("%s is already clocked in", args[0])
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
		Use:   "clock-out <username>",
		Short: "End a shift",
		Args:  cobra.ExactArgs(1),
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
			return emit(map[string]any{"username": args[0]})
		},
	}
}

func newAttendanceStatusCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "status <username>",
		Short: "Clocked in? Scheduled today?",
		Args:  cobra.ExactArgs(1),
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
		Use:   "set <username> <date YYYY-MM-DD> [start] [end]",
		Short: "Schedule someone to work a day (updates any existing entry for that day)",
		Args:  cobra.RangeArgs(2, 4),
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
		Use:   "clear <username> <date YYYY-MM-DD>",
		Short: "Remove someone's schedule for a day",
		Args:  cobra.ExactArgs(2),
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

	cmd.AddCommand(&cobra.Command{
		Use:   "show [date YYYY-MM-DD]",
		Short: "Who's scheduled on a day (default: today)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			date := timeNowDate()
			if len(args) == 1 {
				date = args[0]
			}
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
		},
	})

	cmd.AddCommand(&cobra.Command{
		Use:   "override <username> [date YYYY-MM-DD]",
		Short: "Quick NS override: let someone work a day they're not on the rota for (default: today)",
		Args:  cobra.RangeArgs(1, 2),
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
