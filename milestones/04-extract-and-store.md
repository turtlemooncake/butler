# Milestone 4: Extract the text and save it

**Goal:** the crawl now keeps something. For every HTML page it fetches, Butler
pulls out the title and the main text (no menus, footers or cookie banners)
and saves them to a SQLite database file on your machine. Crawling the same
page again updates its row instead of adding a second one.

**Done when:**

```sh
$ go run . crawl -max-pages 4 -max-depth 1 -delay 500ms https://go.dev/doc/
[1/4] depth 0  https://go.dev/doc/  (137 links)
[2/4] depth 1  https://go.dev/  (93 links)
[3/4] depth 1  https://go.dev/solutions/case-studies  (75 links)
[4/4] depth 1  https://go.dev/solutions/use-cases  (52 links)
done: 4 fetched, 0 errors, 107 still queued
saved 4 pages to butler.db (4 in total)

$ go run . crawl -max-pages 2 -max-depth 1 -delay 500ms https://go.dev/doc/
...
saved 2 pages to butler.db (4 in total)
```

The second crawl fetched two pages Butler already had, so the total stays at 4.
You can look inside the database with the `sqlite3` tool that comes with macOS:

```sh
$ sqlite3 butler.db "SELECT url, title, length(text), fetched_at FROM pages"
https://go.dev/doc/|Documentation - The Go Programming Language|10586|2026-10-18T19:39:18Z
https://go.dev/|The Go Programming Language|3626|2026-10-18T19:39:19Z
https://go.dev/solutions/case-studies|Case Studies - The Go Programming Language|420|2026-10-18T19:39:17Z
https://go.dev/solutions/use-cases|Use Cases - The Go Programming Language|594|2026-10-18T19:39:17Z

$ sqlite3 butler.db "SELECT substr(text, 1, 200) FROM pages WHERE url = 'https://go.dev/doc/'"
The Go programming language is an open source project to make programmers more
productive.
Go is expressive, concise, clean, and efficient. Its concurrency
mechanisms make it easy to write programs that get the most out
```

(The numbers and times will differ.)

```sh
$ go test ./...
?   	butler	[no test files]
ok  	butler/internal/crawler	0.381s
ok  	butler/internal/extract	0.626s
ok  	butler/internal/store	0.893s
```

**Concepts:** function types and passing a function as an argument, closures,
new packages of your own, named result values, `strings.Split` /
`strings.Join`, `database/sql`, a blank import (`_ "..."`) to register a
driver, SQL placeholders, upserts, methods and pointer receivers, `Scan` into
pointers, sentinel errors like `sql.ErrNoRows`, `time.Time` ↔ text,
`t.TempDir`, why `os.Exit` skips `defer`.

---

## How the pieces fit

```
main.go  runCrawl(args)
  ├─ flags (+ new -db flag)
  ├─ st := store.Open("butler.db")          ← internal/store
  ├─ defer st.Close()
  └─ crawler.Crawl(url, opts, handle)
          │
          │  for every HTML page fetched, Crawl calls:
          ▼
     handle(page)   ← a function written inside runCrawl
       ├─ title, text := extract.Extract(page.URL, page.Body)   ← internal/extract
       └─ st.SavePage(store.Page{URL, Title, Text, FetchedAt})
```

Each package does one job and none of them know about each other.
`crawler` fetches pages, `extract` turns HTML into text, `store` saves rows.
Only `main.go` knows about all three and connects them.

---

## Step 1: Let `Crawl` hand pages back

`Crawl` currently returns `[]string`, the URLs it fetched. Now the caller needs
the **pages**, and it needs each one **as soon as it's fetched**, not in a big
slice at the end. Saving as you go means a crawl that fails at page 150 has
still saved 149 pages, and you don't hold 150 page bodies in memory at once.

The Go way to do this is to have the caller pass in a **function** for `Crawl`
to call with each page:

