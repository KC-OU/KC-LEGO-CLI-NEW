package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"
	"golang.org/x/term"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/backup"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/ui"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// passphraseFor finds the backup passphrase without ever taking it as a
// command-line argument (which would land in shell history and `ps`): a file
// (--passphrase-file), the WMS_BACKUP_PASSPHRASE environment variable, or a
// hidden prompt on a terminal.
func passphraseFor(file string, confirmTwice bool) (string, error) {
	if file != "" {
		b, err := os.ReadFile(file)
		if err != nil {
			return "", err
		}
		if p := strings.TrimRight(string(b), "\r\n"); p != "" {
			return p, nil
		}
		return "", fmt.Errorf("%s is empty", file)
	}
	if p := os.Getenv("WMS_BACKUP_PASSPHRASE"); p != "" {
		return p, nil
	}
	if !term.IsTerminal(int(os.Stdin.Fd())) {
		return "", usageError("no passphrase: use --passphrase-file, set WMS_BACKUP_PASSPHRASE, or run on a terminal")
	}
	fmt.Fprint(os.Stderr, "Backup passphrase: ")
	if confirmTwice {
		return readPasswordTwice()
	}
	b, err := term.ReadPassword(int(os.Stdin.Fd()))
	fmt.Fprintln(os.Stderr)
	return string(b), err
}

func newBackupCmd() *cobra.Command {
	var encrypt, dryRun bool
	var passFile string
	cmd := &cobra.Command{
		Use:   "backup",
		Short: "Back up ModernWMS's database via SQLite's online backup API (--encrypt to seal it with a passphrase)",
		Long: "Writes a consistent copy of ModernWMS's database. It holds every user's password hash, so --encrypt seals\n" +
			"it with a passphrase (age format: `age -d file.age` opens it too), proves the sealed copy decrypts, and\n" +
			"only then deletes the plaintext. The passphrase comes from --passphrase-file, the WMS_BACKUP_PASSPHRASE\n" +
			"environment variable or a hidden prompt — never from a command-line argument. Lose it and the backup\n" +
			"is unrecoverable. Restore with `wms backup decrypt`.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			t := ui.New()
			dir := config.Get(config.ModernWMSBackupDir)
			if dryRun {
				return planned("write a ModernWMS backup into "+dir, map[string]any{"directory": dir, "encrypted": encrypt})
			}
			var pass string
			if encrypt { // ask first, so a missing passphrase doesn't cost a backup run
				var err error
				if pass, err = passphraseFor(passFile, true); err != nil {
					return err
				}
			}
			result, err := backup.Run(context.Background(), wmsdb.NewClient(), "")
			if err != nil {
				say(ui.Status(t, false, err.Error()))
				return withCode(exitNetwork, err)
			}
			path, size := result.Path, result.Size
			if encrypt {
				if path, err = backup.Seal(result.Path, pass); err != nil {
					say(ui.Status(t, false, err.Error()))
					return err
				}
				if fi, err := os.Stat(path); err == nil {
					size = fi.Size()
				}
			}
			say(ui.Status(t, true, "Backup created successfully!"))
			say(ui.Fact(t, "File", path))
			say(ui.Fact(t, "Size", fmt.Sprintf("%.2f KB", float64(size)/1024)))
			if encrypt {
				say(ui.Warn(t, "Encrypted. Keep the passphrase somewhere safe: without it this backup cannot be opened."))
			}
			emitHook("backup_done", map[string]any{"kind": "modernwms", "file": path, "bytes": size, "encrypted": encrypt})
			return emit(map[string]any{"file": path, "bytes": size, "encrypted": encrypt})
		},
	}
	cmd.Flags().BoolVar(&encrypt, "encrypt", false, "seal the backup with a passphrase (age)")
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "read the passphrase from this file")
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "show where the backup would go and stop")
	cmd.AddCommand(newBackupDecryptCmd())
	return cmd
}

func newBackupDecryptCmd() *cobra.Command {
	var outPath, passFile string
	cmd := &cobra.Command{
		Use:   "decrypt <backup.db.age>",
		Short: "Open an encrypted backup (never overwrites an existing file)",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			cmd.SilenceUsage = true
			t := ui.New()
			if outPath == "" {
				outPath = backup.PlainName(args[0])
			}
			if _, err := os.Stat(outPath); err == nil {
				return usageError("%s already exists — pick another name with -o", outPath)
			}
			pass, err := passphraseFor(passFile, false)
			if err != nil {
				return err
			}
			if _, err := backup.DecryptFile(args[0], outPath, pass); err != nil {
				if errors.Is(err, os.ErrNotExist) {
					return withCode(exitNotFound, err)
				}
				return withCode(exitAuth, err)
			}
			abs, _ := filepath.Abs(outPath)
			say(ui.Status(t, true, "Decrypted"))
			say(ui.Fact(t, "File", abs))
			return emit(map[string]any{"file": abs})
		},
	}
	cmd.Flags().StringVarP(&outPath, "out", "o", "", "where to write the decrypted database (default: the name without .age)")
	cmd.Flags().StringVar(&passFile, "passphrase-file", "", "read the passphrase from this file")
	return cmd
}
