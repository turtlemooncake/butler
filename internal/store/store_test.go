package store

import (
	"errors"
	"path/filepath"
	"testing"
	"time"
)

func TestSaveAndGet(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	when := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	p := Page{URL: "https://example.com/a", Title: "A", Text: "first", FetchedAt: when}
	if err := s.SavePage(p); err != nil {
		t.Fatalf("SavePage: %v", err)
	}

	// Saving the same URL again replaces it instead of adding a second row.
	p.Text = "second"
	if err := s.SavePage(p); err != nil {
		t.Fatalf("SavePage again: %v", err)
	}

	n, err := s.CountPages()
	if err != nil {
		t.Fatalf("CountPages: %v", err)
	}
	if n != 1 {
		t.Errorf("CountPages = %d, want 1", n)
	}

	got, err := s.GetPage(p.URL)
	if err != nil {
		t.Fatalf("GetPage: %v", err)
	}
	if got != p {
		t.Errorf("GetPage = %+v, want %+v", got, p)
	}

	if _, err := s.GetPage("https://example.com/nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("GetPage(missing) error = %v, want ErrNotFound", err)
	}
}
