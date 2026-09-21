package main

import (
	"bufio"
	"fmt"
	"os"
	"strings"

	"github.com/mdp/qrterminal/v3"
	"github.com/spf13/cobra"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/twofa"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
)

func newUsers2FACmd() *cobra.Command {
	cmd := &cobra.Command{Use: "2fa", Short: "Manage TOTP two-factor authentication for TUI logins"}
	cmd.AddCommand(newUsers2FAEnableCmd(), newUsers2FADisableCmd(), newUsers2FAUnlockCmd(), newUsers2FAStatusCmd())
	return cmd
}

func newUsers2FAEnableCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:               "enable <user>",
		Short:             "Enroll a user in TOTP 2FA (opt-in, off by default)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeUser(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			username := args[0]

			secret, uri, err := twofa.Enroll(username, source)
			if err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}

			say(ui.Fact(t, "Account", username+" ("+source+")"))
			say(ui.Fact(t, "Manual entry secret (if you can't scan)", secret))
			fmt.Println()
			qrterminal.GenerateHalfBlock(uri, qrterminal.L, os.Stdout)
			fmt.Println()
			fmt.Println("Scan this with any TOTP app (Google Authenticator, Authy, 1Password, ...), then enter the 6-digit code it shows:")

			reader := bufio.NewReader(os.Stdin)
			fmt.Print("Code: ")
			code, _ := reader.ReadString('\n')
			code = strings.TrimSpace(code)

			backupCodes, err := twofa.Confirm(username, source, code)
			if err != nil {
				say(ui.Status(t, false, "confirmation failed: "+err.Error()))
				return err
			}

			say(ui.Status(t, true, "2FA enabled for "+username))
			say(ui.Warn(t, "Save these backup codes now — they will not be shown again. Each works once if you lose your device:"))
			for _, c := range backupCodes {
				fmt.Println("  " + c)
			}
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "modernwms", "account source: modernwms or partdb")
	return cmd
}

func newUsers2FADisableCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:               "disable <user>",
		Short:             "Remove TOTP 2FA from a user",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeUser(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			username := args[0]
			if err := twofa.Disable(username, source); err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.Status(t, true, "2FA disabled for "+username))
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "modernwms", "account source: modernwms or partdb")
	return cmd
}

func newUsers2FAUnlockCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:               "unlock <user>",
		Short:             "Clear a 2FA lockout (5 wrong codes in a row lock the account for 5 minutes, doubling each time)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeUser(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			if err := twofa.Unlock(args[0], source); err != nil {
				say(ui.Status(t, false, err.Error()))
				return err
			}
			say(ui.Status(t, true, "2FA lockout cleared for "+args[0]))
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "modernwms", "account source: modernwms or partdb")
	return cmd
}

func newUsers2FAStatusCmd() *cobra.Command {
	var source string
	cmd := &cobra.Command{
		Use:               "status <user>",
		Short:             "Show whether a user has TOTP 2FA enabled",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: completeUser(0),
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			username := args[0]
			enabled := twofa.IsEnabled(username, source)
			label := "DISABLED"
			if enabled {
				label = fmt.Sprintf("ENABLED (%d backup codes remaining)", twofa.RemainingBackupCodes(username, source))
				if until, locked := twofa.Locked(username, source); locked {
					label += " — LOCKED until " + until.Format("15:04:05")
				}
			}
			say(ui.Fact(t, username+" ("+source+")", label))
			return nil
		},
	}
	cmd.Flags().StringVar(&source, "source", "modernwms", "account source: modernwms or partdb")
	return cmd
}
