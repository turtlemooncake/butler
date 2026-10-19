package extract

import (
	"strings"
	"testing"
)

const page = `<html><head><title>Gophers | Example</title></head><body>
<nav><a href="/">Home</a> <a href="/about">About</a> <a href="/login">Log in</a></nav>
<article>
  <h1>All about gophers</h1>
  <p>Gophers are small burrowing rodents that live in North and Central America.
     They spend most of their lives underground, digging long tunnels.</p>
  <p>They eat roots, tubers and other plant parts, which makes them unpopular
     with gardeners. A single gopher can move more than a ton of soil a year.</p>
  <p>The Go programming language uses a gopher as its mascot, drawn by Renee French.</p>
</article>
<footer>Copyright 2026 Example Inc. All rights reserved. Privacy policy.</footer>
</body></html>`

func TestExtract(t *testing.T) {
	title, text, err := Extract("https://example.com/gophers", []byte(page))
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(title, "Gophers") {
		t.Errorf("title = %q, want it to contain %q", title, "Gophers")
	}
	if !strings.Contains(text, "burrowing rodents") {
		t.Errorf("text is missing the article:\n%s", text)
	}
	if strings.Contains(text, "Log in") || strings.Contains(text, "All rights reserved") {
		t.Errorf("text still has nav or footer:\n%s", text)
	}
}

func TestCleanText(t *testing.T) {
	got := cleanText("\n\n  first line  \n\t\n   \nsecond line\n\n")
	want := "first line\nsecond line"
	if got != want {
		t.Errorf("got %q, want %q", got, want)
	}
}