```go
// Crawl fetches pages breadth-first from start, staying on start's host, and
// calls handle with every HTML page it fetches. If handle returns an error,
// the crawl stops and Crawl returns that error.
func Crawl(start string, opts Options, handle func(Page) error) error
```

`func(Page) error` is a **function type**: "a function that takes a `Page` and
returns an `error`". In Go, functions are values. You can store them in
variables and pass them as arguments, just like an `int` or a string.

**Your task:**

1. Change the signature as above. `Crawl` now returns only `error`, so every
   `return nil, err` becomes `return err`, and the final line is `return nil`.
2. Delete the `crawled` slice and the `append` to it.
3. After printing the `[n/N] depth ...` line for an HTML page, call the
   function:
   ```go
   if err := handle(page); err != nil {
   	return err
   }
   ```
   Call it before the `MaxDepth` check, so pages at the depth limit are passed
   along too.

Why stop the crawl when `handle` fails? Because the caller decides what's
fatal. If the database can't save, there's no point continuing. A page whose
text can't be extracted is a different matter, and you'll make `handle` return
`nil` for that one in Step 9.

**Fix the test.** `crawl_test.go` still expects `got, err := Crawl(...)`. Pass
a function that collects URLs instead:

```go
var got []string
err := Crawl(srv.URL+"/", tc.opts, func(p Page) error {
	got = append(got, p.URL)
	return nil
})
```

`func(p Page) error { ... }` written inline is a **function literal**. It can
read and change `got`, which belongs to the code around it. A function that
does this is called a **closure**. You've already used one: the `httptest`
handler in milestone 3 appends to `requested` the same way.

Run `go test ./internal/crawler/` before moving on. It should pass unchanged.

---

## Step 2: The `extract` package

Raw HTML is mostly not content. A typical page wraps a few paragraphs in
navigation, a footer, scripts, sidebars and cookie notices. Feeding all that to
the LLM later would bury the useful text and waste its limited context.
**Readability** is the algorithm Firefox's Reader View uses to find the main
content, and `go-readability` is a Go port of it.

```sh
go get github.com/go-shiori/go-readability
```

You'll notice its version in `go.mod` looks like
`v0.0.0-20251205110129-5db1dc9836f0`. That's a **pseudo-version**: the module
has no release tags, so Go names the version after the commit's date and hash.
It works exactly like a normal version.

What you'll use from it:

```go
article, err := readability.FromReader(r, pageURL) // r is an io.Reader, pageURL a *url.URL
article.Title       // the page title, cleaned up
article.TextContent // the main content as plain text
```

`pageURL` lets it resolve relative links and images inside the article.

**Your task:** create `internal/extract/extract.go`:

```go
// Package extract turns a web page's HTML into its title and main text.
package extract

// Extract returns the title and the main readable text of an HTML page,
// without navigation, ads and other boilerplate.
func Extract(pageURL string, body []byte) (title, text string, err error) {
	// 1. url.Parse(pageURL)
	// 2. readability.FromReader(bytes.NewReader(body), u)
	//    wrap an error: fmt.Errorf("readability: %w", err)
	// 3. return the trimmed title, cleanText(article.TextContent), nil
}
```

Notes:

- The import path ends in `go-readability`, but the package inside is called
  `readability`. A dash isn't allowed in a Go name, so the two differ. To make
  this obvious to readers, you can name the import explicitly:
  ```go
  import readability "github.com/go-shiori/go-readability"
  ```
- `(title, text string, err error)` are **named results**. Here they work as
  documentation: the signature says which `string` is which. You still write
  full `return` statements: `return "", "", err`.
- The `// Package extract ...` line is the package's doc comment. Write one
  per package, above the `package` line in one of its files.

---

## Step 3: Clean up the text

`TextContent` keeps the HTML's whitespace, so it's full of blank lines and
indentation:

```
\n\n      Documentation\n    \n\n  The Go programming language is ...
```

Write a helper in the same file that trims each line and drops the empty ones:

```go
// cleanText trims every line and drops the empty ones.
func cleanText(s string) string
```

