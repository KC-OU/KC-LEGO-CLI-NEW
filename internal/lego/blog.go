package lego

import (
	"database/sql"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Review states of a public blog post. Only an admin approving a post in review publishes it.
const (
	PostDraft     = "draft"
	PostReview    = "review"
	PostPublished = "published"
	PostRejected  = "rejected"
	PostHidden    = "hidden" // was published, switched off by an admin; Show brings it back as it was
)

// PublicPhotoPrefix marks a set photo (set_photos.note) as one the public blog may show:
// "blog:Bag 3", "blog:Finished set". Every other set photo stays admin-only.
const PublicPhotoPrefix = "blog:"

var postSlugRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

type Post struct {
	ID                                        int64
	Slug, SetNum, Theme, Title, Summary, Body string
	Audio, BuildStatus, State, PreviewToken   string
	Source, SourceHash, UpdatedBy             string
	CreatedAt, UpdatedAt, PublishedAt         string
}

const postCols = `id, slug, set_num, theme, title, summary, body, audio, build_status, review_state, preview_token, source, source_hash, updated_by, created_at, updated_at, published_at`

func scanPost(sc interface{ Scan(...any) error }) (*Post, error) {
	var p Post
	err := sc.Scan(&p.ID, &p.Slug, &p.SetNum, &p.Theme, &p.Title, &p.Summary, &p.Body, &p.Audio, &p.BuildStatus, &p.State,
		&p.PreviewToken, &p.Source, &p.SourceHash, &p.UpdatedBy, &p.CreatedAt, &p.UpdatedAt, &p.PublishedAt)
	return &p, err
}

func (d *DB) GetPost(slug string) (*Post, error) {
	p, err := scanPost(d.QueryRow(`SELECT `+postCols+` FROM blog_posts WHERE slug = ?`, slug))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	return p, err
}

// ListPosts returns every post, newest-updated first (state "" = all states).
func (d *DB) ListPosts(state string) ([]Post, error) {
	q, args := `SELECT `+postCols+` FROM blog_posts`, []any{}
	if state != "" {
		q += ` WHERE review_state = ?`
		args = append(args, state)
	}
	rows, err := d.Query(q+` ORDER BY updated_at DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Post
	for rows.Next() {
		p, err := scanPost(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *p)
	}
	return out, rows.Err()
}

// SavePost creates or updates a post by slug. An edit to a published or rejected post sends it
// back to draft, so published text can never change without an admin approving it again.
// source/hash record where it came from ("web", or "git" with the file's hash).
func (d *DB) SavePost(p Post, by, source, hash string) (*Post, error) {
	p.Slug = strings.ToLower(strings.TrimSpace(p.Slug))
	if !postSlugRE.MatchString(p.Slug) {
		return nil, errors.New("slug must be lowercase letters, digits and dashes (e.g. 21027-1)")
	}
	if strings.TrimSpace(p.Title) == "" {
		return nil, errors.New("title is required")
	}
	if len(p.Body) > 200_000 {
		return nil, errors.New("post is too long")
	}
	now := time.Now().UTC().Format(timeLayout)
	old, err := d.GetPost(p.Slug)
	if err != nil {
		return nil, err
	}
	if old == nil {
		tok, err := newToken()
		if err != nil {
			return nil, err
		}
		state := PostDraft
		if source == "git" {
			state = PostReview // a pushed file goes straight to the review queue, never live
		}
		_, err = d.Exec(`INSERT INTO blog_posts (slug,set_num,theme,title,summary,body,audio,build_status,review_state,preview_token,source,source_hash,updated_by,created_at,updated_at)
			VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, p.Slug, p.SetNum, p.Theme, p.Title, p.Summary, p.Body, p.Audio, p.BuildStatus, state, tok, source, hash, by, now, now)
		if err != nil {
			return nil, err
		}
		return d.GetPost(p.Slug)
	}
	state := old.State
	if source == "git" {
		state = PostReview
		// A post with structured sections goes for review only once its checks pass: until then the pushed text
		// waits as a draft for the writer to finish the other sections.
		if s, _ := d.Sections(p.Slug); s != nil && s.Exists && !d.postReadyWith(p) {
			state = PostDraft
		}
	} else if state == PostPublished || state == PostRejected || state == PostHidden {
		state = PostDraft
	}
	_, err = d.Exec(`UPDATE blog_posts SET set_num=?, theme=?, title=?, summary=?, body=?, audio=?, build_status=?, review_state=?, source=?, source_hash=?, updated_by=?, updated_at=?, published_at=? WHERE slug=?`,
		p.SetNum, p.Theme, p.Title, p.Summary, p.Body, p.Audio, p.BuildStatus, state, source, hash, by, now, "", p.Slug)
	if err != nil {
		return nil, err
	}
	return d.GetPost(p.Slug)
}

