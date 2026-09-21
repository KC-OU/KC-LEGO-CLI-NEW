// Package partdb reads and writes Part-DB's SQLite file directly — it is
// bind-mounted on the host, unlike ModernWMS's, so no docker-exec bridge is
// needed here.
package partdb

import (
	"database/sql"
	"fmt"

	_ "modernc.org/sqlite"

	"github.com/KC-OU/KC-LEGO-CLI-NEW/internal/config"
)

type DB struct {
	*sql.DB
}

func Open(path string) (*DB, error) {
	if path == "" {
		path = config.Get(config.PartDBDBPath)
	}
	// Part-DB itself (PHP) writes to this file, so wait a moment for its lock instead of failing.
	sqlDB, err := sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)")
	if err != nil {
		return nil, fmt.Errorf("opening partdb sqlite file %s: %w", path, err)
	}
	if err := sqlDB.Ping(); err != nil {
		return nil, fmt.Errorf("connecting to partdb sqlite file %s: %w", path, err)
	}
	return &DB{sqlDB}, nil
}
