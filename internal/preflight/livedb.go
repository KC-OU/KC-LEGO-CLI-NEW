package preflight

import (
	"context"
	"database/sql"
	"net/url"
)

// quickCheck opens a SQLite file read-only and returns the result of PRAGMA quick_check ("ok" when
// the file is sound). Nothing is written, and no lock beyond a read is taken.
func quickCheck(ctx context.Context, path string) (string, error) {
	u := url.URL{Scheme: "file", Path: path, RawQuery: "mode=ro&_pragma=busy_timeout(5000)"}
	db, err := sql.Open("sqlite", u.String())
	if err != nil {
		return "", err
	}
	defer db.Close()
	var res string
	if err := db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&res); err != nil {
		return "", err
	}
	return res, nil
}
