# Milestone 2: Find the links on a page

**Goal:** after fetching a page, Butler parses its HTML and lists every link
as a full, absolute URL. Milestone 3's crawler adds these links to its queue.

**Done when:**

```sh
$ go run . crawl http://go.dev
GET https://go.dev/
status:       200 OK
content-type: text/html; charset=utf-8
size:         64188 bytes
links:        93
  https://go.dev/
  https://go.dev/solutions/case-studies
  https://go.dev/solutions/use-cases
  https://go.dev/security/
  https://go.dev/learn/
  ...
```

(The numbers will change as the site changes.) Note the first line: you asked
for `http://go.dev`, but the server redirected you, and Butler reports where it
actually ended up.

```sh
$ go test ./...
ok  	butler/internal/crawler	0.339s
```

…with a table-driven test covering the link cases listed in Step 7.

**Concepts:** third-party modules (`go get`, `go.mod`, `go.sum`), the
`x/net/html` tokenizer, infinite `for` loops, `switch` on types of token,
`continue`, `for range` over a slice, `errors.Is`, resolving relative URLs,
maps as sets, `append`, pointers as parameters, table-driven tests.

---

## How the pieces fit

```
fetch(url) ─> Page{URL: final URL after redirects, Body: HTML bytes, ...}
                 │
Crawl:           ├─ not HTML? print "not HTML, no links", stop
                 ├─ base := url.Parse(page.URL)
                 ├─ links := extractLinks(base, page.Body)       ← new, in links.go
                 └─ print count + each link

extractLinks:
   for each token in the HTML
     is it <a ...>? → for each attribute
        is it href? → trim → resolve against base → keep only http(s)
                    → drop #fragment → skip if seen before → append
```

New code goes in a **new file**, `internal/crawler/links.go`, which is still
`package crawler`. A package can span many files in the same folder, and they
all see each other's names, lowercase ones included.

---

## Step 1: Add your first dependency

Go's standard library has no HTML parser. The usual choice is
`golang.org/x/net/html`, which is maintained by the Go team but lives outside
the standard library.

```sh
go get golang.org/x/net/html
```

What happens:

- `go.mod` gets a `require golang.org/x/net vX.Y.Z` line.
- A new file, `go.sum`, records checksums of the downloaded code, so every
  later download can be checked against them. Commit both files and never
  edit `go.sum` by hand.
- The code itself is cached in `~/go/pkg/mod`, not in your project.

⚠️ **Go version.** You have Go 1.23.4, and the current `x/net` requires
**Go 1.26**. `go get` will print something like:

```
go: golang.org/x/net@v0.61.0 requires go >= 1.26.0; switching to go1.26.9
go: upgraded go 1.23.4 => 1.26.0
```

It changes the `go` line in `go.mod` and **downloads a newer Go toolchain
automatically** for this project. That works, but it's cleaner to upgrade the
Go you have installed so your terminal, VS Code and the project all use the
same version. Go only supports its two newest releases, and 1.23 is past that.
Download the macOS ARM64 installer from https://go.dev/dl/, run it, then check
with `go version`.

After you've written code that imports the package (Step 2), run:

```sh
go mod tidy
```

It removes the `// indirect` comment from the `require` line (because the
package is now actually used) and cleans up anything unused. Run it whenever
you add or remove imports of third-party packages.

---

## Step 2: Tokenize the HTML

`x/net/html` offers two approaches:

- `html.Parse` builds a full **tree** of the document, which you walk recursively.
- `html.NewTokenizer` streams through the document **token by token**: start
  tags, end tags, text and so on.

For finding links, the tokenizer is simpler: one flat loop, no recursion.

```go
z := html.NewTokenizer(bytes.NewReader(body))
for {
	tt := z.Next() // advance to the next token; tt is its type
	switch tt {
	case html.ErrorToken:
		// end of the document (or a real error). See Step 3.
	case html.StartTagToken, html.SelfClosingTagToken:
		t := z.Token()
		// t.Data is the tag name, e.g. "a"
		// t.Attr is a []html.Attribute, each with .Key and .Val
	}
}
```

Notes:

- `for { ... }` with no condition loops **forever**. You leave it with
  `return` (or `break`, but see the warning below).
