# Milestone 5: Split pages into chunks

**Goal:** `butler index` reads every page in the database and splits its text
into **chunks**: overlapping windows of about 200 words. It doesn't save
anything yet. For now it reports how many chunks each page would make, and
`-show` prints one page's chunks so you can check them yourself. Milestone 6
sends these chunks to Ollama to be embedded and stores the result.

**Done when:**

```sh
$ go run . index
 11 chunks  https://go.dev/doc/  (1661 words)
  4 chunks  https://go.dev/  (579 words)
  1 chunks  https://go.dev/solutions/case-studies  (75 words)
  1 chunks  https://go.dev/solutions/use-cases  (97 words)
  3 chunks  https://go.dev/doc/security/  (443 words)
  2 chunks  https://go.dev/learn/  (320 words)
241 chunks  https://go.dev/ref/spec  (38600 words)
7 pages, 263 chunks (size 200 words, overlap 40)

$ go run . index -size 100 -overlap 0
...
7 pages, 420 chunks (size 100 words, overlap 0)

$ go run . index -show https://go.dev/learn/
https://go.dev/learn/: 320 words, 2 chunks

--- chunk 1 of 2 ---
... use the Go package discovery tool, and call functions of an external
module. Create a module This is the first part of a tutorial that introduces
a few fundamental features of

--- chunk 2 of 2 ---
code, use the go command to run your code, use the Go package discovery tool,
and call functions of an external module. Create a module This is the first
part of a tutorial that introduces a few fundamental features of the Go
language. In this tutorial you'll create two modules. ...
```

(Your pages and numbers depend on what you've crawled. The chunks above are
shortened.) Look at the end of chunk 1 and the start of chunk 2: the last 40
words of one chunk are the first 40 words of the next. That's the overlap.

Bad sizes are errors, not crashes:

```sh
$ go run . index -size 50 -overlap 50
index: overlap (50) must be smaller than size (50)
$ echo $?
1
```

```sh
$ go test ./...
?   	butler	[no test files]
ok  	butler/internal/chunk	0.210s
ok  	butler/internal/crawler	0.402s
ok  	butler/internal/extract	0.588s
ok  	butler/internal/store	0.731s
```

**Concepts:** `strings.Fields`, slice expressions (`s[a:b]`, half-open
ranges, slices sharing memory), off-by-one errors, the built-in `min`,
`nil` slices, validating arguments with errors, `db.Query` and looping over
rows, `defer rows.Close()`, `rows.Err()`, `slices.Equal`, subtests with
`t.Run`, `fmt` width verbs like `%3d`.

---

## Why chunks?

In milestone 7, `butler ask` will find the text that best matches your
question and hand it to the chat model. It can't hand over whole pages:

- **Pages are the wrong size.** The Go spec is 38,600 words. That's far more
  than an 8B model can usefully read in one go, and most of it has nothing to
  do with your question.
- **An embedding describes the whole of its text.** One embedding for the
  entire spec would be a blurry average of every topic in it, and it would
  match no question well. A 200-word piece about slices gets an embedding
  that's clearly *about slices*.

So each page is cut into pieces small enough to be about one thing, and big
enough to make sense on their own.

**Why overlap?** Cut the text into back-to-back pieces and some sentences get
cut in half: the first half ends one chunk, the second half starts the next,
and neither chunk contains the whole thought. With overlap, the end of each
chunk is repeated at the start of the next, so anything that falls across a
boundary appears whole in at least one chunk (as long as it's shorter than the
overlap).

```
words:   0 ............................................. 319
chunk 1: [0 ──────────────────────── 200)
chunk 2:                       [160 ──────────────────────── 320)
                                └─ 40 ─┘ overlap
```

**Why words, not characters or tokens?** Models count **tokens**, pieces of
words, and about 1.3 tokens per English word is a fair rule of thumb, so 200
words is roughly 260 tokens. Counting tokens exactly would need the model's
own tokenizer. Words are close enough and never cut a word in half. Characters
would cut words in half.

