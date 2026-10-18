# Milestone 1: Fetch one page

**Goal:** `butler crawl <url>` downloads that single page and prints what came
back. Bad input, network failures and error pages are reported as **errors**
returned from `crawler.Crawl`. The `main.go` you already wrote prints them and
exits 1.

**Done when:**

```sh
$ go run . crawl https://example.com
GET https://example.com
status:       200 OK
content-type: text/html; charset=utf-8
size:         577 bytes

$ go run . crawl example.com
crawl: invalid URL "example.com": must start with http:// or https://
$ echo $?
1

$ go run . crawl https://example.com/does-not-exist
crawl: GET https://example.com/does-not-exist: 404 Not Found

$ go run . crawl https://no-such-site.invalid
crawl: GET https://no-such-site.invalid: Get "https://no-such-site.invalid": dial tcp: lookup no-such-site.invalid: no such host
```

The exact size and content-type can differ, and so can the exact wording of
the network error.

**Concepts:** `net/url`, `net/http` (client, request, response), multiple return
values, `if err != nil`, wrapping errors with `%w`, `defer`, `io.ReadAll`,
`io.LimitReader`, constants, a first small struct.

All the code goes in `internal/crawler/crawler.go`. `main.go` doesn't change.

---

## How the pieces fit

```
Crawl(start string) error
  │
  ├── validate start with net/url ─────────── bad? return an error
  │
  ├── fetch(start) (Page, error)
  │     ├── build a request (method, URL, User-Agent header)
  │     ├── client.Do(req) ──────────────── network failure? return an error
  │     ├── defer resp.Body.Close()
  │     ├── status not 2xx? ─────────────── return an error
  │     └── read the body (with a size cap) → Page
  │
  └── print the Page's status, content-type, size
```

Splitting out `fetch` now pays off in milestone 3, when `Crawl` will call it
in a loop for every page.

---

## Step 1: Validate the URL

`net/url` parses a string into a `*url.URL` struct with fields like `Scheme`,
`Host` and `Path`:

```go
u, err := url.Parse("https://example.com/docs?x=1")
// u.Scheme == "https", u.Host == "example.com", u.Path == "/docs", u.RawQuery == "x=1"
```

`url.Parse` is very lenient. `url.Parse("example.com")` **succeeds**, with an
empty `Scheme` and `Path == "example.com"`. So a nil `err` isn't enough. You
also need to check:

- `u.Scheme` is `"http"` or `"https"`
- `u.Host` isn't empty

**Your task:** at the top of `Crawl`, parse `start`. If the parse fails or the
checks don't pass, return an error:

```go
return fmt.Errorf("invalid URL %q: must start with http:// or https://", start)
```

`fmt.Errorf` works like `Sprintf`, but it gives you an `error` instead of a
`string`.

> **Multiple return values.** `url.Parse` returns two things: `(*url.URL, error)`.
> Go functions often return a result plus an error, with the error last. You
> must accept both: `u, err := ...`. If you don't need one, use `_`:
> `_, err := url.Parse(s)`.

---

## Step 2: A client with a timeout

The easiest way to fetch is `http.Get(url)`, but it uses `http.DefaultClient`,
which has **no timeout**. A slow server could hang Butler forever. Make your
own client:

```go
var client = &http.Client{
	Timeout: 15 * time.Second,
}
```

Put this at **package level** (outside any function) so every fetch reuses it.
A client keeps a pool of open connections, and reusing it keeps them open
between requests, which will matter once the crawler fetches many pages.

Syntax notes:

- `http.Client{Timeout: ...}` is a **struct literal**. It creates a value and
  sets named fields. Fields you don't mention get their zero value.
- `&` takes its address, so `client` is a `*http.Client` (a pointer). Most
  `net/http` types are used through pointers. Calling methods works the same
  either way: `client.Do(...)`.
- `15 * time.Second` is a `time.Duration`. Go durations are typed, so you
  can't accidentally pass a plain `15`.

