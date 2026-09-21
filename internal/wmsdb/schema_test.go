package wmsdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

var (
	pyExecRE    = regexp.MustCompile(`(?s)(?:cur|con)\.execute\(\s*(?:"""(.*?)"""|"([^"\n]*)")`)
	namedRowsRE = regexp.MustCompile(`namedRows\(ctx,\s*"([^"\n]*)"`)
	fmtVerbRE   = regexp.MustCompile(`%[sdv]`)
)

// TestSQLMatchesModernWMSSchema compiles (EXPLAIN — nothing runs) every SQL
// statement this program sends to ModernWMS against the real database
// structure in testdata/wms_schema.sql. It exists because a query referencing
// a column that isn't there (dispatchlist.is_valid) only surfaced as a
// Python traceback on the Overview screen: the SQL lives inside generated
// Python scripts, so the compiler can't see it. Docker isn't needed.
//
// When ModernWMS is upgraded, refresh the fixture from the live database.
func TestSQLMatchesModernWMSSchema(t *testing.T) {
	ddl, err := os.ReadFile(filepath.Join("testdata", "wms_schema.sql"))
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	db.SetMaxOpenConns(1) // ":memory:" is per-connection
	if _, err := db.Exec(string(ddl)); err != nil {
		t.Fatalf("loading the schema fixture: %v", err)
	}

	files, _ := filepath.Glob("*.go")
	syncFiles, _ := filepath.Glob(filepath.Join("..", "sync", "*.go"))
	checked := 0
	for _, path := range append(files, syncFiles...) {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		var stmts []string
		for _, m := range pyExecRE.FindAllStringSubmatch(string(src), -1) {
			stmts = append(stmts, m[1]+m[2])
		}
		for _, m := range namedRowsRE.FindAllStringSubmatch(string(src), -1) {
			stmts = append(stmts, m[1])
		}
		for _, s := range stmts {
			s = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(fmtVerbRE.ReplaceAllString(s, "1")), ";"))
			args := make([]any, strings.Count(s, "?"))
			rows, err := db.Query("EXPLAIN "+s, args...)
			if err != nil {
				t.Errorf("%s: %v\n    %s", path, err, strings.Join(strings.Fields(s), " "))
				continue
			}
			rows.Close()
			checked++
		}
	}
	// Guards the extractor itself: if a refactor changes how scripts are
	// written and this stops finding statements, the test must not pass empty.
	if checked < 50 {
		t.Fatalf("only %d statements were extracted and checked; the extraction patterns need updating", checked)
	}
	t.Logf("%d statements compile against the ModernWMS schema", checked)
}
