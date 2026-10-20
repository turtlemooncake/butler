# Milestone 8: Polish

**Goal:** four small improvements that make Butler pleasant to use every day:

1. **Streaming answers.** `ask` prints the answer as the model writes it,
   instead of after a long silence.
2. **A concurrent crawl.** Slow pages no longer hold up the rest.
   `-workers` sets how many pages are fetched at once.
3. **`butler refresh`** crawls every site you've crawled before, so your
   knowledge bank stays up to date.
4. **`butler stats`** shows what's in the database.

Each part is short, and they can be done in any order except that `stats`
uses the `sites` table from part 3.

**Done when:**

```sh
$ go run . ask what is a slice in Go
A slice in Go is a descriptor for a contiguous segment of an underlying     ← appears word
array [2]. Its length can change while the program runs ...                  by word

Sources:
  [1] 0.70  https://go.dev/ref/spec
  ...

$ go run . crawl -max-pages 3 -max-depth 1 -delay 500ms -workers 4 https://go.dev/doc/
[1/3] depth 0  https://go.dev/doc/  (137 links)
[2/3] depth 1  https://go.dev/  (93 links)
[3/3] depth 1  https://go.dev/solutions/case-studies  (75 links)
done: 3 fetched, 0 errors, 108 still queued
saved 3 pages to butler.db, 0 new or changed (7 in total)

$ go run . refresh -max-pages 20
refreshing https://go.dev/doc/
[1/20] depth 0  https://go.dev/doc/  (137 links)
...
refreshed 1 sites, 2 pages new or changed
run "butler index" to embed them

$ go run . stats
pages:   7 (0 not indexed yet)
chunks:  263
sites:   1
oldest:  2026-10-18 12:39:17 (31 hours ago)
     7  go.dev
```

(Your numbers will differ.)

```sh
$ go test -race ./...
?   	butler	[no test files]
ok  	butler/internal/chunk	1.749s
ok  	butler/internal/crawler	2.256s
ok  	butler/internal/extract	1.455s
ok  	butler/internal/ollama	3.007s
ok  	butler/internal/rag	2.460s
ok  	butler/internal/store	2.821s
```

`-race` turns on the **race detector**. From now on, run your tests with it.

**Concepts:** reading a stream of JSON values, passing a function literal
as a callback, goroutines, `sync.WaitGroup`, loop variables in closures,
data races, `sync.Mutex`, `go test -race`, rate limiting with
`time.Sleep`, sharing flag definitions between subcommands, `ON CONFLICT DO
NOTHING`, `maps.Keys` and `slices.Sorted`, `time.Since`.

---

## Part 1: Streaming answers

### Step 1: Read the stream

In milestone 7 you turned streaming off, because a single `Decode` reads only
the first JSON value of the stream. Now turn it on and keep reading. Ollama
sends one JSON object per line, and the last one has `"done": true`:

```
{"message":{"role":"assistant","content":"A"},"done":false}
{"message":{"role":"assistant","content":" slice"},"done":false}
...
{"message":{"role":"assistant","content":""},"done":true}
```

A `json.Decoder` reads values from a stream **one after another**. Call
`Decode` in a loop and each call returns the next object. The newlines
between them don't matter.

First, split `post` in two, so `Chat` can get at the response body itself:

```go
// send posts in as JSON to path and returns the response if its status is OK.
// The caller must close the response body.
func (c *Client) send(path string, in any) (*http.Response, error)
```

`send` is the first half of `post`: marshal, `http.Post`, check the status.
On a bad status it closes the body itself (`defer resp.Body.Close()` inside
the `if`) before returning `apiError(resp)`. `post` becomes `send`, then
`defer resp.Body.Close()`, then `Decode`. `Embed` doesn't change.

Then add two fields to `chatResponse`:

```go
type chatResponse struct {
	Message Message `json:"message"`
	Done    bool    `json:"done"`
	Error   string  `json:"error"` // Ollama can fail partway through
}
```

And give `Chat` a function to call with each piece, instead of returning a
string:

```go
// Chat sends messages to model and calls onText with each piece of the
// reply as it arrives.
func (c *Client) Chat(model string, messages []Message, onText func(string)) error {
	resp, err := c.send("/api/chat", chatRequest{Model: model, Messages: messages, Stream: true})
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	dec := json.NewDecoder(resp.Body)
	got := 0
	for {
		var part chatResponse
		if err := dec.Decode(&part); err != nil {
			return fmt.Errorf("reading reply from %s: %w", model, err)
		}
		// part.Error set? return it as "ollama: ..."
		onText(part.Message.Content)
		got += len(part.Message.Content)
		if part.Done {
			break
		}
	}
	// got == 0? return the "empty reply" error
	return nil
}
```

