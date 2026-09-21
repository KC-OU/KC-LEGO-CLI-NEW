package update

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
		ok   bool
	}{
		{"v1.2.3", "1.2.3", 0, true}, {"v1.2.4", "v1.2.3", 1, true}, {"1.2.3", "1.10.0", -1, true}, {"2.0.0", "1.99.99", 1, true},
		{"1.0.0", "1.0.0-rc1", 1, true}, {"1.0.0-rc1", "1.0.0", -1, true}, {"1.0.0-rc1", "1.0.0-rc2", -1, true},
		{"dev", "1.0.0", 0, false}, {"1.0.0", "", 0, false}, {"1.0", "1.0.0", 0, false}, {"a.b.c", "1.0.0", 0, false},
	}
	for _, c := range cases {
		got, ok := Compare(c.a, c.b)
		if got != c.want || ok != c.ok {
			t.Errorf("Compare(%q, %q) = %d,%v want %d,%v", c.a, c.b, got, ok, c.want, c.ok)
		}
	}
}

func tarGz(t *testing.T, files map[string][]byte, types map[string]byte) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(zw)
	for name, data := range files {
		typ := byte(tar.TypeReg)
		if tt, ok := types[name]; ok {
			typ = tt
		}
		h := &tar.Header{Name: name, Mode: 0o755, Size: int64(len(data)), Typeflag: typ}
		if typ == tar.TypeSymlink {
			h.Linkname, h.Size = "/etc/passwd", 0
			data = nil
		}
		if err := tw.WriteHeader(h); err != nil {
			t.Fatal(err)
		}
		tw.Write(data)
	}
	tw.Close()
	zw.Close()
	return buf.Bytes()
}

type fakeRelease struct {
	*httptest.Server
	mu      sync.Mutex
	tag     string
	archive []byte
	sums    string
	noSums  bool
	noAsset bool
	hits    int
	evilURL string
	status  int
}

func newRelease(t *testing.T, tag string, binary []byte) *fakeRelease {
	f := &fakeRelease{tag: tag}
	f.archive = tarGz(t, map[string][]byte{"LICENSE": []byte("mit"), "wms": binary}, nil)
	name := ArchiveName(tag, runtime.GOOS, runtime.GOARCH)
	sum := sha256.Sum256(f.archive)
	f.sums = hex.EncodeToString(sum[:]) + "  " + name + "\n" + strings.Repeat("0", 64) + "  other.zip\n"
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		f.mu.Lock()
		defer f.mu.Unlock()
		f.hits++
		switch {
		case r.URL.Path == "/repos/KC-OU/KC-LEGO-CLI-NEW/releases/latest":
			if f.status != 0 {
				w.WriteHeader(f.status)
				return
			}
			assets := fmt.Sprintf(`{"name":%q,"browser_download_url":"%s/dl/%s","size":1}`, name, f.URL, name)
			if f.evilURL != "" {
				assets = fmt.Sprintf(`{"name":%q,"browser_download_url":%q,"size":1}`, name, f.evilURL)
			}
			if f.noAsset {
				assets = `{"name":"wms-go_9.9.9_plan9_arm.tar.gz","browser_download_url":"x"}`
			}
			if !f.noSums {
				assets += fmt.Sprintf(`,{"name":"checksums.txt","browser_download_url":"%s/dl/checksums.txt"}`, f.URL)
			}
			fmt.Fprintf(w, `{"tag_name":%q,"name":"r","assets":[%s]}`, f.tag, assets)
		case r.URL.Path == "/dl/"+name:
			w.Write(f.archive)
		case r.URL.Path == "/dl/checksums.txt":
			w.Write([]byte(f.sums))
		default:
			w.WriteHeader(404)
		}
	}))
	t.Cleanup(f.Close)
	return f
}

// set changes the fake between requests; the server goroutine reads these fields.
func (f *fakeRelease) set(fn func(*fakeRelease)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeRelease) client() *Client {
	return &Client{API: f.URL, Repo: DefaultRepo, HTTP: f.Client()}
}