- `bytes.NewReader(body)` wraps your `[]byte` as an `io.Reader`, the stream
  type that the tokenizer (like most of Go's I/O) reads from.
- `case A, B:` matches either value. `<a href="...">` is a start tag, and
  `<a href="..."/>` is self-closing. Handle both.

⚠️ **`break` inside `switch`** exits only the `switch`, **not** the `for`
loop. That's why this loop ends with `return`. (Go has labeled breaks for
this, but you won't need them here.)

**Your task:** create `links.go` with:

```go
package crawler

// extractLinks returns the absolute http(s) URLs of every <a href> in body,
// resolved against base, without fragments or duplicates, in page order.
func extractLinks(base *url.URL, body []byte) ([]string, error) {
	// ...
}
```

`base *url.URL` is a **pointer** to a URL struct. `url.Parse` already gives you
a `*url.URL`, so you pass it straight through. Pointers are cheap to pass, and
it's how `net/url` is meant to be used.

---

## Step 3: Stop at the end of the document

When the tokenizer runs out of input, `z.Next()` returns `html.ErrorToken`, and
`z.Err()` says why. If the input simply ended, that's `io.EOF`, which isn't a
real error. Anything else is.

```go
case html.ErrorToken:
	if errors.Is(z.Err(), io.EOF) {
		return links, nil // normal end: hand back what we found
	}
	return nil, z.Err()
```

`errors.Is` checks whether an error **is, or wraps,** a particular error value.
This is where the `%w` wrapping from milestone 1 pays off: code further up can
still ask "was this ultimately an EOF?" even after layers of wrapping.

---

## Step 4: Pick out `<a href>`

Inside the start-tag case:

1. If `t.Data` isn't `"a"`, skip this token with `continue`.
2. Loop over the attributes:
   ```go
   for _, attr := range t.Attr {
   	// attr.Key, attr.Val
   }
   ```
   `range` over a slice gives you `(index, value)`. You don't need the index,
   so throw it away with `_`.
3. Skip attributes that aren't `href`.
4. `strings.TrimSpace(attr.Val)`. Real pages often have
   `href="  /about\n"`. Skip it if it's empty after trimming.

> **`continue` in a `switch` inside a `for`** jumps to the next iteration of
> the **loop**. That's what you want here, and it's the opposite of how
> `break` behaves.

---

## Step 5: Make it absolute

Links come in many shapes. Say the page is `https://example.com/docs/intro`:

| href | Means |
|---|---|
| `https://other.org/x` | `https://other.org/x` (already absolute) |
| `/about` | `https://example.com/about` (from the site root) |
| `setup` | `https://example.com/docs/setup` (relative to the current folder) |
| `../blog/` | `https://example.com/blog/` |
| `//cdn.example.com/a` | `https://cdn.example.com/a` (same scheme, different host) |
| `#top` | `https://example.com/docs/intro#top` (same page) |
| `mailto:me@x.com` | not a web page |

You don't have to handle these rules yourself. A `*url.URL` has a `Parse`
**method** that resolves a reference against it:

```go
u, err := base.Parse(href) // u is the absolute *url.URL
```

(This is different from the **function** `url.Parse`. `base.Parse` is called on
an existing URL and resolves relative to it.)

Then:

- If `err != nil`, skip this link with `continue`. One broken href shouldn't
  stop the whole page.
- Keep only `u.Scheme == "http"` or `"https"`. This drops `mailto:`, `tel:`,
  `javascript:` and similar.
- Remove the fragment with `u.Fragment = ""`. `/faq#q1` and `/faq#q2` are the
  same page to a crawler.
- Convert back to a string with `u.String()`.

---

## Step 6: No duplicates, keep the order

Pages link to the same URL many times (nav bar, footer…). Use a **map as a
set** to remember what you've already added:

```go
var links []string        // nil slice: len 0, ready to append to
seen := map[string]bool{} // empty map

if !seen[s] {             // a missing key returns the zero value: false
	seen[s] = true
	links = append(links, s)
}
```

- `append` returns the (possibly new) slice, so you must assign it back:
  `links = append(links, s)`. Writing `append(links, s)` alone is a compile
  error.
- Use the map only for checking and the slice for the result, because a
  **map has no order**. Ranging over a map gives a different order each time.
  The slice keeps page order.
- `var links []string` is a nil slice. If no links are found, you'll return
  `nil`, which is fine. `len(nil)` is 0 and `range` over nil does nothing.

---

## Step 7: Test it (table-driven)

`extractLinks` takes bytes and returns strings, with no network involved, so
it's easy to test. Go's usual style is a **table-driven test**: a slice of
cases, then one loop that checks them all.

Create `internal/crawler/links_test.go` and finish the table:

```go
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
		// your cases here
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
```

Syntax notes:

- `[]struct{ ... }{ ... }` is a slice of an **anonymous struct** type, defined
  and filled in one go. It's very common in tests.
- Backticks make a **raw string**: no escapes, and `"` can appear inside
  freely. That's handy for HTML.
- `t.Run(name, func...)` makes each case a **subtest**. It shows up separately
  in `go test -v`, and you can run one alone with
  `go test -run 'TestExtractLinks/dedupe' ./...`.
- `slices.Equal` (stdlib) compares two slices element by element. `nil` and an
  empty slice count as equal.
- `%q` on a `[]string` prints each element quoted, which makes stray spaces
  visible.

**Add cases for:**

| Case | Input | Want |
|---|---|---|
| relative | `setup` | `https://example.com/docs/setup` |
| parent dir | `../blog/` | `https://example.com/blog/` |
| fragment only | `#top` | `https://example.com/docs/intro` |
| strip fragment | `/faq#q1` | `https://example.com/faq` |
| empty / missing href | `<a href="">e</a><a>no href</a>` | `nil` |
| duplicates | `/a` and `/a#x` | just `https://example.com/a` |
| whitespace | `"  /b  "` | `https://example.com/b` |
| not an `<a>` | `<link href="/style.css"><img src="/i.png">` | `nil` |
| self-closing | `<a href="/c"/>` | `https://example.com/c` |

Write the test **before** you finish `extractLinks` if you like. Watching cases
go from FAIL to PASS is a nice way to work.

---

## Step 8: Use the final URL, and wire it into `Crawl`

**The final URL.** In `fetch`, you currently store `URL: rawURL`. But
`http://go.dev` redirects to `https://go.dev/`, and relative links on that page
must be resolved against **where you ended up**, not where you started. The
response knows the final URL:

```go
resp.Request.URL // *url.URL of the last request, after any redirects
```

**Your task:** change the `URL` field in `fetch`'s result to
`resp.Request.URL.String()`.

**In `Crawl`, after printing the summary:**

1. If the content type doesn't start with `text/html`, print
   `not HTML, no links` and `return nil`. (`strings.HasPrefix(s, prefix)`.
   Content types often have extras like `; charset=utf-8`, so don't use `==`.)
2. Parse `page.URL` with `url.Parse` to get the base.
3. Call `extractLinks`. On error, wrap it:
   `fmt.Errorf("extracting links from %s: %w", page.URL, err)`.
4. Print `links:` with the count, then each link indented two spaces.

Try it on `http://go.dev`, `https://go.dev/doc/`, or the site you plan to
crawl. Try an image URL too, to see the "not HTML" path.

---

## Errors you'll likely see

| Error / symptom | Meaning |
|---|---|
| `no required module provides package golang.org/x/net/html` | You haven't run `go get` yet (or you ran it in a different folder). |
| `requires go >= 1.26.0` | See the Go version note in Step 1. |
| `append(links, s) (value of type []string) is not used` | Assign it back: `links = append(links, s)`. |
| Program hangs at 100% CPU | The loop never returns. Check the `ErrorToken` case. A `break` there only leaves the `switch`. |
| Links like `https://example.com/docs/about` for `/about` | You're joining strings yourself instead of using `base.Parse`. |
| Relative links resolve to the wrong host after a redirect | `Page.URL` is still `rawURL`, not `resp.Request.URL.String()`. |
| `undefined: extractLinks` in the test | The test file must be `package crawler` (not `crawler_test`) to see lowercase names. |

---

## Check yourself

- Why does `extractLinks` need `base` at all? What would `/about` mean without it?
- What's the difference between `url.Parse(s)` and `base.Parse(s)`?
- Why use both a map **and** a slice instead of just the map?
- What does `break` do inside a `switch` inside a `for`? And `continue`?
- Why must links be resolved against the URL *after* redirects?
- What do `go.mod` and `go.sum` each record?

When it's working, send me `links.go`, `links_test.go` and the updated
`crawler.go` for review. Milestone 3 turns this into a real crawler: a queue of
URLs, a visited set, same-site rules, page/depth limits, a polite delay and
robots.txt.

<details>
<summary>Stuck? Skeleton of <code>extractLinks</code> (spoiler, not the full answer)</summary>

```go
func extractLinks(base *url.URL, body []byte) ([]string, error) {
	z := html.NewTokenizer(bytes.NewReader(body))
	var links []string
	seen := map[string]bool{}

	for {
		switch z.Next() {
		case html.ErrorToken:
			// EOF → return links, nil; otherwise return the error
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			// not "a"? continue
			for _, attr := range t.Attr {
				// not "href"? continue
				// trim; empty? continue
				// base.Parse; error? continue
				// not http/https? continue
				// clear fragment; seen? else add
			}
		}
	}
}
```

Imports for `links.go`: `bytes`, `errors`, `io`, `net/url`, `strings`,
`golang.org/x/net/html`.

</details>