If the connection drops before `done`, `Decode` returns `io.EOF` (or
`unexpected EOF`), so a cut-off answer is an error, not a silent half answer.

`onText func(string)` is the same idea as `Crawl`'s `handle func(Page)
error` in milestone 4: the caller decides what happens with each piece.

### Step 2: Print as it arrives

In `runAsk`:

```go
err = client.Chat(*model, messages, func(s string) { fmt.Print(s) })
fmt.Println()
if err != nil {
	return fmt.Errorf("asking %s: %w", *model, err)
}
```

You can't pass `fmt.Print` directly. Its type is `func(...any) (int,
error)`, not `func(string)`, so the function literal wraps it.

`os.Stdout` isn't buffered in Go, so each piece shows up as soon as it's
printed. You lose `strings.TrimSpace` here, which is fine: you can't trim a
string you haven't finished receiving.

### Step 3: Test it

Update the chat tests in `ollama_test.go`. A fake server can stream simply by
writing several lines:

```go
w.Write([]byte(`{"message":{"content":"hi"},"done":false}
{"message":{"content":" [1]"},"done":false}
{"message":{"content":""},"done":true}
{"message":{"content":"after done"},"done":false}
`))
```

**Your task:**

1. `TestChat`: collect the pieces in a slice with your `onText` function and
   check they join to `"hi [1]"`. The line after `done` must be ignored.
   Check that the request now has `"stream": true` and still has
   `"think": false`.
2. `TestChatErrors`, table-driven, with one case per error. Each case gives a
   status code, a reply and a string the error must contain:

   | Case | Status | Reply | Error contains |
   |---|---|---|---|
   | missing model | 404 | `{"error":"model 'nope' not found"}` | `not found` |
   | error mid-stream | 200 | a normal line, then `{"error":"out of memory"}` | `out of memory` |
   | empty reply | 200 | `{"message":{"content":""},"done":true}` | `empty reply` |
   | cut off | 200 | one line with `"done":false`, then nothing | `EOF` |

---

## Part 2: A concurrent crawl

So far Butler fetches one page, waits for it, then fetches the next. If a page
takes 3 seconds, everything waits 3 seconds. With **goroutines**, Butler can
wait for several pages at once.

Two limits keep this polite:
- **`-delay`** still spaces out the *starts* of requests. Butler never sends
  more than one request per delay, however many workers there are.
- **`-workers`** caps how many requests are open at the same time.

So with a 1-second delay and fast pages, you won't see much speed-up. The
gain is when pages are slow: their waiting now overlaps.

### Step 4: Goroutines and `WaitGroup`

```go
go fetch(u) // runs fetch in a new goroutine; this line returns immediately
```

A **goroutine** is a function running at the same time as the rest of your
program. Goroutines are cheap: thousands are fine. `go f()` starts one, and
your code carries on without waiting.

To wait for a group of them, use a `sync.WaitGroup`:

```go
var wg sync.WaitGroup
for _, u := range urls {
	wg.Go(func() {
		// runs in its own goroutine
	})
}
wg.Wait() // blocks until every function passed to wg.Go has returned
```

`wg.Go` (Go 1.25+) starts the goroutine and counts it. You'll see the older
form, `wg.Add(1)` + `go func() { defer wg.Done(); ... }()`, in a lot of
existing code. It does the same thing.

**Getting results back.** A goroutine can't `return` a value to you. The
simplest way is a slice with one slot per goroutine. Each goroutine writes
only to its own index, so they never touch the same memory:

```go
results := make([]result, len(batch))
for i, item := range batch {
	wg.Go(func() {
		results[i].page, results[i].err = fetch(item.url)
	})
}
wg.Wait()
// now every results[i] is filled in
```

> **Loop variables in closures.** The function literal captures `i` and
> `item`. Since Go 1.22, every loop iteration gets **new** `i` and `item`
> variables, so each goroutine sees its own. Before 1.22, they all shared one
> variable, and every goroutine would often see the last item. You'll find
> old code with `item := item` lines working around this. You don't need them.

You might have expected **channels** here. They're Go's other tool for
goroutines: a pipe that one goroutine sends values into and another receives
from. They're the right fit when results arrive over time and need handling
as they come. Here you wait for the whole batch anyway, so a slice is simpler.

### Step 5: Fetch in batches

Add a field to `Options`:

```go
Workers int // how many pages to fetch at once; 0 means 1
```

and a small type in `crawler.go`:

```go
// result is what fetching one queued URL gave.
type result struct {
	page Page
	err  error
}
```

Now change the top of `Crawl`'s loop. Instead of taking one item from the
queue, take up to `workers` items, fetch them all at once, then handle them
one by one **in queue order**, exactly as before:

```go
workers := max(opts.Workers, 1)