func installedExe(t *testing.T, content string) string {
	dir := t.TempDir()
	p := filepath.Join(dir, "wms")
	if err := os.WriteFile(p, []byte(content), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

const goodBinary = "#!/bin/sh\necho wms new\n"

func TestLatestAndItsFailureModes(t *testing.T) {
	f := newRelease(t, "v1.2.0", []byte(goodBinary))
	rel, err := f.client().Latest(context.Background())
	if err != nil || rel.Tag != "v1.2.0" || len(rel.Assets) != 2 {
		t.Fatalf("latest = %+v %v", rel, err)
	}
	f.set(func(f *fakeRelease) { f.status = 404 })
	if _, err := f.client().Latest(context.Background()); !errors.Is(err, ErrNoReleases) {
		t.Errorf("404 = %v", err)
	}
	f.set(func(f *fakeRelease) { f.status = 403 })
	if _, err := f.client().Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "rate-limiting") {
		t.Errorf("403 = %v", err)
	}
	f.set(func(f *fakeRelease) { f.status = 500 })
	if _, err := f.client().Latest(context.Background()); err == nil {
		t.Error("500 must be an error")
	}
	if _, err := (&Client{API: "http://api.example.com", Repo: DefaultRepo}).Latest(context.Background()); err == nil || !strings.Contains(err.Error(), "https") {
		t.Errorf("a non-https update address must be refused: %v", err)
	}
}

func TestApplyVerifiesInstallsAndKeepsTheOldBinary(t *testing.T) {
	f := newRelease(t, "v1.2.0", []byte(goodBinary))
	exe := installedExe(t, "OLD BINARY")
	rel, _ := f.client().Latest(context.Background())
	res, err := f.client().Apply(context.Background(), rel, "v1.1.0", Options{Exe: exe})
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != goodBinary {
		t.Errorf("the new binary is installed: %q", b)
	}
	if b, _ := os.ReadFile(exe + ".old"); string(b) != "OLD BINARY" || res.OldPath != exe+".old" {
		t.Errorf("the old binary is kept: %q", b)
	}
	if fi, _ := os.Stat(exe); fi.Mode().Perm() != 0o755 {
		t.Errorf("mode = %v", fi.Mode().Perm())
	}
	if !res.Verified || res.Version != "v1.2.0" || len(res.SHA256) != 64 {
		t.Errorf("result = %+v", res)
	}
	if left, _ := filepath.Glob(filepath.Join(filepath.Dir(exe), ".wms-new-*")); len(left) != 0 {
		t.Errorf("temp files left behind: %v", left)
	}
}

func TestNothingIsInstalledUnlessEveryCheckPasses(t *testing.T) {
	ctx := context.Background()
	cases := map[string]func(f *fakeRelease){
		"checksum mismatch":       func(f *fakeRelease) { f.archive = append(f.archive, 'x') },
		"no checksums.txt":        func(f *fakeRelease) { f.noSums = true },
		"asset not listed":        func(f *fakeRelease) { f.sums = strings.Repeat("0", 64) + "  other.zip\n" },
		"no build for platform":   func(f *fakeRelease) { f.noAsset = true },
		"untrusted download host": func(f *fakeRelease) { f.evilURL = "https://evil.example.com/wms.tar.gz" },
		"plain http download":     func(f *fakeRelease) { f.evilURL = "http://cdn.example.com/wms.tar.gz" },
	}
	for name, mutate := range cases {
		f := newRelease(t, "v1.2.0", []byte(goodBinary))
		f.set(mutate)
		exe := installedExe(t, "OLD BINARY")
		rel, err := f.client().Latest(ctx)
		if err != nil {
			t.Fatalf("%s: %v", name, err)
		}
		if _, err := f.client().Apply(ctx, rel, "v1.1.0", Options{Exe: exe}); err == nil {
			t.Errorf("%s: must be refused", name)
		}
		if b, _ := os.ReadFile(exe); string(b) != "OLD BINARY" {
			t.Errorf("%s: the installed binary changed: %q", name, b)
		}
		if _, err := os.Stat(exe + ".old"); err == nil {
			t.Errorf("%s: nothing should have been moved", name)
		}
	}
}

func TestOlderOrEqualVersionsAndPackageManagedPathsAreRefusedUnlessForced(t *testing.T) {
	ctx := context.Background()
	f := newRelease(t, "v1.2.0", []byte(goodBinary))
	rel, _ := f.client().Latest(ctx)
	exe := installedExe(t, "OLD")
	for _, cur := range []string{"v1.2.0", "v2.0.0", "1.2.0"} {
		if _, err := f.client().Apply(ctx, rel, cur, Options{Exe: exe}); err == nil || !strings.Contains(err.Error(), "not newer") {
			t.Errorf("current %s: %v", cur, err)
		}
	}
	if _, err := f.client().Apply(ctx, rel, "v2.0.0", Options{Exe: exe, Force: true}); err != nil {
		t.Errorf("--force allows a downgrade/reinstall: %v", err)
	}
	if _, err := f.client().Apply(ctx, rel, "v1.0.0", Options{Exe: "/usr/bin/wms-not-real"}); err == nil || !strings.Contains(err.Error(), "package-managed") {
		t.Errorf("package-managed path: %v", err)
	}
	if _, err := f.client().Apply(ctx, rel, "dev", Options{Exe: installedExe(t, "OLD")}); err != nil {
		t.Errorf("a dev build can always update: %v", err)
	}
}

func TestABinaryThatDoesNotRunIsNotInstalled(t *testing.T) {
	f := newRelease(t, "v1.2.0", []byte("#!/bin/sh\nexit 7\n"))
	exe := installedExe(t, "OLD BINARY")
	rel, _ := f.client().Latest(context.Background())
	if _, err := f.client().Apply(context.Background(), rel, "v1.0.0", Options{Exe: exe}); err == nil || !strings.Contains(err.Error(), "does not run") {
		t.Fatalf("error = %v", err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "OLD BINARY" {
		t.Errorf("the good binary must survive: %q", b)
	}
}

func TestOnlyTheWmsFileIsExtractedFromAnArchive(t *testing.T) {
	good := tarGz(t, map[string][]byte{"wms": []byte("BIN")}, nil)
	if b, err := extractBinary(good); err != nil || string(b) != "BIN" {
		t.Errorf("good: %q %v", b, err)
	}
	nested := tarGz(t, map[string][]byte{"dist/wms": []byte("BIN2")}, nil)
	if b, err := extractBinary(nested); err != nil || string(b) != "BIN2" {
		t.Errorf("a nested wms is fine (its path is never used): %q %v", b, err)
	}
	for name, a := range map[string][]byte{
		"traversal":     tarGz(t, map[string][]byte{"../../etc/wms": []byte("x")}, nil),
		"symlink":       tarGz(t, map[string][]byte{"wms": nil}, map[string]byte{"wms": tar.TypeSymlink}),
		"other names":   tarGz(t, map[string][]byte{"README": []byte("x"), "wms.sh": []byte("x")}, nil),
		"empty file":    tarGz(t, map[string][]byte{"wms": {}}, nil),
		"not gzip":      []byte("hello"),
		"truncated tar": good[:len(good)/2],
	} {
		if _, err := extractBinary(a); err == nil {
			t.Errorf("%s must be refused", name)
		}
	}
}

func TestRedirectsOffTheAllowListAreRefused(t *testing.T) {
	evil := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.Write([]byte("evil")) }))
	defer evil.Close()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, evil.URL+"/x", http.StatusFound)
	}))
	defer srv.Close()
	c := &Client{API: srv.URL, Repo: DefaultRepo, HTTP: srv.Client()}
	if _, err := c.fetch(context.Background(), srv.URL+"/a", 1024); err == nil {
		t.Error("a redirect to another host must be refused")
	}
}

func TestChecksumParsing(t *testing.T) {
	sums := []byte(strings.Repeat("a", 64) + "  wms-go_1_linux_amd64.tar.gz\n" + strings.Repeat("B", 64) + " *bin.tar.gz\nshort  x\n")
	if got, ok := checksumFor(sums, "wms-go_1_linux_amd64.tar.gz"); !ok || got != strings.Repeat("a", 64) {
		t.Errorf("got %q %v", got, ok)
	}
	if got, ok := checksumFor(sums, "bin.tar.gz"); !ok || got != strings.Repeat("b", 64) {
		t.Errorf("binary-mode marker and case: %q %v", got, ok)
	}
	if _, ok := checksumFor(sums, "x"); ok {
		t.Error("a malformed line is ignored")
	}
}

func FuzzExtractBinary(f *testing.F) {
	f.Add([]byte("garbage"))
	f.Add([]byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 3})
	f.Fuzz(func(t *testing.T, data []byte) { _, _ = extractBinary(data) })
}
