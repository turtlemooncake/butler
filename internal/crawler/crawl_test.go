package crawler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestCrawl(t *testing.T) {
	pages := map[string]string{
		"/robots.txt": "User-agent: *\nDisallow: /private\n",
		"/":           `<a href="/a">a</a> <a href="/private/x">p</a> <a href="https://other.example/">o</a>`,
		"/a":          `<a href="/b">b</a> <a href="/">home</a> <a href="/a#top">top</a>`,
		"/b":          `<a href="/missing">m</a> <a href="/c">c</a>`,
		"/c":          `the end`,
	}
	var requested []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requested = append(requested, r.URL.Path)
		body, ok := pages[r.URL.Path]
		if !ok {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, body)
	}))
	defer srv.Close()

	tests := []struct {
		name string
		opts Options
		want []string
	}{
		{"depth 0", Options{MaxPages: 10, MaxDepth: 0}, []string{"/"}},
		{"all", Options{MaxPages: 10, MaxDepth: 5}, []string{"/", "/a", "/b", "/c"}},
		{"depth 1", Options{MaxPages: 10, MaxDepth: 1}, []string{"/", "/a"}},
		{"max pages", Options{MaxPages: 2, MaxDepth: 5}, []string{"/", "/a"}},
		// /b links to /missing (a 404) before /c: the crawl must carry on past it.
		{"broken link", Options{MaxPages: 10, MaxDepth: 5}, []string{"/", "/a", "/b", "/c"}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requested = nil
			var got []string
			err := Crawl(srv.URL+"/", tc.opts, func(p Page) error {
				got = append(got, p.URL)
				return nil
			})
			if err != nil {
				t.Fatalf("Crawl: %v", err)
			}
			for i := range got {
				got[i] = strings.TrimPrefix(got[i], srv.URL)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("crawled %q, want %q", got, tc.want)
			}
			if slices.Contains(requested, "/private/x") {
				t.Errorf("fetched /private/x, which robots.txt disallows")
			}
		})
	}
}
