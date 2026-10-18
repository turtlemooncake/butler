# Milestone 3: Crawl a site

**Goal:** `butler crawl` turns into a real crawler. From the start URL it works
outward, breadth-first: the start page first, then every page it links to, then
every page *those* link to, and so on. It stays on the start URL's host, stops
at a page limit and a depth limit, waits between requests, sends its own
User-Agent, and obeys the site's `robots.txt`.

**Done when:**

```sh
$ go run . crawl -max-pages 6 -max-depth 1 -delay 500ms http://go.dev
[1/6] depth 0  https://go.dev/  (93 links)
[2/6] depth 1  https://go.dev/solutions/case-studies  (75 links)
[3/6] depth 1  https://go.dev/solutions/use-cases  (52 links)
[4/6] depth 1  https://go.dev/doc/security/  (71 links)
[5/6] depth 1  https://go.dev/learn/  (70 links)
[6/6] depth 1  https://go.dev/ref/spec  (59 links)
done: 6 fetched, 0 errors, 35 still queued
```

(The numbers will change as the site changes.) A URL with flags after it is a
usage error:

```sh
$ go run . crawl https://go.dev -max-pages 3
usage: butler crawl [flags] <url>
  -delay duration
    	wait between requests (default 1s)
  -max-depth int
    	how many links away from the start URL to go (default 3)
  -max-pages int
    	stop after fetching this many pages (default 50)
$ echo $?
1
```