- `strings.Split(s, "\n")` gives you a `[]string` of lines.
- `strings.TrimSpace(line)` removes spaces, tabs and `\r` from both ends.
- Collect the non-empty lines in a slice, then `strings.Join(lines, "\n")`
  puts them back together.

Keep the line breaks between paragraphs. They make the text readable in
`sqlite3`, and milestone 5's chunker can use them.

> **Loop variables are copies.** In `for _, line := range lines`, assigning
> `line = strings.TrimSpace(line)` changes your copy, not the slice. That's fine
> here, because you append the trimmed copy to a new slice.

---

## Step 4: Test `extract`

Create `internal/extract/extract_test.go`. Give `Extract` a small page with a
nav bar, an article and a footer:

```go
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
```

The test uses `strings.Contains`, not `==`, because readability decides the
exact wording and spacing. What matters is that the article is in and the
boilerplate is out.

**Your task:** add `TestCleanText`. Pass it
`"\n\n  first line  \n\t\n   \nsecond line\n\n"` and check the result is exactly
`"first line\nsecond line"`. `cleanText` is all your code, so here `==` is the
right check.

> Readability needs a few sentences of real text before it believes something
> is an article. If you shrink the test page to one short `<p>`, it may decide
> there's no content.

---

## Step 5: The `store` package: open the database

SQLite is a whole SQL database in a single file, with no server to run. Go
talks to SQL databases through the standard library's `database/sql` package,
which needs a **driver** for the specific database.
`modernc.org/sqlite` is a driver written in pure Go, so there's no C compiler
to install.

```sh
go get modernc.org/sqlite
```

Create `internal/store/store.go`:

```go
// Package store keeps crawled pages in a SQLite database.
package store

import (
	"database/sql"

	_ "modernc.org/sqlite" // registers the "sqlite" driver
)
```

That `_` import is a **blank import**. You never call anything from
`modernc.org/sqlite` directly. Importing it runs its setup code, which tells
`database/sql` "I handle the driver named `sqlite`". The `_` tells the compiler
you're importing it only for that side effect, so it doesn't complain about an
unused import.

**The table.** Define the schema as a constant:

```go
const schema = `
CREATE TABLE IF NOT EXISTS pages (
	id         INTEGER PRIMARY KEY,
	url        TEXT NOT NULL UNIQUE,
	title      TEXT NOT NULL,
	text       TEXT NOT NULL,
	fetched_at TEXT NOT NULL
)`
```

- `IF NOT EXISTS` makes it safe to run every time Butler starts: it creates
  the table the first time and does nothing afterwards.
- `UNIQUE` on `url` means the database itself refuses a second row with the
  same URL. Step 6 relies on this.
- `INTEGER PRIMARY KEY` gives each row a number automatically. Milestone 6
  will use it to link chunks back to their page.

**The `Store` type.** Wrap the database handle in your own type, so the rest
of Butler calls `st.SavePage(...)` and never writes SQL itself:

```go
// Store is an open Butler database.
type Store struct {
	db *sql.DB
}

// Open opens (or creates) the database at path and makes sure the tables exist.
func Open(path string) (*Store, error) {
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, err
	}
	if _, err := db.Exec(schema); err != nil {
		db.Close()
		return nil, fmt.Errorf("creating tables in %s: %w", path, err)
	}
	return &Store{db: db}, nil
}

// Close closes the database.
func (s *Store) Close() error {
	return s.db.Close()
}
```

Notes:

- `sql.Open` doesn't actually open anything yet. It only checks the driver
  name and remembers the path. The file is created and opened on the first
  real query, which here is `db.Exec(schema)`. That's why `Open` runs the
  schema straight away: a bad path fails here, not halfway through a crawl.