for len(queue) > 0 && fetched < opts.MaxPages {
	n := min(workers, len(queue), opts.MaxPages-fetched)
	batch := queue[:n]
	queue = queue[n:]

	// Fetch the whole batch at once, starting one request per delay.
	results := make([]result, n)
	var wg sync.WaitGroup
	for i, item := range batch {
		if fetched > 0 {
			time.Sleep(delay)
		}
		fetched++
		wg.Go(func() {
			results[i].page, results[i].err = fetch(item.url)
		})
	}
	wg.Wait()

	// Then handle the results one by one, in queue order.
	for i, item := range batch {
		page, err := results[i].page, results[i].err
		handled++
		// ... the rest of your old loop body, unchanged
	}
}
```

**Your task:** make that change. Your old loop body moves into the inner
`for`, one level deeper. Delete its `queue[0]`, `time.Sleep` and `fetch`
lines, since they've moved to the batch part. Keep everything else. Its
`continue`s now skip to the next item in the batch, which is what you want.

Notes:

- **`max(opts.Workers, 1)`**: the tests (and any caller) may leave `Workers`
  at 0. `min(0, ...)` would make every batch empty, and the loop would never
  end.
- **`handled`** is a new counter for the `[2/3]` line. `fetched` is already
  at the end of the batch by the time you print, so it's no longer the
  right number. Declare it next to `fetched` and print it instead.
- **Only the fetching runs concurrently.** `seen`, `queue`, `failed` and
  `handle` are used only in the second loop, by one goroutine, so they need no
  protection. Keeping shared state in one goroutine is the easiest way to
  stay safe.
- **Order is unchanged.** The batch comes from the front of the queue and
  new links still go on the back, so the crawl is still breadth-first. Your
  existing `TestCrawl` should pass without a change.

Add the flag in `main.go`:

```go
fs.IntVar(&opts.Workers, "workers", 4, "how many pages to fetch at once")
```

and change `-delay`'s help to `"wait between starting requests"`, which is
what it means now.

### Step 6: Test it, and meet the race detector

A **data race** is two goroutines using the same variable at the same time,
where at least one of them writes to it. The result is unpredictable: a lost
update, a corrupted slice, a crash once in a thousand runs. The race detector
watches memory as your tests run and reports any race it sees:

```sh
go test -race ./...
```

`httptest` servers handle each request in **its own goroutine**. So once the
crawler fetches concurrently, a test handler that changes a variable is shared
memory. Add `TestCrawlConcurrent` in a new file,
`internal/crawler/concurrent_test.go`. It uses a slow fake site and counts how
many requests are open at once:

```go
func TestCrawlConcurrent(t *testing.T) {
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

	// your crawl and checks here
}
```

A **`sync.Mutex`** ("mutual exclusion") lets only one goroutine at a time run
the code between `Lock` and `Unlock`. Any other goroutine calling `Lock` waits
its turn. Every read and write of `inFlight` and `most` happens while holding
`mu`, so there's no race.

**Your task:** crawl `srv.URL + "/"` with `Options{MaxPages: 5, MaxDepth: 1,
Workers: 4}`, timing it with `start := time.Now()` and `time.Since(start)`.
Check that:

1. 5 pages were handled.
2. `most` is 4: the four links were fetched at the same time.
3. It took under 400ms. One page at a time would take about 500ms (5 × 100);
   with 4 workers it's about 200ms (`/`, then the other four together).

Then **see a race on purpose.** Comment out the four `mu.Lock()` /
`mu.Unlock()` lines, and add `_ = mu` under `var mu sync.Mutex` so the unused
variable still compiles. Run `go test -race ./internal/crawler/`. The test
fails with `WARNING: DATA RACE` and the stack traces of the two goroutines
that touched `inFlight` at the same time. Without `-race`, the same test
usually passes, which is what makes races dangerous. Put the locks back.

> `TestCrawl` from milestone 3 appends to `requested` in its handler without
> a lock. That's safe only because it never sets `Workers`, so its requests
> come one at a time. If you ever give it workers, give it a mutex too.

---

## Part 3: `butler refresh`

To refresh, Butler needs to remember **where each crawl started**. Pages
alone aren't enough: a refresh should also find pages that are new since
last time, and that means crawling from the start again.

### Step 7: Remember the sites

Add a third table to `schema`:

```sql
CREATE TABLE IF NOT EXISTS sites (
	url TEXT PRIMARY KEY
)
```

and two methods:

```go
// SaveSite remembers url as a crawl start, so refresh can crawl it again.
func (s *Store) SaveSite(url string) error {
	_, err := s.db.Exec(`INSERT INTO sites (url) VALUES (?) ON CONFLICT DO NOTHING`, url)
	return err
}

