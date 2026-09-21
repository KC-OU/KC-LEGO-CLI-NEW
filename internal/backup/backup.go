// Package backup is the thin CLI-facing wrapper around wmsdb.Client.Backup:
// it names the destination file and stats the result.
package backup

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/wmsdb"
)

// StaleAfter is how old the newest backup may be before `wms doctor` and the alerts call it a failure.
const StaleAfter = 7 * 24 * time.Hour

type Result struct {
	Path string
	Size int64
}

func Run(ctx context.Context, client *wmsdb.Client, backupDir string) (*Result, error) {
	if backupDir == "" {
		backupDir = config.Get(config.ModernWMSBackupDir)
	}
	if err := os.MkdirAll(backupDir, 0700); err != nil {
		return nil, fmt.Errorf("creating backup dir: %w", err)
	}

	dest := filepath.Join(backupDir, fmt.Sprintf("modernwms_db_%s.db", time.Now().Format("20060102_150405")))
	if err := client.Backup(ctx, dest); err != nil {
		return nil, err
	}
	// The dumped DB contains user tables (password hashes, etc.) — don't
	// leave it world-readable at the umask's mercy like the source data.
	if err := os.Chmod(dest, 0600); err != nil {
		return nil, fmt.Errorf("securing backup file permissions: %w", err)
	}

	info, err := os.Stat(dest)
	if err != nil {
		return nil, fmt.Errorf("backup file missing after copy: %w", err)
	}
	return &Result{Path: dest, Size: info.Size()}, nil
}

// Newest is the most recent ModernWMS backup in dir (sealed or not): its file
// name and modification time, ok=false when there is none.
func Newest(dir string) (name string, mod time.Time, ok bool) {
	matches, _ := filepath.Glob(filepath.Join(dir, "modernwms_db_*"))
	for _, m := range matches {
		if strings.HasSuffix(m, EncryptedSuffix+".verify") {
			continue
		}
		if fi, err := os.Stat(m); err == nil && fi.ModTime().After(mod) {
			name, mod, ok = filepath.Base(m), fi.ModTime(), true
		}
	}
	return
}
