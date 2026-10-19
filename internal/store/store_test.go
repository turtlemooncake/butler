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

func TestVectorRoundTrip(t *testing.T) {
	v := []float32{1.5, -2, 0, 3.14159, -0.0001}
	b := encodeVector(v)
	if len(b) != 20 {
		t.Errorf("encodeVector: %d bytes, want 20", len(b))
	}

	got, err := decodeVector(b)
	if err != nil {
		t.Fatalf("decodeVector: %v", err)
	}
	if !slices.Equal(got, v) {
		t.Errorf("decodeVector = %v, want %v", got, v)
	}

	if _, err := decodeVector(make([]byte, 5)); err == nil {
		t.Error("decodeVector of 5 bytes: got no error, want one")
	}
}

func testChunks(texts ...string) []Chunk {
	var cs []Chunk
	for _, t := range texts {
		cs = append(cs, Chunk{Text: t, Embedding: []float32{1, 2, 3}})
	}
	return cs
}

func urls(pages []Page) []string {
	var us []string
	for _, p := range pages {
		us = append(us, p.URL)
	}
	return us
}

func TestChunks(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "test.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer s.Close()

	when := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	a := Page{URL: "https://example.com/a", Title: "A", Text: "text a", FetchedAt: when}
	b := Page{URL: "https://example.com/b", Title: "B", Text: "text b", FetchedAt: when}

	// checkToIndex fails the test if PagesToIndex doesn't return want.
	checkToIndex := func(step string, want ...string) {
		t.Helper()
		pages, err := s.PagesToIndex()
		if err != nil {
			t.Fatalf("%s: PagesToIndex: %v", step, err)
		}
		if got := urls(pages); !slices.Equal(got, want) {
			t.Errorf("%s: PagesToIndex = %q, want %q", step, got, want)
		}
	}
	// checkCount fails the test if CountChunks isn't want.
	checkCount := func(step string, want int) {
		t.Helper()
		n, err := s.CountChunks()
		if err != nil {
			t.Fatalf("%s: CountChunks: %v", step, err)
		}
		if n != want {
			t.Errorf("%s: CountChunks = %d, want %d", step, n, want)
		}
	}

	// 1. New pages have no chunks, so both need indexing.
	for _, p := range []Page{a, b} {
		if err := s.SavePage(p); err != nil {
			t.Fatalf("SavePage: %v", err)
		}
	}
	checkToIndex("new pages", a.URL, b.URL)

	// 2. Once a has chunks, only b is left.
	if err := s.SaveChunks(a.URL, testChunks("x", "y")); err != nil {
		t.Fatalf("SaveChunks: %v", err)
	}
	checkCount("after SaveChunks", 2)
	checkToIndex("after SaveChunks", b.URL)

	// 3. Saving again replaces a's chunks instead of adding to them.
	if err := s.SaveChunks(a.URL, testChunks("z")); err != nil {
		t.Fatalf("SaveChunks again: %v", err)
	}
	checkCount("after SaveChunks again", 1)

	// 4. Chunks for a page that doesn't exist.
	if err := s.SaveChunks("https://example.com/nope", testChunks("x")); !errors.Is(err, ErrNotFound) {
		t.Errorf("SaveChunks(missing page) error = %v, want ErrNotFound", err)
	}

	// 5. Re-crawling a with the same text keeps its chunks.
	if err := s.SavePage(a); err != nil {
		t.Fatalf("SavePage same text: %v", err)
	}
	checkToIndex("same text", b.URL)

	// 6. Re-crawling a with new text drops its chunks.
	a.Text = "new text a"
	if err := s.SavePage(a); err != nil {
		t.Fatalf("SavePage new text: %v", err)
	}
	checkToIndex("new text", a.URL, b.URL)
	checkCount("new text", 0)

	// 7. DeleteAllChunks empties the table.
	if err := s.SaveChunks(b.URL, testChunks("x", "y", "z")); err != nil {
		t.Fatalf("SaveChunks b: %v", err)
	}
	if err := s.DeleteAllChunks(); err != nil {
		t.Fatalf("DeleteAllChunks: %v", err)
	}
	checkCount("after DeleteAllChunks", 0)
}
