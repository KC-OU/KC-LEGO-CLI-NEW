package wmsdb

import (
	"context"
	"fmt"
	"strings"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/docker"
)

const containerBackupTmpPath = "/tmp/modernwms_backup.db"

// Backup uses SQLite's online backup API inside the container (safe for a
// live/open database, unlike a naive file copy) then docker-cp's the result
// out to hostDestPath, cleaning up the temp file inside the container
// afterward.
func (c *Client) Backup(ctx context.Context, hostDestPath string) error {
	script := c.connectPrelude() + fmt.Sprintf(`
dst = sqlite3.connect(%s)
con.backup(dst)
dst.close()
con.close()
print("BACKUP_SUCCESS")
`, pyStringLit(containerBackupTmpPath))

	stdout, err := c.RunScript(ctx, script)
	if err != nil {
		return fmt.Errorf("creating database dump inside container: %w", err)
	}
	if !strings.Contains(stdout, "BACKUP_SUCCESS") {
		return fmt.Errorf("backup script did not report success: %s", stdout)
	}

	src := fmt.Sprintf("%s:%s", c.Container, containerBackupTmpPath)
	if err := docker.Cp(ctx, src, hostDestPath); err != nil {
		return fmt.Errorf("copying backup file to host: %w", err)
	}

	_ = docker.Rm(ctx, c.Container, containerBackupTmpPath)
	return nil
}
