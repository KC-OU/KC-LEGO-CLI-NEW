package teedy

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// fakeTeedy mimics just enough of the real API (confirmed live against an
// actual instance, see this package's doc comment) for these tests:
// login sets a cookie, document list returns whatever's in docs, document
// create + file upload append a new one.
func fakeTeedy(t *testing.T, docs *[]Document) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/user/login" && r.Method == http.MethodPost:
			http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "fake-session"})
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{}"))
		case r.URL.Path == "/api/document/list" && r.Method == http.MethodGet:
			if _, err := r.Cookie("auth_token"); err != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total": len(*docs), "documents": *docs})
		case r.URL.Path == "/api/document" && r.Method == http.MethodPut:
			_ = r.ParseForm()
			id := "doc-" + r.FormValue("title")
			*docs = append(*docs, Document{ID: id, Title: r.FormValue("title")})
			_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
		case r.URL.Path == "/api/file" && r.Method == http.MethodPut:
			if err := r.ParseMultipartForm(1 << 20); err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_, _, err := r.FormFile("file")
			if err != nil {
				w.WriteHeader(http.StatusBadRequest)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func newTestClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, username: "admin", password: "pw", http: &http.Client{}}
}

func TestFindByTitleSubstringMatchesCaseInsensitively(t *testing.T) {
	docs := []Document{{ID: "1", Title: "75192-1_TestUser_2026-10-24.pdf"}}
	srv := fakeTeedy(t, &docs)
	c := newTestClient(srv.URL)

	found, err := c.FindByTitleSubstring("75192-1_testuser")
	if err != nil {
		t.Fatal(err)
	}
	if !found {
		t.Fatal("expected a match")
	}
}

func TestFindByTitleSubstringNoMatch(t *testing.T) {
	docs := []Document{{ID: "1", Title: "10220-1_DaveSmith_2026-10-01.pdf"}}
	srv := fakeTeedy(t, &docs)
	c := newTestClient(srv.URL)

	found, err := c.FindByTitleSubstring("75192-1_TestUser")
	if err != nil {
		t.Fatal(err)
	}
	if found {
		t.Fatal("expected no match")
	}
}

func TestUploadDocumentCreatesAndAttachesAFile(t *testing.T) {
	var docs []Document
	srv := fakeTeedy(t, &docs)
	c := newTestClient(srv.URL)

	if err := c.UploadDocument("75192-1_TestUser_2026-10-24.pdf", "sheet.pdf", []byte("hello")); err != nil {
		t.Fatal(err)
	}
	if len(docs) != 1 || docs[0].Title != "75192-1_TestUser_2026-10-24.pdf" {
		t.Fatalf("docs = %+v", docs)
	}
}

func TestClientReLogsInOnAnExpiredSession(t *testing.T) {
	docs := []Document{{ID: "1", Title: "x"}}
	loginCount := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api/user/login":
			loginCount++
			// The first session is immediately treated as expired below,
			// so a second login must happen for the call to succeed.
			http.SetCookie(w, &http.Cookie{Name: "auth_token", Value: "session"})
			_, _ = w.Write([]byte("{}"))
		case r.URL.Path == "/api/document/list":
			ck, _ := r.Cookie("auth_token")
			if loginCount < 2 && ck != nil {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"total": len(docs), "documents": docs})
		}
	}))
	t.Cleanup(srv.Close)
	c := newTestClient(srv.URL)

	got, err := c.ListRecentDocuments(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 {
		t.Fatalf("got %v", got)
	}
	if loginCount != 2 {
		t.Fatalf("loginCount = %d, want 2 (one retry after the 401)", loginCount)
	}
}

func TestNewReturnsNilWhenNotConfigured(t *testing.T) {
	if c := New(); c != nil {
		t.Fatalf("expected nil, got %+v", c)
	}
}