**Why not paragraphs?** Paragraphs vary wildly: one line on some pages, 2,000
words on others. Word windows give every chunk a predictable size. The cost is
that line breaks become plain spaces inside a chunk, which makes no difference
to an embedding.

---

## How the pieces fit

```
main.go  runIndex(args)
  ├─ flags: -db, -size, -overlap, -show
  ├─ st := store.Open(dbPath),  defer st.Close()
  │
  ├─ -show URL?  page := st.GetPage(url)                   ← from milestone 4
  │              chunks := chunk.Split(page.Text, size, overlap)
  │              print each chunk
  │
  └─ otherwise:  pages := st.ListPages()                   ← new, internal/store
                 for each page:
                    chunks := chunk.Split(page.Text, size, overlap)   ← new, internal/chunk
                    print count + URL
                 print totals
```

`chunk` is a new package that knows nothing about pages, databases or the
web. It turns a string into a `[]string`. That makes it the easiest package in
Butler to test, and you'll test it thoroughly.

---

## Step 1: Words

```go
words := strings.Fields("  Go is\n\nfun.\t ")
// []string{"Go", "is", "fun."}
```

`strings.Fields` splits a string around **runs of whitespace**: spaces, tabs,
newlines, any number of them in a row. Leading and trailing whitespace
produces no empty strings. Compare `strings.Split(s, " ")`, which splits on
every single space and gives you `""` for each extra one.

Punctuation stays attached to its word (`"fun."`), which is what you want: the
chunk text should read like the original.

Joining the words back with `strings.Join(words, " ")` gives you the text with
every line break and run of spaces turned into one space.

---

## Step 2: Slice expressions

You've used `queue[1:]` to drop the front of the crawl queue. The full form is
`s[low:high]`:

```go
words := []string{"a", "b", "c", "d", "e"}
words[1:3]  // ["b" "c"]          from index 1 up to, NOT including, 3
words[:2]   // ["a" "b"]          low defaults to 0
words[3:]   // ["d" "e"]          high defaults to len(words)
words[2:2]  // []                 empty, not an error
words[0:5]  // all five           high may equal len(words)
words[0:6]  // panic: slice bounds out of range [:6] with capacity 5
```

The range is **half-open**: it includes `low` and excludes `high`. That has two
handy results:

- `len(s[low:high]) == high - low`. A 200-word chunk starting at `start` is
  exactly `words[start : start+200]`.
- Back-to-back ranges don't overlap or leave gaps: `s[0:3]` and `s[3:5]` cover
  `s[0:5]`.

`high` can be at most `len(s)`. One past that, and the program **panics**:
it stops with an error message and a stack trace. Most of this milestone's
care goes into never asking for a `high` that's too big.

> **Slices share memory.** `words[1:3]` doesn't copy anything. It's a new view
> onto the same underlying array, so changing `words[1:3][0]` changes
> `words[1]` too. Chunks don't change their words, and `strings.Join` builds a
> new string anyway, so it doesn't matter here. It will matter one day: when
> two slices seem to affect each other, this is why.

---

## Step 3: The `chunk` package

Create `internal/chunk/chunk.go`:

```go
// Package chunk splits text into overlapping pieces for embedding.
package chunk

// Split breaks text into chunks of size words. Each chunk after the first
// starts with the last overlap words of the chunk before it. Whitespace
// inside a chunk is collapsed to single spaces. Text with no words gives no
// chunks.
func Split(text string, size, overlap int) ([]string, error)
```

**Check the arguments first.** `size` and `overlap` come from command-line
flags, so a user can pass anything. Return an error for:

| Bad input | Error message |
|---|---|
| `size < 1` | `size must be at least 1, got 0` |
| `overlap < 0` | `overlap must not be negative, got -5` |
| `overlap >= size` | `overlap (50) must be smaller than size (50)` |

The last one matters most. Each chunk starts `size - overlap` words after the
previous one. Call that the **step**. If the step is 0 the loop never moves
forward and runs forever. If it's negative, it moves backwards.

> **Error or panic?** Go code panics for *programmer* mistakes, like a slice
> index out of range, that should never happen in a correct program. Input a
> user can get wrong gets an `error`, so `main` can print a helpful message.

