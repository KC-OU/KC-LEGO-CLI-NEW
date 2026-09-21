package uiapp

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSavingAPartTellsEnabledPluginsAndAuditsTheRun(t *testing.T) {
	app, _ := flowApp(t) // hooks run in the foreground in tests
	dir := os.Getenv("WMS_PLUGIN_DIR")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	for p := dir; strings.HasPrefix(p, os.TempDir()) && p != os.TempDir(); p = filepath.Dir(p) {
		os.Chmod(p, 0o755)
	}
	log := filepath.Join(t.TempDir(), "events.log")
	os.Chmod(filepath.Dir(log), 0o777)
	script := "#!/bin/bash\nif [[ \"$1\" == hook ]]; then cat >> " + log + "; echo >> " + log + "; fi\n"
	if err := os.WriteFile(filepath.Join(dir, "wms-watcher"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := app.plugins.Enable("watcher", []string{"part_added", "part_updated"}, ""); err != nil {
		t.Fatal(err)
	}

	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "5", cfConfirm: "yes"})
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "9", cfConfirm: "yes"})

	b, _ := os.ReadFile(log)
	events := string(b)
	if !strings.Contains(events, `"event":"part_added"`) || !strings.Contains(events, `"event":"part_updated"`) ||
		!strings.Contains(events, `"part":"3001"`) || !strings.Contains(events, `"qty":9`) || !strings.Contains(events, `"was":5`) {
		t.Errorf("the plugin should have seen an add then an update:\n%s", events)
	}
	if a := auditText(t, app); !strings.Contains(a, "ACTION:PLUGIN_HOOK | STATUS:SUCCESS") {
		t.Errorf("hook runs are audited:\n%s", a)
	}
	// an app with no plugin folder is unaffected
	os.RemoveAll(dir)
	enter(t, app, scrLegoPartAdd, "3001")
	pickText(t, app, scrPick, "Red")
	confirm(app, scrPartConfirm, map[int]string{cfQty: "10", cfConfirm: "yes"})
	if app.messageErr {
		t.Errorf("a missing plugin folder must not disturb saving: %q", app.message)
	}
}