// ListSites returns every saved crawl start, in alphabetical order.
func (s *Store) ListSites() ([]string, error)
```

`ON CONFLICT DO NOTHING` makes saving the same site twice harmless.
`ListSites` is the usual `Query` loop with one column. Its last line can be
`return sites, rows.Err()`: if `rows.Err()` is nil, that returns the sites
and no error.

**Test it:** `TestSites` saves `b`, `a`, then `b` again, and checks
`ListSites` returns exactly `a`, `b`.

### Step 8: Share the crawl code

`refresh` needs the same flags as `crawl`, and the same code to save each
page. Move both out of `runCrawl`:

```go
// crawlFlags adds the flags that crawl and refresh share to fs.
func crawlFlags(fs *flag.FlagSet) (opts *crawler.Options, dbPath *string) {
	opts = &crawler.Options{}
	fs.IntVar(&opts.MaxPages, "max-pages", 50, "stop after fetching this many pages")
	// ... -max-depth, -delay, -workers, as before
	dbPath = fs.String("db", "butler.db", "SQLite database file")
	return opts, dbPath
}

// crawlSite crawls start and saves each page's text. It returns how many
// pages it saved, and how many of those were new or had changed.
func crawlSite(st *store.Store, start string, opts crawler.Options) (saved, changed int, err error)
```

`crawlSite` holds the `crawler.Crawl` call and its function literal from
`runCrawl`, with one addition before `SavePage`:

```go
old, err := st.GetPage(p.URL)
if err != nil && !errors.Is(err, store.ErrNotFound) {
	return err
}
if old.Text != text {
	changed++
}
```

A new page gives `ErrNotFound` and an empty `old`, and `"" != text`, so new
pages count as changed without a special case.

Here the named results do real work, not just documentation: the closure
increments `saved` and `changed` directly, and the function ends with
`return saved, changed, err`.

**Your task:**

1. Shrink `runCrawl` to: `crawlFlags`, parse, the usage check, open the
   store, `crawlSite`, then `st.SaveSite(fs.Arg(0))`, then print
   `saved 3 pages to butler.db, 0 new or changed (7 in total)`. Save the site
   *after* a successful crawl, so a typo'd URL is never remembered.
2. Write `runRefresh`: `crawlFlags`, no arguments allowed, `ListSites`
   (none? print `no sites in butler.db; run "butler crawl" first`), then
   `crawlSite` for each, printing `refreshing <url>` first. If one site fails,
   print the error and carry on. At the end, print the total changed, and if
   it's more than 0, `run "butler index" to embed them`.
3. Add `"refresh"` to the `switch` in `main` and to the usage line.

Your existing `butler.db` has no sites yet. The next `crawl` you run adds one.

Pages that disappear from a site stay in the database. Removing them would
need to track which pages each refresh saw, which is a good extension, but
not a small one.

---

## Part 4: `butler stats`

### Step 9: Count things

`runStats` takes only `-db`. Everything it needs, you already have:
`ListPages`, `PagesToIndex`, `CountChunks` and `ListSites`.

1. Print the counts of pages (and how many aren't indexed yet), chunks and
   sites.
2. Find the oldest `FetchedAt`. Loop over the pages, keeping the earliest:
   `if oldest.IsZero() || p.FetchedAt.Before(oldest)`. A zero `time.Time`
   means "not set yet". Print it as
   `oldest.Local().Format(time.DateTime)`, followed by
   `time.Since(oldest).Hours()` with `%.0f`. This tells you when it's
   time to refresh.
3. Count pages per host in a `map[string]int`, using `url.Parse(p.URL)` and
   `.Host`. `perHost[host]++` works even for a new key, because a missing key
   reads as 0.
4. Print the hosts in alphabetical order. A map has no order, so sort its
   keys:
   ```go
   for _, host := range slices.Sorted(maps.Keys(perHost)) {
   	fmt.Printf("  %4d  %s\n", perHost[host], host)
   }
   ```
   `maps.Keys` returns an **iterator** over the keys, and `slices.Sorted`
   collects it into a sorted slice.

Add `"stats"` to `main`'s `switch` and to the usage line.

---

## Errors you'll likely see

| Error / symptom | Meaning |
|---|---|
| The answer appears all at once, after a wait | `Stream` is still `false` in `Chat`'s request. |
| Only the first word of the answer | `Decode` isn't in a loop, or the loop stops before `done`. |
| `cannot use fmt.Print (value of type func(a ...any) (n int, err error)) as func(string) value` | Wrap it: `func(s string) { fmt.Print(s) }`. |
| `ask` hangs after the answer | The loop never sees `Done`. Check the `json:"done"` tag. |
| The crawl never ends, and nothing is fetched | `Workers` is 0, so every batch is empty. Use `max(opts.Workers, 1)`. |
| Pages print as `[3/3]`, `[3/3]`, `[3/3]` | You print `fetched` instead of `handled`. |
| Every result in a batch is empty, and no error | You read `results` before `wg.Wait()`. |
| `WARNING: DATA RACE` | Two goroutines share a variable without a lock. The report names both. Lock it with a `sync.Mutex`, or give each goroutine its own slot. |
| `wg.Go undefined` | Go older than 1.25. Your `go.mod` says 1.26, so check `go version`. |
| `TestCrawlConcurrent`: `at most 1 requests at once` | The fetches don't run in goroutines, or `Workers` isn't passed through. |
| `refresh` says `no sites` after you've crawled | `SaveSite` isn't called, or you crawled before adding it. Crawl once more. |
| `stats` prints `0001-01-01` as the oldest | `oldest` was never set. Check the `IsZero()` condition. |

---

## Check yourself

- Why does a single `Decode` read only the first piece of a streamed answer?
  How does a loop fix that, and when does the loop stop?
- What happens to a goroutine's work if you read the results before
  `wg.Wait()`?
- Why is it safe for many goroutines to write into the same `results` slice?
- Why do `seen` and `queue` not need a mutex?
- What's a data race? Why does `TestCrawlConcurrent` need a mutex when
  `TestCrawl` doesn't?
- With `-delay 1s -workers 4`, how many requests per second can Butler
  send? When does `-workers` actually speed things up?
- Why does `refresh` crawl from the start URL instead of re-fetching the
  stored pages?
- Why does `runCrawl` save the site after the crawl rather than before?

That's Butler built: crawl, index, ask, refresh. Send me `crawler.go`,
`concurrent_test.go`, `ollama.go`, `ollama_test.go`, `store.go`,
`store_test.go` and `main.go` for a final review.

<details>
<summary>Stuck? Skeleton of <code>send</code> and <code>post</code> (spoiler, not the full answer)</summary>

```go
func (c *Client) send(path string, in any) (*http.Response, error) {
	body, err := json.Marshal(in)
	if err != nil {
		return nil, err
	}
	resp, err := http.Post(c.BaseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("is Ollama running? %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		defer resp.Body.Close()
		return nil, apiError(resp)
	}
	return resp, nil
}

func (c *Client) post(path string, in, out any) error {
	resp, err := c.send(path, in)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// Decode into out; error? wrap it with path
	return nil
}
```

</details>

<details>
<summary>Stuck? Skeleton of <code>runRefresh</code> (spoiler, not the full answer)</summary>

```go
func runRefresh(args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
	opts, dbPath := crawlFlags(fs)
	fs.Parse(args)
	// NArg() != 0? usage, exit 1

	// open the store, defer Close, ListSites; none? print and return nil

	changed := 0
	for _, site := range sites {
		fmt.Println("refreshing", site)
		_, c, err := crawlSite(st, site, *opts)
		if err != nil {
			fmt.Printf("  error: %v\n", err) // one broken site shouldn't stop the rest
		}
		changed += c
	}
	// print the totals, and the "run butler index" hint if changed > 0
	return nil
}
```

</details>