---

## Step 3: Build the request and send it

To set a header (Butler's **User-Agent**, so site owners can see who's
crawling), build the request yourself instead of using `client.Get`:

```go
req, err := http.NewRequest(http.MethodGet, rawURL, nil) // nil = no request body
if err != nil {
	return Page{}, err
}
req.Header.Set("User-Agent", "butler-bot/0.1")

resp, err := client.Do(req)
if err != nil {
	return Page{}, fmt.Errorf("GET %s: %w", rawURL, err)
}
```

> **`%w` vs `%v`.** Both put the inner error's text into the message. `%w` also
> *wraps* it, so code further up can still inspect the original error
> later with `errors.Is` / `errors.As`. Use `%w` when you're passing an error up
> and adding context. You get readable chains like:
> `crawl: GET https://x.invalid: Get "...": dial tcp: lookup x.invalid: no such host`.
> Each layer adds its own prefix.

> **Why `err` again?** `req, err :=` and then `resp, err :=` is allowed, because
> `:=` only needs **at least one new** variable on the left. Here `resp` is new,
> so `err` is just reassigned.

---

## Step 4: Always close the body with `defer`

`resp.Body` is a stream from the network. You **must** close it, or the
connection is never released, and a crawler will eventually run out of
connections.

```go
resp, err := client.Do(req)
if err != nil {
	return Page{}, fmt.Errorf(...)
}
defer resp.Body.Close()
```

`defer` schedules a call to run **when the surrounding function returns**, no
matter which `return` ends it. So you write the cleanup right after you get the
resource, and every exit path is covered.

⚠️ The order matters: `defer` goes **after** the `err` check. If `Do` failed,
`resp` is `nil`, and `resp.Body.Close()` would panic.

---

## Step 5: Check the status code

A 404 or 500 is **not** an error from `client.Do`. The request worked, but the
server said no. You have to check for it yourself:

```go
resp.StatusCode   // int, e.g. 404
resp.Status       // string, e.g. "404 Not Found"
```

**Your task:** if the status isn't in the 200–299 range, return an error like
`GET https://example.com/does-not-exist: 404 Not Found`. `net/http` has named
constants such as `http.StatusOK` (200) and `http.StatusMultipleChoices` (300)
if you'd rather not use bare numbers.

> **Redirects** (301, 302…) are followed automatically, up to 10, so you'll
> see the final page's status. `resp.Request.URL` tells you where you ended up,
> and the crawler will need that in milestone 3.

---

## Step 6: Read the body, with a cap

`io.ReadAll(r)` reads a stream until it ends and returns `([]byte, error)`. A
page could be huge (someone links a 2 GB file), so wrap the body in
`io.LimitReader` to stop after a maximum:

```go
const maxBodyBytes = 5 << 20 // 5 MiB

body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
```

- `const` is a compile-time constant. `5 << 20` means "5 shifted left 20 bits",
  which is 5 × 1,048,576 (5 MiB). It's a common Go idiom for sizes.
- Like `client`, put `maxBodyBytes` at package level, near the top of the file.
- `[]byte` is a slice of bytes, the raw content. `len(body)` is its size.
  `string(body)` converts it to text when you need it.

Also grab the content type, since milestone 3 will skip anything that isn't
HTML:

```go
ct := resp.Header.Get("Content-Type") // e.g. "text/html; charset=UTF-8"
```

---

## Step 7: Return a `Page`

`fetch` has several things to hand back, so group them in a small **struct**.
Define it at package level:

```go
// Page is the result of fetching one URL.
type Page struct {
	URL         string
	Status      string
	ContentType string
	Body        []byte
}
```

Then the shape of `fetch` is:

```go
// fetch downloads one URL and returns it as a Page.
func fetch(rawURL string) (Page, error) {
	// Steps 3–6 go here.
	// On any error:  return Page{}, err   (Page{} = an empty Page)
	// On success:    return Page{URL: rawURL, Status: resp.Status, ...}, nil
}
```