**Then split.** Get the words with `strings.Fields`. If there are none, return
`nil, nil`: no chunks, and no error. A `nil` slice has length 0 and works with
`len`, `range` and `append`, so callers don't need to treat it specially.

Otherwise, walk through the words one step at a time:

```go
step := size - overlap
var chunks []string
for start := 0; ; start += step {
	end := min(start+size, len(words))
	chunks = append(chunks, strings.Join(words[start:end], " "))
	// stop if this chunk reached the last word
}
return chunks, nil
```

- `for start := 0; ; start += step` is a three-part `for` with no condition in
  the middle, so it loops until a `break` or `return`.
- `min` is a **built-in** function (since Go 1.21), like `len` and `append`.
  It keeps `end` from going past the last word, so the final chunk can be
  shorter than `size`.

**Your task:** write `Split`. The comment marks the line that's left: when
`end == len(words)`, this chunk ended on the last word, so `break`.

⚠️ **The tempting loop is wrong.** It's natural to write:

```go
for start := 0; start < len(words); start += step { ... }  // ❌
```

Try it in your head with 200 words, size 200, overlap 40. The first chunk is
`words[0:200]`, the whole text. Then `start` becomes 160, which is still
`< 200`, so you get a second chunk, `words[160:200]`: 40 words the first
chunk already contained. Every text whose length lands in the overlap makes a
useless extra chunk. Stopping when `end` reaches the end, not when `start`
does, avoids it. This kind of bug is called an **off-by-one error**, and Step
4's tests exist to catch exactly this.

---

## Step 4: Test `Split`

Chunking is all edge cases, so a table-driven test fits well. Real text makes
the expected output hard to write, so use made-up words that say where they
are: `w0 w1 w2 ...`. Then you can see at a glance that a chunk starts at word 3.

Create `internal/chunk/chunk_test.go`:

```go
package chunk

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// words returns "w0 w1 ... w(n-1)".
func words(n int) string {
	var ws []string
	for i := range n {
		ws = append(ws, fmt.Sprintf("w%d", i))
	}
	return strings.Join(ws, " ")
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		size, overlap int
		want          []string
	}{
		{"empty", "", 5, 2, nil},
		{"fewer words than size", words(3), 5, 2, []string{"w0 w1 w2"}},
		{"exactly size", words(5), 5, 2, []string{"w0 w1 w2 w3 w4"}},
		{"one word over", words(6), 5, 2, []string{"w0 w1 w2 w3 w4", "w3 w4 w5"}},
		// your cases here
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Split(tc.text, tc.size, tc.overlap)
			if err != nil {
				t.Fatalf("Split: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Split(%q, %d, %d)\n got %q\nwant %q",
					tc.text, tc.size, tc.overlap, got, tc.want)
			}
		})
	}
}
```

New things here:

- `for i := range n` loops `i` from 0 to `n-1`. Ranging over an integer
  is new in Go 1.22.
- `size, overlap int` declares two struct fields of the same type in one line,
  just like function parameters.
- `t.Run(name, func...)` runs each case as a **subtest**. A failure is
  reported as `TestSplit/one_word_over`, so you know which case broke, and
  `t.Fatalf` stops only that case, not the whole test. You can run one case:
  `go test ./internal/chunk/ -run 'TestSplit/exactly'`.
- `slices.Equal` (standard library package `slices`) compares two slices
  element by element. `==` doesn't work on slices; the compiler won't allow
  it. Note that `slices.Equal(nil, []string{})` is `true`: both are empty.
- `%q` prints a `[]string` with quotes around each element, so you can see
  where one chunk ends and the next begins, and spot stray spaces.

**Your task:** add cases for:

1. Whitespace only (`"  \n\t  "`): no chunks.
2. Messy whitespace (`"a\n\nb   c\t d"`, size 5): one chunk, `"a b c d"`.
3. Two chunks that end exactly on the last word: 8 words, size 5, overlap 2.
4. Three chunks with a short last one: 9 words, size 5, overlap 2.
5. No overlap: 5 words, size 2, overlap 0, giving `"w0 w1"`, `"w2 w3"`, `"w4"`.
6. Overlap of `size - 1`, the largest allowed: 4 words, size 3, overlap 2.

