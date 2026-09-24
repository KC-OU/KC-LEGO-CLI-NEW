package main

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// WMS_ALIASES_FILE (default ~/.config/wms-go/aliases): one "name = full command"
// line per shortcut, expanded before cobra ever sees argv — a personal shortcut
// for a long command you type often, without needing a shell alias.
//
//	r = lego report missing
//
// Only the first word is checked, and only once (no recursive/chained aliases,
// so a typo in the aliases file can't loop) — everything after it on the real
// command line is appended as-is.
func expandAlias(args []string) []string {
	if len(args) == 0 {
		return args
	}
	full, ok := loadAliases()[args[0]]
	if !ok {
		return args
	}
	return append(strings.Fields(full), args[1:]...)
}

func aliasesFilePath() string {
	if p := os.Getenv("WMS_ALIASES_FILE"); p != "" {
		return p
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "wms-go", "aliases")
}

func loadAliases() map[string]string {
	path := aliasesFilePath()
	if path == "" {
		return nil
	}
	f, err := os.Open(path)
	if err != nil {
		return nil
	}
	defer f.Close()
	m := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, cmd, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		if name, cmd := strings.TrimSpace(name), strings.TrimSpace(cmd); name != "" && cmd != "" {
			m[name] = cmd
		}
	}
	return m
}