`fetch` is **lowercase** because only `Crawl` (in the same package) uses it.
`Page` is **capitalized** because later packages (the store) will want it.

Finally, in `Crawl`, after validating:

```go
page, err := fetch(start)
if err != nil {
	return err
}
// print the four lines from "Done when" using fmt.Printf
```

To line up the output, pad the labels yourself (`"status:       %s\n"`) or use
a width: `%-14s` prints a string left-aligned in 14 characters.

---

## Step 8: Imports

`crawler.go` now needs `fmt`, `io`, `net/http`, `net/url` and `time`. If you
installed `goimports`, it adds them for you on save. Otherwise the compiler
will tell you which ones are missing or unused.

Be polite while testing: fetch `https://example.com` (which exists for exactly
this) or a site you own, and don't run it in a loop.

---

## Errors you'll likely see

| Error / symptom | Meaning |
|---|---|
| `no new variables on left side of :=` | Every variable on the left already exists. Use `=` instead. |
| `not enough return values` | `fetch` returns `(Page, error)`, so every `return` needs both: `return Page{}, err`. |
| `cannot use body (variable of type []byte) as string value` | Convert with `string(body)`, or store it as `[]byte`. |
| `invalid memory address or nil pointer dereference` | Probably used `resp` (or deferred `Close`) before checking `err`. |
| `crawl: invalid URL` for a good URL | Check the `Scheme` condition. `||` vs `&&` is the usual culprit. |
| The program hangs | You're using `http.Get` / `http.DefaultClient`, which has no timeout. |

---

## Optional: your first test

Go has built-in testing, and `net/http/httptest` starts a fake server, so you
can test `fetch` without the internet. Create
`internal/crawler/crawler_test.go`:

```go
package crawler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFetchOK(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "<html>hi</html>")
	}))
	defer srv.Close()

	page, err := fetch(srv.URL)
	if err != nil {
		t.Fatalf("fetch: %v", err)
	}
	if len(page.Body) != 15 {
		t.Errorf("body size = %d, want 15", len(page.Body))
	}
}
```

Run it with `go test ./...`. Then try writing `TestFetch404` yourself: the
handler can call `w.WriteHeader(http.StatusNotFound)`, and the test should check
that `err` is **not** nil.

(Test files end in `_test.go`, test functions start with `Test` and take
`*testing.T`. Being in `package crawler` lets the test call lowercase `fetch`.)

---

## Check yourself

- Why does `url.Parse("example.com")` not return an error, and what do you
  check instead?
- Why must `defer resp.Body.Close()` come after the `err` check?
- Does a 404 make `client.Do` return an error? Where do you catch it?
- What would break if `client` were created inside `fetch` on every call?
- What's the difference between `%w` and `%v` in `fmt.Errorf`?

When the **Done when** checks pass, send me `crawler.go` (and the test, if
you wrote one) for review. Milestone 2 parses the HTML in `page.Body` and finds
every link.

<details>
<summary>Stuck? Skeleton of <code>crawler.go</code> (spoiler, not the full answer)</summary>

```go
package crawler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"
)

const maxBodyBytes = 5 << 20

var client = &http.Client{Timeout: 15 * time.Second}

// Page is the result of fetching one URL.
type Page struct {
	URL         string
	Status      string
	ContentType string
	Body        []byte
}

// Crawl will fetch pages starting from start. For now it fetches only start.
func Crawl(start string) error {
	// 1. url.Parse + scheme/host checks
	// 2. page, err := fetch(start)
	// 3. print
	return nil
}

// fetch downloads one URL and returns it as a Page.
func fetch(rawURL string) (Page, error) {
	// NewRequest, set User-Agent, client.Do, defer Close,
	// status check, ReadAll(LimitReader), return Page
}
```

</details>