- `db.Exec` runs SQL that doesn't return rows. It returns a `sql.Result`
  (how many rows changed, the new row's ID) that you don't need, hence `_`.
- `db` is lowercase, so other packages can't reach around `Store` and run SQL
  directly.
- `func (s *Store) Close() error` is a **method**. The `(s *Store)` before the
  name is the **receiver**: `Close` belongs to `*Store`, and you call it as
  `st.Close()`. Inside, `s` is the store it was called on, like `this` or
  `self` in other languages.
- The receiver is a **pointer** (`*Store`), so the method works on the
  original value rather than a copy. Use pointer receivers for types like this
  one that wrap a resource, and use them for all of the type's methods.

---

## Step 6: Save a page

Add a `Page` type for what the store keeps, and a method to save one:

```go
// Page is one crawled page as stored in the database.
type Page struct {
	URL       string
	Title     string
	Text      string
	FetchedAt time.Time
}

// SavePage inserts p, or replaces the stored copy if its URL is already there.
func (s *Store) SavePage(p Page) error {
	_, err := s.db.Exec(`
		INSERT INTO pages (url, title, text, fetched_at) VALUES (?, ?, ?, ?)
		ON CONFLICT (url) DO UPDATE SET
			title = excluded.title,
			text = excluded.text,
			fetched_at = excluded.fetched_at`,
		p.URL, p.Title, p.Text, p.FetchedAt.UTC().Format(time.RFC3339))
	// wrap a non-nil err: fmt.Errorf("saving %s: %w", p.URL, err)
}
```

`store.Page` and `crawler.Page` are different types that share a name. That's
fine, because outside their packages they're always written with the package
name in front.

**Placeholders.** Each `?` is a **placeholder**. The values after the SQL fill
them in, in order. The driver sends them to SQLite **separately** from the SQL
text, so a value can never be mistaken for SQL. Never build SQL with
`fmt.Sprintf`:

```go
// ❌ Never do this
s.db.Exec(fmt.Sprintf("INSERT INTO pages (title) VALUES ('%s')", p.Title))
```

A page titled `Rock 'n' Roll` breaks that query. A page whose title contains
carefully chosen SQL could make it run something else entirely. This is **SQL
injection**, and Butler stores text from any website it visits. Placeholders
avoid it completely.

**Upsert.** `INSERT ... ON CONFLICT (url) DO UPDATE` is an **upsert**: insert a
new row, or, if the `UNIQUE` URL already exists, update that row instead.
`excluded.title` means "the title from the row you tried to insert". This is
what makes a re-crawl refresh pages instead of failing or duplicating them.

**Times.** SQLite has no date type, so store `fetched_at` as text.
`time.RFC3339` is a standard format, like `2026-10-18T19:39:18Z`. Converting to
UTC first means every row uses the same time zone and the text sorts in time
order.

> **Go's time formats.** `time.RFC3339` is the layout string
> `"2006-01-02T15:04:05Z07:00"`. Go layouts are written using one specific
> moment, Mon Jan 2 15:04:05 MST 2006 (1 2 3 4 5 6 7), instead of codes like
> `%Y-%m-%d`. Use the named constants and you rarely need to think about it.

---

## Step 7: Read pages back

Two more methods, mostly for tests and the summary line:

```go
// CountPages returns how many pages are stored.
func (s *Store) CountPages() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM pages`).Scan(&n)
	return n, err
}
```

`QueryRow` runs a query that returns one row. `Scan` copies that row's columns
into your variables, in order. It takes **pointers** (`&n`) so it can write to
them, the same idea as `fs.IntVar(&opts.MaxPages, ...)` in milestone 3.

```go
// ErrNotFound is returned by GetPage when no page has that URL.
var ErrNotFound = errors.New("page not found")