The milestone 1 errors (`crawl example.com`, a 404 start page, a host that
doesn't exist) still print an error and exit 1.

```sh
$ go test ./...
ok  	butler/internal/crawler	0.509s
```

…with a new test that crawls a fake site (Step 9) and checks which pages were
visited, in what order, and that a page `robots.txt` disallows was never
fetched.

**Concepts:** the `flag` package (`FlagSet`, `IntVar`, `DurationVar`), passing
pointers so a function can fill in your variables, an options struct, a struct
type for queue entries, a slice as a queue, breadth-first search, maps as sets
(again, but now the timing matters), `time.Duration` and `time.Sleep`, package
constants, `robots.txt`, `httptest` with several routes.

---

## How the pieces fit

```
main:   flags ─> crawler.Options{MaxPages, MaxDepth, Delay}
                    │
Crawl(start, opts)  ▼
  ├─ parse start ─> site (its Host is the only host we visit)
  ├─ fetchRobots(site) ─> rules; start disallowed? return an error   ← robots.go
  ├─ queue := [{start, depth 0}]     seen := {start}
  │
  └─ while queue not empty and fetched < MaxPages:
        item := front of queue
        sleep(delay)  (not before the first request)
        page := fetch(item.url) ─── error? print it, carry on
        page.URL on another host? (redirected off-site) skip it
        not HTML? no links, carry on
        links := extractLinks(...)
        item.depth == MaxDepth? don't follow its links
        for each link: seen? other host? disallowed?  → skip
                       else mark seen, push {link, item.depth+1} on the back
```

`fetch` and `extractLinks` don't change. Milestone 3 is all about what happens
**between** them.

---

## Step 1: Options and flags

Crawl now needs settings, so group them in a struct in `crawler.go`:

```go
// Options limits a crawl.
type Options struct {
	MaxPages int
	MaxDepth int
	Delay    time.Duration
}
```

Fill it from command-line flags in `main.go`. Go's `flag` package handles
`-max-pages 50` (and `--max-pages 50` and `-max-pages=50`), parses the values,
and prints help. Because `crawl` is a subcommand, give it its own **FlagSet**:

```go
var opts crawler.Options
fs := flag.NewFlagSet("crawl", flag.ExitOnError)
fs.IntVar(&opts.MaxPages, "max-pages", 50, "stop after fetching this many pages")
fs.IntVar(&opts.MaxDepth, "max-depth", 3, "how many links away from the start URL to go")
fs.DurationVar(&opts.Delay, "delay", time.Second, "wait between requests")
fs.Parse(args) // args is os.Args[2:], everything after "crawl"
```

Then `fs.NArg()` is how many non-flag arguments are left, and `fs.Arg(0)` is
the first of them: the URL.

Notes:

- `&opts.MaxPages` is a **pointer** to that field. `IntVar` stores the pointer,
  and when `fs.Parse` finds `-max-pages 6`, it writes 6 through it. That's how a
  function can fill in a variable that belongs to you. (There's also `fs.Int`,
  which returns a new `*int` instead. `IntVar` fits better when you already have
  a struct to fill.)
- `DurationVar` parses strings like `500ms`, `2s` or `1m30s` into a
  `time.Duration`.
- `flag.ExitOnError` means a bad flag (`-max-pages lots`) prints the error and
  the help, then exits with status 2. You don't need to check `Parse`'s error.

⚠️ **Flags must come before the URL.** `flag` stops at the first argument that
isn't a flag. So with `crawl https://go.dev -max-pages 3`, it sees the URL,
stops, and leaves *three* arguments: the URL, `-max-pages` and `3`. Many CLI
tools allow either order, but the standard `flag` package doesn't.

**Your task:** in the `"crawl"` case, replace the `len(args) != 1` check with
the FlagSet above. If `fs.NArg() != 1`, print
`usage: butler crawl [flags] <url>`, then `fs.PrintDefaults()` (which lists the
flags), and exit 1. Pass `fs.Arg(0)` and `opts` to `Crawl`. `main.go` now also
imports `flag` and `time`.

---

## Step 2: The new `Crawl`

The shape changes to:

```go
// Crawl fetches pages breadth-first from start, staying on start's host.
// It returns the URLs it fetched successfully, in order.
func Crawl(start string, opts Options) ([]string, error)
```

The `[]string` result is mainly for the test in Step 9. `main` can ignore it
with `_, err := crawler.Crawl(...)`. (Milestone 4 will hand back the pages
themselves so they can be saved.)

The first thing `Crawl` does is parse `start` and check it, just as `fetch`
does. `fetch` keeps its own check, but `Crawl` needs the parsed URL right away:
its `Host` decides what counts as "this site", and robots.txt (Step 7) lives on
that host.

```go
site, err := url.Parse(start)
if err != nil || (site.Scheme != "http" && site.Scheme != "https") || site.Host == "" {
	return nil, fmt.Errorf("invalid URL %q: must start with http:// or https://", start)
}
```

A function that returns `([]string, error)` returns `nil, err` on failure. A
nil slice is the "zero value" for slices, just like `Page{}` was for `Page`.

---

## Step 3: The queue and the seen set

Each queue entry needs a URL **and** how deep it is, so make a small struct
type. It's only used inside `Crawl`, so it's lowercase:

```go
type queued struct {
	url   string
	depth int
}
```

The start page is depth 0, pages it links to are depth 1, and so on. Use a
slice as a **queue**: take from the front, add to the back.

```go
queue := []queued{{url: start, depth: 0}}
seen := map[string]bool{start: true}

for len(queue) > 0 {
	item := queue[0]   // look at the front
	queue = queue[1:]  // and drop it
	// ... fetch item.url, then for new links:
	queue = append(queue, queued{url: link, depth: item.depth + 1})
}
```

Syntax notes:

- `[]queued{{url: start, depth: 0}}` is a slice literal with one struct inside.
  Inside a slice literal you can leave out the type name on each element:
  `{...}` instead of `queued{...}`.
- `map[string]bool{start: true}` creates a map that already has one key.
- `queue[1:]` is a **slice expression**: everything from index 1 to the end.
  It doesn't copy anything. It's a new view onto the same underlying array, so
  it's cheap.

**Why breadth-first?** Taking from the **front** means you finish everything at
depth 1 before starting depth 2. When `MaxPages` cuts the crawl short, you've
got the pages closest to the start, which are usually the most important ones.
Taking from the **back** instead (a stack) would dive down the first link's
links, then their links, and might spend the whole budget in one corner of the
site.

**When to mark a URL as seen.** Mark it when you **add it to the queue**, not
when you fetch it. Say `/` links to `/a` and `/b`, and `/a` also links to `/b`.
If you only marked pages seen after fetching them, `/b` would be in the queue
twice by the time you got to it.

---

## Step 4: Limits, and errors that don't stop the crawl

**Max pages.** Count the fetches you make (successful or not, since each one is
a request to the site) and add the limit to the loop condition:

```go
fetched, failed := 0, 0
for len(queue) > 0 && fetched < opts.MaxPages {
```

`fetched, failed := 0, 0` declares two variables at once.

**Max depth.** A page at depth `MaxDepth` still gets fetched, but its links
aren't followed. After extracting links:

```go
if item.depth >= opts.MaxDepth {
	continue // the next loop iteration, skipping the link-queueing below
}
```

So `-max-depth 0` fetches only the start page, and `-max-depth 1` fetches the
start page plus the pages it links to.

**Errors.** In a crawl of 200 pages, a few will be 404s or time out. That
shouldn't end the crawl. Print it, count it and move on:

```go
page, err := fetch(item.url)
if err != nil {
	if item.depth == 0 {
		return nil, err // the start page itself failed: nothing to crawl
	}
	fmt.Printf("  error: %v\n", err)
	failed++
	continue
}
```

Keeping the start page's error fatal means the milestone 1 checks
(`crawl https://example.com/does-not-exist`) still exit 1.

---

## Step 5: Stay on the site

Before queueing a link, parse it and compare hosts:

```go
u, err := url.Parse(link)
if err != nil || u.Host != site.Host {
	continue
}
```

`extractLinks` already gave you absolute URLs, so this `url.Parse` (the plain
**function**, not `base.Parse`) is enough.

**Redirects.** A same-host URL can still redirect somewhere else. After a
successful fetch:

1. Mark `page.URL` (where the request ended up) as seen too, so that you won't
   fetch it again under its real name. For example, `https://go.dev/security/`
   redirects to `https://go.dev/doc/security/`.
2. Parse `page.URL`. If its host isn't `site.Host`, print a `skip:` line and
   `continue`. Don't follow links from a page on someone else's site.

The parsed `page.URL` is also the `base` that `extractLinks` needs, as it was in
milestone 2.

⚠️ `example.com` and `www.example.com` are **different hosts**. If your start
URL redirects to `www.`, the start page is skipped as off-site. Start the crawl
from the `www.` URL instead. (Treating them as the same is a nice extension once
everything works.)

---

## Step 6: Be polite: delay and User-Agent

You're about to make lots of requests to someone else's server. Two habits keep
crawlers welcome:

**Wait between requests.** Sleep before every fetch except the first:

```go
if fetched > 0 {
	time.Sleep(delay)
}
fetched++
```

`time.Sleep` takes a `time.Duration` and pauses the program for that long.
The default 1 second is a good place to start. (Where `delay` comes from is
Step 7. For now, use `opts.Delay`.)

**Say who you are.** `fetch` sets the User-Agent to a string literal. Make it a
package constant, since robots.txt needs it too:

```go
const userAgent = "butler/0.1"
```

…and use `userAgent` in `fetch`. Site owners read their logs; a clear
User-Agent lets them see who's crawling, and lets their robots.txt give rules
to Butler specifically.

At this point the crawler works without robots.txt. Try it before moving on:

```sh
go run . crawl -max-pages 5 -max-depth 1 https://go.dev/doc/
```

---

## Step 7: robots.txt

Sites put crawling rules in `/robots.txt` at the root of the host. For example,
https://go.dev/robots.txt. A typical file:

```
User-agent: *
Disallow: /private/
Crawl-delay: 2

User-agent: BadBot
Disallow: /
```

Each `User-agent` line starts a **group** of rules. A crawler uses the group
that matches its name most closely, falling back to `*`. `Disallow` lines list
path prefixes it must not fetch, and `Crawl-delay` asks for a minimum number of
seconds between requests.

Parsing this properly has fiddly edge cases (wildcards, `Allow` overriding
`Disallow`, which group matches), so use a library:

```sh
go get github.com/temoto/robotstxt
```

The parts you need:

| API | What it does |
|---|---|
| `robotstxt.FromResponse(resp)` | Reads and parses an `*http.Response` for robots.txt. Returns `(*robotstxt.RobotsData, error)`. |
| `data.FindGroup(userAgent)` | Picks the group for your User-Agent. Returns a `*robotstxt.Group`. |
| `group.Test(path)` | `true` if you may fetch `path`. |
| `group.CrawlDelay` | The `Crawl-delay` as a `time.Duration` (0 if there isn't one). |

`FromResponse` also handles the status code the way Google does:

- **2xx:** parse the rules.
- **4xx** (the file doesn't exist): no rules, everything allowed.
- **5xx** (the server is having trouble): **nothing** allowed. Better to back
  off than to hammer a struggling site.

That's why you can't reuse `fetch` here: `fetch` turns a 404 into an error, but
for robots.txt a 404 is a normal, meaningful answer.

**Your task:** create `internal/crawler/robots.go` with:

```go
// fetchRobots downloads site's /robots.txt and returns the rules for Butler.
func fetchRobots(site *url.URL) (*robotstxt.Group, error)
```

1. Build the URL: `site.Scheme + "://" + site.Host + "/robots.txt"`.
2. Make the request like `fetch` does: `http.NewRequest`, set the
   User-Agent, `client.Do`, wrap a network error with `GET %s: %w`, and
   `defer resp.Body.Close()` after the error check.
3. `robotstxt.FromResponse(resp)`. Wrap an error as `parsing %s: %w`.
4. Return `data.FindGroup(userAgent)`.

Then `go mod tidy`, as in milestone 2.

**Using it in `Crawl`**, right after validating `start`:

1. `robots, err := fetchRobots(site)`. Return the error if there is one.
2. If `!robots.Test(site.RequestURI())`, return an error:
   `robots.txt disallows %s`.
3. Use the larger of `opts.Delay` and `robots.CrawlDelay` as the delay.
4. In the link loop, also skip links where `!robots.Test(u.RequestURI())`.

`u.RequestURI()` is the path plus the query string, like `/search?q=go`, which
is the part of the URL that robots.txt rules match against.

This only covers `site.Host`, and that's all you need, because the crawler never
leaves it.

---

## Step 8: Output

Replace milestone 2's detailed printout with one line per page:

```
[3/6] depth 1  https://go.dev/solutions/use-cases  (52 links)
```

That's `fetched`, `opts.MaxPages`, `item.depth`, `page.URL` and `len(links)`.
For a non-HTML page, print `(not HTML)` instead of the link count, and don't
look for links. After the loop, print a summary:

```
done: 6 fetched, 0 errors, 35 still queued
```

Append each successfully fetched same-host `page.URL` to a `crawled` slice, and
`return crawled, nil` at the end.

---

## Step 9: Test it against a fake site

`httptest.NewServer` can serve a whole little site, robots.txt included.
Create `internal/crawler/crawl_test.go`:

```go
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
		"/b":          `<a href="/c">c</a>`,
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
		// your cases here
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			requested = nil
			got, err := Crawl(srv.URL+"/", tc.opts)
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
```

Notes:

- `body, ok := pages[path]` is the **comma-ok** form of a map lookup. `ok` is
  `false` if the key is missing, which tells "not there" apart from "there, but
  empty".
- The handler is a **closure**: a function written inline that can use
  `pages` and `requested` from the test around it. More on these in milestone 7.
- `srv.URL` is something like `http://127.0.0.1:52817`. Trimming it off makes
  the expected values short and readable.
- `Delay` is left out of every case, so it's 0, and the test runs instantly.
- `for i := range got` gives only the index, which you need in order to change
  the slice's elements in place.

**Add cases for:**

| Case | Options | Want |
|---|---|---|
| depth 1 | `MaxPages: 10, MaxDepth: 1` | `/`, `/a` |
| max pages | `MaxPages: 2, MaxDepth: 5` | `/`, `/a` |
| broken link | `MaxPages: 10, MaxDepth: 5`, with `/b` also linking to `/missing` | `/`, `/a`, `/b`, `/c` (the 404 doesn't stop the crawl) |

For the last one, the simplest way is to add `<a href="/missing">m</a>` to `/b`
in the `pages` map. Check that the earlier cases still pass, since `/missing`
is at the same depth as `/c`.

---

## Errors you'll likely see

| Error / symptom | Meaning |
|---|---|
| `usage: butler crawl [flags] <url>` even though you gave a URL | The flags come after the URL. Put them first. |
| `flag provided but not defined: -max_pages` | The flag names use dashes: `-max-pages`. |
| `invalid value "5" for flag -delay` | Durations need a unit: `5s`, `500ms`. |
| `cannot use opts.MaxPages (variable of type int) as *int value` | `IntVar` wants a pointer: `&opts.MaxPages`. |
| The same page is fetched twice | You mark URLs seen when fetching instead of when queueing, or you forgot to mark `page.URL` after a redirect. |
| Crawl wanders onto other sites | The host check is missing, or it compares whole URLs instead of `.Host`. |
| `-max-depth 0` fetches more than one page | The depth check runs before fetching instead of before queueing links, or it uses `>` instead of `>=`. |
| Crawl stops at the first 404 | You `return` the error instead of printing and `continue`-ing (except at depth 0). |
| Test fails with `fetched /private/x` | Links aren't checked with `robots.Test` before they're queued. |
| `undefined: robotstxt` | Missing the import `"github.com/temoto/robotstxt"`, or `go get` wasn't run. |

---

## Check yourself

- What's the difference between taking from the front of the queue and taking
  from the back? Why does it matter when `MaxPages` cuts the crawl short?
- Why mark a URL as seen when it's queued rather than when it's fetched?
- Why does `IntVar` take `&opts.MaxPages` and not `opts.MaxPages`?
- What does `queue = queue[1:]` do to the underlying array? Does it copy?
- Why can't `fetchRobots` just call `fetch`?
- What should a crawler do if robots.txt returns 404? And 503? Why are those
  different?
- A same-host link redirects to another host. What does your crawler do?

When it's working, send me `main.go`, `crawler.go`, `robots.go` and
`crawl_test.go` for review. Milestone 4 extracts each page's main text and
saves it to SQLite, so the crawl actually keeps something.

<details>
<summary>Stuck? Skeleton of <code>Crawl</code> (spoiler, not the full answer)</summary>

```go
func Crawl(start string, opts Options) ([]string, error) {
	// site := url.Parse(start) + checks
	// robots := fetchRobots(site); start allowed?
	// delay := max(opts.Delay, robots.CrawlDelay)

	queue := []queued{{url: start, depth: 0}}
	seen := map[string]bool{start: true}
	var crawled []string
	fetched, failed := 0, 0

	for len(queue) > 0 && fetched < opts.MaxPages {
		item := queue[0]
		queue = queue[1:]

		// sleep unless this is the first fetch; fetched++
		// page, err := fetch(item.url)
		//   error: depth 0 → return it; else print, failed++, continue
		// seen[page.URL] = true
		// final := url.Parse(page.URL); other host? print skip, continue
		// crawled = append(crawled, page.URL)
		// not HTML? print, continue
		// links := extractLinks(final, page.Body); print the page line
		// item.depth >= opts.MaxDepth? continue
		for _, link := range links {
			// seen? continue
			// u := url.Parse(link); error, other host, or disallowed? continue
			// mark seen, append queued{link, item.depth + 1}
		}
	}

	// print the summary
	return crawled, nil
}
```

`max(a, b)` is built into Go (since 1.21) and works on `time.Duration` too.

</details>
