package preflight

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// genericPatterns are shapes that should never be in a public repository. Machine-specific strings
// (your own domain, e-mail address, IP) are NOT listed here, because listing them would publish them:
// they come from the private PIIPatterns file.
var genericPatterns = []struct {
	Name string
	RE   *regexp.Regexp
}{
	{"private key block", regexp.MustCompile(`-----BEGIN [A-Z ]*PRIVATE KEY-----`)},
	{"GitHub token", regexp.MustCompile(`\bgh[pousr]_[A-Za-z0-9]{20,}`)},
	{"Slack token", regexp.MustCompile(`\bxox[abprs]-[A-Za-z0-9-]{10,}`)},
	{"AWS access key", regexp.MustCompile(`\bAKIA[0-9A-Z]{16}\b`)},
	{"Google API key", regexp.MustCompile(`\bAIza[0-9A-Za-z_-]{35}\b`)},
	{"credential in a URL", regexp.MustCompile(`://[^/\s:@]+:[^/\s@]{6,}@[A-Za-z0-9.-]+`)},
	{"hard-coded secret assignment", regexp.MustCompile(`(?i)\b(password|passwd|secret|api[_-]?key|token)\b\s*[:=]\s*["'][A-Za-z0-9+/_=-]{20,}["']`)},
}

// Secret is a value that must not appear anywhere in the tree. Origin says where it came from
// ("settings.json:REBRICKABLE_API_KEY"); the value itself is never printed.
type Secret struct {
	Origin string
	Value  string
}

// Finding is one hit. It carries the file and line but never the matched text.
type Finding struct {
	Path string
	Line int
	What string
}

func (f Finding) String() string { return fmt.Sprintf("%s:%d (%s)", f.Path, f.Line, f.What) }

// LoadSecrets reads the values that would be damaging in a public repository from the machine's own
// files: every string in settings.json and 2fa.json and every value in the deploy values file.
// Short values (under 8 characters) are ignored: they would match ordinary words.
func LoadSecrets(settings, twofa, deployValues string) []Secret {
	var out []Secret
	seen := map[string]bool{}
	add := func(origin, v string) {
		v = strings.TrimSpace(v)
		if len(v) < 8 || seen[v] {
			return
		}
		seen[v] = true
		out = append(out, Secret{Origin: origin, Value: v})
	}
	for _, f := range []struct{ path, name string }{{settings, "settings.json"}, {twofa, "2fa.json"}} {
		b, err := os.ReadFile(f.path)
		if err != nil {
			continue
		}
		var doc any
		if json.Unmarshal(b, &doc) == nil {
			walkStrings(doc, f.name, add)
		}
	}
	if fh, err := os.Open(deployValues); err == nil {
		defer fh.Close()
		sc := bufio.NewScanner(fh)
		for sc.Scan() {
			line := strings.TrimSpace(sc.Text())
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if k, v, ok := strings.Cut(line, "="); ok {
				add("deploy values:"+strings.TrimSpace(k), strings.Trim(strings.TrimSpace(v), `"'`))
			}
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Origin < out[j].Origin })
	return out
}

func walkStrings(v any, path string, add func(origin, v string)) {
	switch t := v.(type) {
	case string:
		add(path, t)
	case map[string]any:
		for k, x := range t {
			walkStrings(x, path+":"+k, add)
		}
	case []any:
		for _, x := range t {
			walkStrings(x, path, add)
		}
	}
}

// LoadPII reads the private list of regular expressions (one per line, # for comments).
func LoadPII(path string) ([]*regexp.Regexp, []string) {
	fh, err := os.Open(path)
	if err != nil {
		return nil, nil
	}
	defer fh.Close()
	var res []*regexp.Regexp
	var bad []string
	sc := bufio.NewScanner(fh)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		re, err := regexp.Compile("(?i)" + line)
		if err != nil {
			bad = append(bad, line)
			continue
		}
		res = append(res, re)
	}
	return res, bad
}

const maxScanBytes = 2 << 20

// ScanTree looks through every text file under root. Files over 2 MB and binary files are not read
// (the size check reports those separately).
func ScanTree(root string, pii []*regexp.Regexp, secrets []Secret) ([]Finding, error) {
	var out []Finding
	// Files are opened through a root-scoped handle, so a symbolic link in the tree cannot lead the scan
	// (or a reader of its report) outside it.
	rt, err := os.OpenRoot(root)
	if err != nil {
		return nil, err
	}
	defer rt.Close()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.Type()&fs.ModeSymlink != 0 {
			rel, _ := filepath.Rel(root, path)
			out = append(out, Finding{rel, 1, "symbolic link (not followed)"})
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		info, err := d.Info()
		if err != nil || !info.Mode().IsRegular() || info.Size() > maxScanBytes {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		fh, err := rt.Open(rel)
		if err != nil {
			return nil
		}
		b, err := io.ReadAll(io.LimitReader(fh, maxScanBytes))
		fh.Close()
		if err != nil || isBinary(b) {
			return nil
		}
		text := string(b)
		for n, line := range strings.Split(text, "\n") {
			for _, p := range genericPatterns {
				if p.RE.MatchString(line) {
					out = append(out, Finding{rel, n + 1, p.Name})
				}
			}
			for _, re := range pii {
				if re.MatchString(line) {
					out = append(out, Finding{rel, n + 1, "personal-data pattern"})
				}
			}
			for _, s := range secrets {
				if strings.Contains(line, s.Value) {
					out = append(out, Finding{rel, n + 1, "value from " + s.Origin})
				}
			}
		}
		return nil
	})
	return out, err
}

func isBinary(b []byte) bool {
	n := min(len(b), 8000)
	for _, c := range b[:n] {
		if c == 0 {
			return true
		}
	}
	return false
}

// DataFiles lists files that look like data or credentials rather than source, and files too large
// to belong in a source repository.
func DataFiles(root string) (data, large []string) {
	_ = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		name := strings.ToLower(d.Name())
		switch {
		case strings.HasSuffix(name, ".db"), strings.HasSuffix(name, ".db-wal"), strings.HasSuffix(name, ".sqlite"),
			strings.HasSuffix(name, ".age"), strings.HasSuffix(name, ".log"), strings.HasSuffix(name, ".pem"), strings.HasSuffix(name, ".key"),
			name == "settings.json", name == "credentials.json", name == "2fa.json", name == ".env", strings.HasPrefix(name, ".env."):
			data = append(data, rel)
		}
		if info, err := d.Info(); err == nil && info.Size() > 1<<20 {
			large = append(large, rel)
		}
		return nil
	})
	return
}