// SetPostState moves a post along draft -> review -> published. Unpublish returns it to draft.
func (d *DB) SetPostState(slug, to, by string) error {
	p, err := d.GetPost(slug)
	if err != nil {
		return err
	}
	if p == nil {
		return errors.New("no such post")
	}
	ok := map[string][]string{
		PostDraft:     {PostReview},
		PostReview:    {PostPublished, PostRejected, PostDraft, PostChanges},
		PostChanges:   {PostReview, PostDraft},
		PostRejected:  {PostDraft, PostReview},
		PostPublished: {PostDraft, PostHidden},
		PostHidden:    {PostPublished, PostDraft},
	}
	allowed := false
	for _, t := range ok[p.State] {
		allowed = allowed || t == to
	}
	if !allowed {
		return fmt.Errorf("a %s post cannot go to %s", p.State, to)
	}
	var pub string
	switch {
	case to == PostHidden || (p.State == PostHidden && to == PostPublished):
		pub = p.PublishedAt // hiding and showing keep the original publish date
	case to == PostPublished:
		pub = time.Now().UTC().Format(timeLayout)
	}
	_, err = d.Exec(`UPDATE blog_posts SET review_state=?, updated_by=?, updated_at=?, published_at=? WHERE slug=?`,
		to, by, time.Now().UTC().Format(timeLayout), pub, slug)
	return err
}

// EnsureDraftForSet creates a private draft for a set the first time it is queued for a check,
// with its facts pre-filled. It never touches an existing post.
func (d *DB) EnsureDraftForSet(setNum, label, by string) error {
	slug := strings.ToLower(strings.TrimSpace(setNum))
	if !postSlugRE.MatchString(slug) {
		return nil
	}
	if p, err := d.GetPost(slug); err != nil || p != nil {
		return err
	}
	title, theme := strings.TrimSpace(label), ""
	var name string
	if err := d.QueryRow(`SELECT name, theme FROM sets WHERE set_num = ?`, setNum).Scan(&name, &theme); err == nil && name != "" {
		title = name
	} else if err := d.QueryRow(`SELECT name, theme FROM ref_sets WHERE set_num = ?`, setNum).Scan(&name, &theme); err == nil && name != "" {
		title = name
	}
	if title == "" {
		title = setNum
	}
	_, err := d.SavePost(Post{Slug: slug, SetNum: setNum, Theme: theme, Title: title,
		Body: "## Background\n\nWrite a few lines about this set in your own words.\n\n## Build notes\n\n"}, by, "web", "")
	if err == nil {
		err = d.EnsureSections(slug, by)
	}
	return err
}

// postReadyWith is PostReady for text that has just been saved but is not stored yet (the git sync path).
func (d *DB) postReadyWith(p Post) bool {
	for _, c := range d.ReadyChecks(p.Slug, "") {
		if c.Blocking && !c.OK && c.Name != "Text is clean" {
			return false
		}
	}
	errs, _ := LintPost(p.Body)
	return len(errs) == 0
}

// AudioFileRE names the audio files a post may play from the server's own blog-audio folder.
var AudioFileRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,80}\.(mp3|m4a|ogg|wav)$`)