// GetPage returns the stored page for url, or ErrNotFound.
func (s *Store) GetPage(url string) (Page, error)
```

**Your task:** write `GetPage`.

1. `QueryRow` with
   `SELECT url, title, text, fetched_at FROM pages WHERE url = ?` and `url`.
2. `Scan` into `&p.URL, &p.Title, &p.Text, &fetchedAt`, where `fetchedAt` is a
   `string`. `Scan` can't turn the stored text into a `time.Time` for you.
3. If the error is `sql.ErrNoRows`, return `Page{}, ErrNotFound`. Check with
   `errors.Is(err, sql.ErrNoRows)`.
4. Any other error: return it.
5. `time.Parse(time.RFC3339, fetchedAt)` turns the text back into a
   `time.Time`.

`ErrNotFound` is a **sentinel error**: a fixed error value that callers can
check with `errors.Is(err, store.ErrNotFound)`. It's the same pattern as
`io.EOF` and `sql.ErrNoRows`. Returning your own instead of `sql.ErrNoRows`
means callers don't need to know the store uses `database/sql` underneath.

---

## Step 8: Test the store

`t.TempDir()` gives each test a fresh folder that's deleted when the test
ends, so tests never touch your real `butler.db`. Create
`internal/store/store_test.go`:

```go
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

	// your checks here
}
```

`filepath.Join` builds a file path with the right separator for the OS.

**Your task:** finish the test so it checks:

1. Saving the same URL again with `p.Text = "second"` works, and
   `CountPages` is still 1.
2. `GetPage(p.URL)` returns the **second** version. You can compare whole
   structs with `!=` when all their fields are comparable:
   `if got != p { t.Errorf("GetPage = %+v, want %+v", got, p) }`. `%+v` prints
   a struct with its field names.
3. `GetPage` for a URL that was never saved returns an error where
   `errors.Is(err, ErrNotFound)` is true.

⚠️ The test uses `time.Date(...)` and not `time.Now()` on purpose. `time.Now()`
has nanoseconds, and RFC 3339 text keeps only whole seconds, so the time you
read back would never be `==` the one you saved.

---

## Step 9: Wire it up in `main.go`

`main.go`'s `"crawl"` case is getting long, and it now needs a `defer`. There's
a catch:

⚠️ **`os.Exit` skips deferred calls.** It ends the program immediately, so
with

```go
st, err := store.Open(...)
defer st.Close()
if err := crawler.Crawl(...); err != nil {
	os.Exit(1) // st.Close() never runs
}
```

the database is never closed properly. The usual fix is to move the work into
a function that **returns** an error, and keep `os.Exit` in `main`:

```go
case "crawl":
	if err := runCrawl(args); err != nil {
		fmt.Fprintln(os.Stderr, "crawl:", err)
		os.Exit(1)
	}