Work out each `want` on paper before you run the test. If the test disagrees,
decide which of you is right before changing anything.

Then write a second test, `TestSplitBadArgs`, with a table of `size, overlap`
pairs that must fail: `(0, 0)`, `(-1, 0)`, `(5, -1)`, `(5, 5)` and `(5, 6)`.
For each, check that `err != nil`. The text doesn't matter; use `words(10)`.

Finally, break your `Split` on purpose: switch to the ❌ loop from Step 3 and
run the tests. The "exactly size" case should fail. Then put it back. A test
that has never failed hasn't proved it can catch anything.

---

## Step 5: List every page

`index` needs every page in the store. `QueryRow` from milestone 4 returns a
single row. For many rows, use `Query`:

```go
rows, err := s.db.Query(`SELECT url, title, text, fetched_at FROM pages ORDER BY id`)
if err != nil {
	return nil, err
}
defer rows.Close()

for rows.Next() {
	// rows.Scan(...) into the current row's columns, like QueryRow's Scan
}
if err := rows.Err(); err != nil {
	return nil, err
}
```

- `rows.Next()` moves to the next row and returns `false` when there are no
  more, or when something went wrong.
- That's why you check `rows.Err()` **after** the loop. Without it, an error
  halfway through looks exactly like the end of the results, and you'd return
  half the pages without knowing.
- `defer rows.Close()` releases the connection the query is using. If you
  `return` from inside the loop because a `Scan` failed, the deferred `Close`
  still runs.
- `ORDER BY id` returns pages in the order they were first saved. Without it,
  SQL doesn't promise any order.

**Your task:** add to `store.go`:

```go
// ListPages returns every stored page, oldest first.
func (s *Store) ListPages() ([]Page, error)
```

Inside the loop, scan into a `Page` and a `fetchedAt` string, parse the time
the same way `GetPage` does, and `append` the page to the slice you return.

This loads every page into memory at once. A few hundred pages of text is a
few megabytes, which is fine. If Butler ever holds tens of thousands of pages,
you'd process rows one at a time instead.

**Test it.** Add `TestListPages` to `store_test.go`: open a temp database,
check `ListPages` returns no pages, save three pages with different URLs, and
check you get three back, in the order you saved them (compare the URLs).

---

## Step 6: `butler index`

Write `runIndex` in `main.go`, following the same shape as `runCrawl`:

```go
// runIndex handles "butler index": it splits every stored page into chunks.
// For now it only reports the counts; milestone 6 stores the chunks.
func runIndex(args []string) error
```

and call it from the `"index"` case the same way `"crawl"` calls `runCrawl`,
printing `index: <err>` and exiting with 1 on an error.

1. A `FlagSet` named `"index"` with:
   - `-db` (default `"butler.db"`), as in `crawl`
   - `-size` (default `200`), `"words per chunk"`
   - `-overlap` (default `40`), `"words shared by neighbouring chunks"`
   - `-show` (default `""`), `"print the chunks of this one URL"`
2. If `fs.NArg() != 0`, it's a usage error, as in `crawl`.
3. **Check the sizes before opening the database**, by calling
   `chunk.Split("", *size, *overlap)` and returning its error. An empty string
   gives no chunks, but the arguments are still checked. Failing early means a
   typo in a flag never leaves an empty `butler.db` behind.
4. `store.Open`, return its error, `defer st.Close()`.
5. If `*show != ""`, call `GetPage`, split its text and print it in the
   format from **Done when**. Return after printing. If the page isn't in
   the store, return an error like `https://go.dev/nope: page not found`
   (wrapping with `%w` keeps `ErrNotFound` checkable).
6. Otherwise, `ListPages`. If there are none, print
   `no pages in butler.db; run "butler crawl" first` and return `nil`.
7. For each page, split it, add the chunk count to a running total, and print
   one line:
   ```go
   fmt.Printf("%3d chunks  %s  (%d words)\n", len(chunks), p.URL, len(strings.Fields(p.Text)))
   ```
   `%3d` pads the number with spaces to at least 3 characters, so the URLs
   line up (until a page has 1,000 chunks).
