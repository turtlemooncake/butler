package store

import (
	"errors"
	"path/filepath"
	"slices"
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

func TestListPages(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	pages, err := s.ListPages()
	if err != nil {
		t.Fatalf("ListPages on empty store: %v", err)
	}
	if len(pages) != 0 {
		t.Errorf("ListPages on empty store = %d pages, want 0", len(pages))
	}

	when := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	urls := []string{"https://example.com/c", "https://example.com/a", "https://example.com/b"}
	for _, u := range urls {
		if err := s.SavePage(Page{URL: u, Title: "T", Text: "text", FetchedAt: when}); err != nil {
			t.Fatalf("SavePage(%s): %v", u, err)
		}
	}

	pages, err = s.ListPages()
	if err != nil {
		t.Fatalf("ListPages: %v", err)
	}
	var got []string
	for _, p := range pages {
		got = append(got, p.URL)
	}
	if !slices.Equal(got, urls) {
		t.Errorf("ListPages URLs = %q, want %q", got, urls)
	}
}
