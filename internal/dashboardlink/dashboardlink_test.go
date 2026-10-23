package dashboardlink

import (
	"path/filepath"
	"testing"
)

func TestSetThenHasRoundTrips(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "links.json"))
	if has, err := s.Has("12345"); err != nil || has {
		t.Fatalf("has = %v, %v, want false, nil", has, err)
	}
	if err := s.Set("12345", true); err != nil {
		t.Fatal(err)
	}
	if has, err := s.Has("12345"); err != nil || !has {
		t.Fatalf("has = %v, %v, want true, nil", has, err)
	}
}

func TestHasIsCaseInsensitive(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "links.json"))
	if err := s.Set("Alex", true); err != nil {
		t.Fatal(err)
	}
	if has, err := s.Has("ALEX"); err != nil || !has {
		t.Fatalf("has = %v, %v, want true, nil", has, err)
	}
}

func TestSetFalseUnlinks(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "links.json"))
	_ = s.Set("12345", true)
	if err := s.Set("12345", false); err != nil {
		t.Fatal(err)
	}
	if has, err := s.Has("12345"); err != nil || has {
		t.Fatalf("has = %v, %v, want false, nil", has, err)
	}
}

func TestHasOnAMissingFileIsFalseNotAnError(t *testing.T) {
	s := New(filepath.Join(t.TempDir(), "never-written.json"))
	if has, err := s.Has("anyone"); err != nil || has {
		t.Fatalf("has = %v, %v, want false, nil", has, err)
	}
}