```

When `runCrawl` returns, its deferred `st.Close()` runs, and only then does
`main` exit.

**Your task:** write `runCrawl` in `main.go`:

```go
// runCrawl handles "butler crawl": it crawls a site and saves each page's text.
func runCrawl(args []string) error
```

1. Move the FlagSet code into it. Add a flag for the database file:
   ```go
   dbPath := fs.String("db", "butler.db", "SQLite database file")
   ```
   `fs.String` returns a `*string`. Read the value with `*dbPath` after
   `fs.Parse`. The `*` in front of a pointer **dereferences** it: "the value
   this points to".
2. Keep the `fs.NArg() != 1` check with its `os.Exit(1)`. Nothing is deferred
   yet at that point, so exiting is fine.
3. `st, err := store.Open(*dbPath)`, return the error, `defer st.Close()`.
4. Count saved pages in a `saved` variable, and call `crawler.Crawl` with a
   function literal:
   ```go
   saved := 0
   err = crawler.Crawl(fs.Arg(0), opts, func(p crawler.Page) error {
   	title, text, err := extract.Extract(p.URL, p.Body)
   	// error? print "  not saved: <url>: <err>" and return nil
   	// text == ""? print "  not saved: <url>: no text found" and return nil
   	saved++
   	return st.SavePage(store.Page{URL: p.URL, Title: title, Text: text, FetchedAt: time.Now()})
   })
   ```
   This closure uses `st` and changes `saved`, both from `runCrawl`.
   An extraction failure returns `nil`, so the crawl carries on without that
   page. A save failure returns the error, so the crawl stops.
5. After the crawl, print
   `saved 4 pages to butler.db (4 in total)` using `saved`, `*dbPath` and
   `st.CountPages()`.

`main.go` now imports `butler/internal/extract` and `butler/internal/store`.

**Ignore the database in git.** Add `butler.db` to `.gitignore` as a new line.
It's your data, not source code, and it can get big.

Then run the **Done when** commands. Run the second crawl to check that the
total doesn't go up.

---

## Errors you'll likely see

| Error / symptom | Meaning |
|---|---|
| `sql: unknown driver "sqlite" (forgotten import?)` | The `_ "modernc.org/sqlite"` import is missing from `store.go`. |
| `"modernc.org/sqlite" imported and not used` | You wrote the import without the `_ `. |
| `not enough arguments in call to Crawl` | `crawl_test.go` (or `main.go`) still uses the old two-argument `Crawl`. |
| `assignment mismatch: 2 variables but Crawl returns 1 value` | `Crawl` returns only `error` now: `err := Crawl(...)`. |
| `UNIQUE constraint failed: pages.url` | The `ON CONFLICT ... DO UPDATE` part is missing, so a re-crawl tries to insert a duplicate. |
| `sql: expected 4 arguments, got 3` (or similar) | The number of `?` and the number of values after the SQL don't match. |
| `sql: Scan error ... converting ... to time.Time` | Scan `fetched_at` into a `string`, then `time.Parse` it. |
| `GetPage` test fails, times differ by nanoseconds | The test uses `time.Now()`. Use a fixed `time.Date(...)` in UTC. |
| Every page says `not saved: ... no text found` | You're passing `article.Content` (HTML) or the wrong variable to `cleanText`. Use `TextContent`. |
| `butler.db` shows up in `git status` | Add it to `.gitignore`. |

---

## Check yourself

- Why does `Crawl` take a function instead of returning a `[]Page`?
- What makes a function literal a closure? Which variables does the one in
  `runCrawl` capture?
- What does the `_` in `_ "modernc.org/sqlite"` do? What happens without the
  import at all?
- Why does `Open` run the schema immediately instead of waiting for the first
  save?
- What's wrong with building SQL with `fmt.Sprintf`? What do `?`
  placeholders do differently?
- What does `ON CONFLICT (url) DO UPDATE` need from the schema to work?
- Why does `Scan` take `&n` and not `n`?
- Why move the crawl code out of `main` into `runCrawl`?

When it's working, send me `main.go`, `crawler.go`, `extract.go`, `store.go`
and the three test files for review. Milestone 5 splits each page's text into
overlapping chunks, ready to be embedded.

<details>
<summary>Stuck? Skeleton of <code>GetPage</code> (spoiler, not the full answer)</summary>

```go
func (s *Store) GetPage(url string) (Page, error) {
	var p Page
	var fetchedAt string
	err := s.db.QueryRow(`SELECT ... WHERE url = ?`, url).Scan(/* four pointers */)
	if errors.Is(err, sql.ErrNoRows) {
		return Page{}, ErrNotFound
	}
	if err != nil {
		return Page{}, err
	}
	// p.FetchedAt, err = time.Parse(time.RFC3339, fetchedAt)
	// error? return it
	return p, nil
}
```

Imports for `store.go`: `database/sql`, `errors`, `fmt`, `time`,
`_ "modernc.org/sqlite"`.

</details>

<details>
<summary>Stuck? Skeleton of <code>Extract</code> (spoiler, not the full answer)</summary>

```go
func Extract(pageURL string, body []byte) (title, text string, err error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", "", err
	}

	article, err := readability.FromReader(bytes.NewReader(body), u)
	// error? wrap it with "readability: %w"

	return strings.TrimSpace(article.Title), cleanText(article.TextContent), nil
}
```

Imports for `extract.go`: `bytes`, `fmt`, `net/url`, `strings`,
`readability "github.com/go-shiori/go-readability"`.

</details>
