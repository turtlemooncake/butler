package crawler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestCrawlConcurrent(t *testing.T) {
	// The handler runs in a new goroutine for every request, so everything it
	// changes is shared and needs the lock.
	var mu sync.Mutex
	inFlight, most := 0, 0

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/robots.txt" {
			http.NotFound(w, r)
			return
		}
		mu.Lock()
		inFlight++
		most = max(most, inFlight)
		mu.Unlock()

		time.Sleep(100 * time.Millisecond) // a slow page

		mu.Lock()
		inFlight--
		mu.Unlock()

		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/" {
			fmt.Fprint(w, `<a href="/1">1</a> <a href="/2">2</a> <a href="/3">3</a> <a href="/4">4</a>`)
		}
	}))
	defer srv.Close()

	start := time.Now()
	pages := 0
	err := Crawl(srv.URL+"/", Options{MaxPages: 5, MaxDepth: 1, Workers: 4}, func(p Page) error {
		pages++ // handle runs in Crawl's own goroutine, so no lock needed
		return nil
	})
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	elapsed := time.Since(start)

	if pages != 5 {
		t.Errorf("handled %d pages, want 5", pages)
	}
	if most != 4 {
		t.Errorf("at most %d requests at once, want 4", most)
	}
	// One at a time, 5 pages would take about 500ms: "/" then 4 links.
	// With 4 workers it's "/" then the 4 links together: about 200ms.
	if elapsed > 400*time.Millisecond {
		t.Errorf("crawl took %v, want under 400ms", elapsed)
	}
}

func TestCrawlConcurrentOrder(t *testing.T) {
	// /a is slow and /b is fast, so /b finishes first. Pages must still be
	// handled in queue order: /a before /b.
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/robots.txt":
			http.NotFound(w, r)
			return
		case "/a":
			time.Sleep(100 * time.Millisecond)
		}
		w.Header().Set("Content-Type", "text/html")
		if r.URL.Path == "/" {
			fmt.Fprint(w, `<a href="/a">a</a> <a href="/b">b</a>`)
		}
	}))
	defer srv.Close()

	var got []string
	err := Crawl(srv.URL+"/", Options{MaxPages: 10, MaxDepth: 1, Workers: 2}, func(p Page) error {
		got = append(got, strings.TrimPrefix(p.URL, srv.URL))
		return nil
	})
	if err != nil {
		t.Fatalf("Crawl: %v", err)
	}
	if want := []string{"/", "/a", "/b"}; !slices.Equal(got, want) {
		t.Errorf("handled %q, want %q", got, want)
	}
}