8. Print the summary line: `7 pages, 263 chunks (size 200 words, overlap 40)`.

`main.go` now also imports `butler/internal/chunk`.

> **Why not `chunk.Split` once per page and keep it?** You will, in milestone 6:
> each chunk gets saved with its embedding. This milestone stops at counting
> so you can try different sizes and look at the results without anything to
> clean up afterwards.

Run the **Done when** commands. Then try `-show` on a short page with
`-size 20 -overlap 5` and check the overlap by eye.

---

## Errors you'll likely see

| Error / symptom | Meaning |
|---|---|
| `panic: runtime error: slice bounds out of range [:205] with capacity 200` | `end` went past the last word. Use `min(start+size, len(words))`. |
| The program hangs | The step is 0, so `start` never moves. Check `overlap >= size` is rejected *before* the loop. |
| An extra chunk at the end that the previous chunk already contained | The ❌ loop from Step 3: you're stopping when `start` reaches the end instead of `end`. |
| `invalid operation: got == tc.want (slice can only be compared to nil)` | Use `slices.Equal(got, tc.want)`. |
| `undefined: min` | Your `go` line in `go.mod` is older than 1.21. Yours says 1.26, so check for a typo. |
| A test case fails with `got ["w0 w1 w2 w3 w4"]`, `want ["w0 w1 w2 w3 w4" "w3 w4"]` | Your test is wrong, not `Split`: the second chunk would repeat words already in the first. |
| `ListPages` returns fewer pages than `CountPages` | Check `rows.Err()` after the loop, and make sure you `append` inside it. |
| `sql: Rows are closed` | You called `rows.Close()` before the loop finished. Use `defer`. |
| `index` creates an empty `butler.db` after a bad `-overlap` | Check the sizes (Step 6.3) before `store.Open`. |

---

## Check yourself

- What does `strings.Fields` do that `strings.Split(s, " ")` doesn't?
- What's `len(s[3:7])`? Why is "half-open" convenient for chunking?
- Why does `Split` stop when `end` reaches the last word, not when `start`
  does? Give a word count, size and overlap where the difference shows.
- What would happen with `overlap == size` if `Split` didn't reject it?
- Why does `Split` return an error for a bad size instead of panicking?
- Why check `rows.Err()` after the `rows.Next()` loop?
- What happens to a sentence that crosses a chunk boundary, with and without
  overlap?
- What does the test gain from words like `w3` instead of real text?

When it's working, send me `chunk.go`, `chunk_test.go`, `store.go`,
`store_test.go` and `main.go` for review. Milestone 6 starts talking to
Ollama: it turns each chunk into an embedding and saves both to the database.

<details>
<summary>Stuck? Skeleton of <code>Split</code> (spoiler, not the full answer)</summary>

```go
func Split(text string, size, overlap int) ([]string, error) {
	if size < 1 {
		return nil, fmt.Errorf("size must be at least 1, got %d", size)
	}
	// overlap < 0? overlap >= size? return an error

	words := strings.Fields(text)
	if len(words) == 0 {
		return nil, nil
	}

	step := size - overlap
	var chunks []string
	for start := 0; ; start += step {
		end := min(start+size, len(words))
		chunks = append(chunks, strings.Join(words[start:end], " "))
		if end == len(words) {
			break
		}
	}
	return chunks, nil
}
```

Imports for `chunk.go`: `fmt`, `strings`.

</details>

<details>
<summary>Stuck? Skeleton of <code>ListPages</code> (spoiler, not the full answer)</summary>

```go
func (s *Store) ListPages() ([]Page, error) {
	rows, err := s.db.Query(`SELECT url, title, text, fetched_at FROM pages ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pages []Page
	for rows.Next() {
		var p Page
		var fetchedAt string
		// rows.Scan(four pointers); error? return it
		// p.FetchedAt, err = time.Parse(...); error? return it
		pages = append(pages, p)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return pages, nil
}
```

</details>
