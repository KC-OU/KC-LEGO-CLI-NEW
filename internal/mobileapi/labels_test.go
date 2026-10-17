package mobileapi

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newLabelsTestServer(s *Server) *httptest.Server {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /mobile/labels/{set}", s.withSession(s.handleLabelPDF))
	return httptest.NewServer(mux)
}

// TestLabelPDFIsAdminOnly mirrors TestNonAdminCannotAssignOrListTickets --
// printing labels is filed under admin tools, not open to every picker.
func TestLabelPDFIsAdminOnly(t *testing.T) {
	s, db := checkWalkEnv(t)
	srv := newLabelsTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/labels/75192-1", token, nil)
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("GET labels as a non-admin = %d, want 403", resp.StatusCode)
	}
}

// TestLabelPDFReturnsARealPDFForAKnownSet is the direct test for the
// happy path: a real PDF (same labels.PDF/LabelData/LabelSets pipeline
// `wms lego labels` uses), as admin, for a set that exists.
func TestLabelPDFReturnsARealPDFForAKnownSet(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newLabelsTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/labels/75192-1", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET labels/75192-1 as admin = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/pdf" {
		t.Errorf("Content-Type = %q, want application/pdf", ct)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	if len(body) < 4 || string(body[:4]) != "%PDF" {
		t.Errorf("body doesn't start with the PDF magic bytes: %q", body[:min(20, len(body))])
	}
}

// TestLabelPDFAcceptsAQueryParamTokenForOpeningDirectly is the regression
// test for withSession's fallback: the mobile app opens this URL via
// Linking.openURL (not client.js, no Authorization header), so a ?token=
// must work exactly like the header does.
func TestLabelPDFAcceptsAQueryParamTokenForOpeningDirectly(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newLabelsTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp, err := http.Get(srv.URL + "/mobile/labels/75192-1?token=" + token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("GET with ?token= = %d, want 200", resp.StatusCode)
	}
}

// TestLabelPDFAllowsAnUnknownSetNumber matches LabelSets/LabelData's own,
// intentional behavior (internal/lego/labeldata.go): any literal set number
// is accepted and gets a label with whatever catalog fields it has (blank
// if none) -- the same thing `wms lego labels <anything>` already does, for
// printing a label ahead of actually adding the set. Not a 404 case.
func TestLabelPDFAllowsAnUnknownSetNumber(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newLabelsTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/labels/99999999-1", token, nil)
	if resp.StatusCode != http.StatusOK {
		t.Errorf("GET labels for an unknown set = %d, want 200 (matches the CLI's own behavior)", resp.StatusCode)
	}
}

func TestLabelPDFRejectsAnInvalidSize(t *testing.T) {
	s, db := checkWalkEnv(t)
	asAdmin(t, "dave")
	srv := newLabelsTestServer(s)
	defer srv.Close()
	token := issueToken(t, db)

	resp := authed(t, http.MethodGet, srv.URL+"/mobile/labels/75192-1?size=not-a-real-size", token, nil)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("GET labels with a bogus size = %d, want 400", resp.StatusCode)
	}
}
