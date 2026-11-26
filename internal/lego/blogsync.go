package lego

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

type postFront struct {
	Slug    string `yaml:"slug"`
	Title   string `yaml:"title"`
	Set     string `yaml:"set"`
	Theme   string `yaml:"theme"`
	Summary string `yaml:"summary"`
	Audio   string `yaml:"audio"`
	Status  string `yaml:"status"`
}

// ParsePostFile reads one Markdown file: optional "---" YAML front matter (slug, title, set,
// theme, summary, audio, status), then the body. The slug defaults to the file name.
func ParsePostFile(name string, data []byte) (Post, error) {
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	var f postFront
	body := text
	if rest, ok := strings.CutPrefix(text, "---\n"); ok {
		head, after, found := strings.Cut(rest, "\n---")
		if !found {
			return Post{}, errors.New("front matter has no closing ---")
		}
		if err := yaml.Unmarshal([]byte(head), &f); err != nil {
			return Post{}, fmt.Errorf("front matter: %w", err)
		}
		body = strings.TrimPrefix(after, "\n")
	}
	slug := f.Slug
	if slug == "" {
		slug = strings.TrimSuffix(filepath.Base(name), filepath.Ext(name))
	}
	if f.Title == "" {
		return Post{}, errors.New("title is required in the front matter")
	}
	return Post{Slug: slug, Title: f.Title, SetNum: f.Set, Theme: f.Theme, Summary: f.Summary, Audio: f.Audio,
		BuildStatus: f.Status, Body: strings.TrimSpace(body)}, nil
}

// SyncPostsDir imports every *.md in dir into the review queue (never straight to published),
// skipping files whose content hash hasn't changed, and copies dir/audio/* to audioDir.
// It returns one line per file for the caller to print.
func (d *DB) SyncPostsDir(dir, audioDir, by string) ([]string, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.md"))
	if err != nil {
		return nil, err
	}
	var out []string
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			out = append(out, filepath.Base(f)+": "+err.Error())
			continue
		}
		sum := sha256.Sum256(data)
		hash := hex.EncodeToString(sum[:])
		p, err := ParsePostFile(f, data)
		if err != nil {
			out = append(out, filepath.Base(f)+": skipped, "+err.Error())
			continue
		}
		if old, _ := d.GetPost(strings.ToLower(p.Slug)); old != nil && old.SourceHash == hash {
			continue
		}
		saved, err := d.SavePost(p, by, "git", hash)
		if err != nil {
			out = append(out, filepath.Base(f)+": skipped, "+err.Error())
			continue
		}
		out = append(out, filepath.Base(f)+": "+saved.Slug+" is waiting for review")
	}
	if audioDir != "" {
		src, srcErr := os.OpenRoot(filepath.Join(dir, "audio"))
		_ = os.MkdirAll(audioDir, 0o755)
		dst, dstErr := os.OpenRoot(audioDir)
		if srcErr == nil && dstErr == nil {
			defer src.Close()
			defer dst.Close()
			entries, _ := os.ReadDir(filepath.Join(dir, "audio"))
			for _, e := range entries {
				name := e.Name()
				if e.IsDir() || !AudioFileRE.MatchString(name) {
					continue
				}
				if b, err := src.ReadFile(name); err == nil {
					_ = dst.WriteFile(name, b, 0o644)
				}
			}
		}
	}
	return out, nil
}
