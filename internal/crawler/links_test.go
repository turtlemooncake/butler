package crawler

import (
	"net/url"
	"slices"
	"testing"
)

func TestExtractLinks(t *testing.T) {
	base, _ := url.Parse("https://example.com/docs/intro")

	tests := []struct {
		name string
		html string
		want []string
	}{
		{"absolute", `<a href="https://other.org/x">x</a>`, []string{"https://other.org/x"}},
		{"root-relative", `<a href="/about">a</a>`, []string{"https://example.com/about"}},
		{"skip mailto", `<a href="mailto:me@example.com">m</a>`, nil},
		{"relative", `<a href="setup">s</a>`, []string{"https://example.com/docs/setup"}},
		{"parent dir", `<a href="../blog/">b</a>`, []string{"https://example.com/blog/"}},
		{"protocol-relative", `<a href="//cdn.example.com/a">c</a>`, []string{"https://cdn.example.com/a"}},
		{"fragment only", `<a href="#top">t</a>`, []string{"https://example.com/docs/intro"}},
		{"strip fragment", `<a href="/faq#q1">f</a>`, []string{"https://example.com/faq"}},
		{"empty or missing href", `<a href="">e</a><a>no href</a>`, nil},
		{"duplicates", `<a href="/a">1</a><a href="/a#x">2</a>`, []string{"https://example.com/a"}},
		{"whitespace", `<a href="  /b  ">b</a>`, []string{"https://example.com/b"}},
		{"not an a tag", `<link href="/style.css"><img src="/i.png">`, nil},
		{"self-closing", `<a href="/c"/>`, []string{"https://example.com/c"}},
		{"keeps page order", `<a href="/z">z</a><a href="/a">a</a><a href="/m">m</a>`, []string{
			"https://example.com/z",
			"https://example.com/a",
			"https://example.com/m",
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := extractLinks(base, []byte(tc.html))
			if err != nil {
				t.Fatalf("extractLinks: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("got %q, want %q", got, tc.want)
			}
		})
	}
}
