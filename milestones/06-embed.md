# Milestone 6: Embed chunks and store the vectors

**Goal:** `butler index` stops counting and starts indexing. For every page
that hasn't been indexed yet, it splits the text into chunks, sends them to
Ollama to be **embedded**, and saves each chunk with its embedding in a new
`chunks` table. Running it again only does pages that are new, or whose text
changed when they were crawled again.

**Done when:**

```sh
$ go run . index
[1/7] https://go.dev/doc/  11 chunks
[2/7] https://go.dev/  4 chunks
[3/7] https://go.dev/solutions/case-studies  1 chunks
[4/7] https://go.dev/solutions/use-cases  1 chunks
[5/7] https://go.dev/doc/security/  3 chunks
[6/7] https://go.dev/learn/  2 chunks
[7/7] https://go.dev/ref/spec  241 chunks
indexed 7 pages, 263 chunks (263 in total)

$ go run . index
indexed 0 pages, 0 chunks (263 in total)
```

The first run takes a little while: Ollama loads the model on the first
request, and the spec alone is 241 chunks. The second run has nothing to do.

```sh
$ sqlite3 butler.db "SELECT page_id, seq, length(embedding), substr(text, 1, 40) FROM chunks LIMIT 3"
1|0|3072|The Go programming language is an open s
1|1|3072|multiple modules. Tutorial: Developing a
1|2|3072|fourth introduces Go's concurrency primi
```

Every embedding is 3,072 bytes: 768 numbers, 4 bytes each. If Ollama isn't
running, `index` says so and exits with 1:

```sh
$ go run . index
index: embedding https://go.dev/doc/: is Ollama running? Post "http://localhost:11434/api/embed": dial tcp [::1]:11434: connect: connection refused
$ echo $?
1
```

```sh
$ go test ./...
?   	butler	[no test files]
ok  	butler/internal/chunk	0.233s
ok  	butler/internal/crawler	0.415s
ok  	butler/internal/extract	0.602s
ok  	butler/internal/ollama	0.318s
ok  	butler/internal/store	0.744s
```

The `ollama` tests use a fake server, so `go test` works without Ollama
running.

**Concepts:** JSON with `encoding/json`, struct tags, `http.Post`, checking
status codes from an API, decoding an error response, `httptest` checking
what the client sent, `float32`, bytes and byte order (`encoding/binary`),
`math.Float32bits`, SQLite BLOBs, transactions (`Begin`, `Commit`,
`defer tx.Rollback()`), subqueries, `NOT IN`, an unexported helper shared by
two methods.

---

## What's an embedding?

An **embedding model** turns a piece of text into a fixed-length list of
numbers, a **vector**. `nomic-embed-text` always returns 768 of them,
whatever the length of the text:

```
"How do I declare a slice?"           → [0.021, -0.113, 0.054, ... 768 numbers]
"Slices are created with make or []T" → [0.019, -0.098, 0.061, ...]
"Gophers dig tunnels"                 → [-0.087, 0.032, -0.140, ...]
```

The model was trained so that texts with **similar meanings** get vectors
that point in **similar directions**, even when they share no words. The two
slice sentences above get close vectors; the gopher one doesn't. Milestone 7
uses that: it embeds your question and looks for the chunks whose vectors
are closest.

Each number on its own means nothing you can read. Only comparing whole
vectors does. So Butler treats them as opaque: get them from Ollama, store
them, and compare them later.

---

## How the pieces fit

```
main.go  runIndex(args)
  ├─ flags (+ new: -model, -ollama, -force)
  ├─ st := store.Open(...)
  ├─ -force? st.DeleteAllChunks()
  ├─ pages := st.PagesToIndex()                           ← pages with no chunks yet
  └─ for each page:
        texts := chunk.Split(page.Text, size, overlap)    ← milestone 5
        vecs  := client.Embed(model, texts)               ← new, internal/ollama
                    └─ POST /api/embed  ──HTTP + JSON──>  Ollama
        st.SaveChunks(page.URL, chunks)                   ← new, in a transaction
                    └─ each vector → 3,072 bytes → BLOB column

store.SavePage (crawl)
  └─ text changed? delete that page's chunks, so index picks it up again
```

`ollama` is a new package that knows how to talk to Ollama and nothing else.
`store` learns to save chunks. `main.go` connects them, as before.

---

## Step 0: Set up Ollama and try it by hand

If you haven't yet, follow the **Setup** section in [GUIDE.md](../GUIDE.md):

```sh
brew install ollama
brew services start ollama
ollama pull nomic-embed-text
```

Then ask it for an embedding with `curl`, so you know what Butler will be
sending and getting back:

```sh
$ curl -s http://localhost:11434/api/embed -d '{
  "model": "nomic-embed-text",
  "input": ["Go is fun", "Gophers dig tunnels"]
}' | head -c 300
{"model":"nomic-embed-text","embeddings":[[0.0123,-0.0456,...],[...]],"total_duration":...
```

(Your numbers will differ.) `input` is a list, so one request can embed many
chunks. `embeddings` comes back in the **same order** as `input`.

Now try a model you haven't pulled:

```sh
$ curl -s http://localhost:11434/api/embed -d '{"model": "nope", "input": ["hi"]}'
{"error":"model 'nope' not found"}
```

On failure, Ollama answers with an error status (404 here) and a JSON object
with an `error` field. Your client will turn that into a Go error.

---

## Step 1: JSON and struct tags

Go's `encoding/json` converts between JSON and Go values. You describe the
JSON's shape with a struct:

```go
type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}
```

The text in backquotes after a field is a **struct tag**. It's extra
information attached to the field that packages can read. `json:"model"`
tells `encoding/json` that the Go field `Model` is called `model` in the
JSON.

```go
body, err := json.Marshal(embedRequest{Model: "nomic-embed-text", Input: []string{"Go is fun"}})
// body is []byte(`{"model":"nomic-embed-text","input":["Go is fun"]}`)
```

`json.Marshal` turns a Go value into JSON bytes. Going the other way:

```go
type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

var out embedResponse
err := json.NewDecoder(resp.Body).Decode(&out)
```

`json.NewDecoder(r).Decode(&v)` reads JSON from an `io.Reader`, like an HTTP
response body, and fills in `v`. It takes a pointer for the same reason
`Scan` does: it needs to write into your variable.

Things to know:

- **Only exported fields are used.** `encoding/json` lives in another package,
  so it can't see lowercase fields. A field named `model` (lowercase) is
  silently left out of the JSON. The struct type itself can be lowercase;
  only the fields need capitals.
- **Fields you don't list are ignored.** Ollama's response also has `model`,
  `total_duration` and more. `embedResponse` only declares `embeddings`, so
  the rest is skipped. Declare what you use.
- **Missing fields keep their zero value.** If the response had no
  `embeddings`, `out.Embeddings` would be `nil`, not an error. You'll check
  for that.
- `[][]float32` is a slice of slices: one `[]float32` per input. JSON numbers
  decode straight into `float32`.

> **`float32` or `float64`?** `float64` is Go's usual float. Embeddings don't
> need that precision, and `float32` takes half the space: 3 KB per chunk
> instead of 6 KB. Ollama itself works in 32-bit floats.

---

## Step 2: The `ollama` package

Create `internal/ollama/ollama.go`:

```go
// Package ollama is a small client for the Ollama HTTP API.
package ollama

// DefaultURL is where Ollama listens unless you configure it otherwise.
const DefaultURL = "http://localhost:11434"

// Client talks to an Ollama server.
type Client struct {
	BaseURL string // for example DefaultURL
}

// Embed returns one embedding per input, in the same order, using model.
func (c *Client) Embed(model string, inputs []string) ([][]float32, error)
```

Using it looks like:

```go
client := &ollama.Client{BaseURL: ollama.DefaultURL}
vecs, err := client.Embed("nomic-embed-text", []string{"Go is fun"})
```

`BaseURL` is a field, not a constant baked into `Embed`, so the test can point
the client at a fake server.

**Your task:** write `Embed`, along with the `embedRequest` and
`embedResponse` types from Step 1:

1. `json.Marshal` an `embedRequest`.
2. Send it:
   ```go
   resp, err := http.Post(c.BaseURL+"/api/embed", "application/json", bytes.NewReader(body))
   ```
   `http.Post` takes the URL, the `Content-Type`, and the body as an
   `io.Reader`. `bytes.NewReader` turns your `[]byte` into one, as you did for
   readability in milestone 4.
   If `err != nil`, the request never got an answer, almost always because
   Ollama isn't running. Wrap it with a hint:
   `fmt.Errorf("is Ollama running? %w", err)`.
3. `defer resp.Body.Close()`.
4. If `resp.StatusCode != http.StatusOK`, return `apiError(resp)` (Step 3).
5. Decode the body into an `embedResponse`. Wrap an error:
   `fmt.Errorf("decoding embed response: %w", err)`.
6. Check `len(out.Embeddings) == len(inputs)`. If not, return
   `fmt.Errorf("asked for %d embeddings, got %d", ...)`. This should never
   happen, but if it did, every chunk after the gap would be stored with
   another chunk's vector, and you'd never notice.
7. Return `out.Embeddings, nil`.

> **No timeout.** `http.Post` uses Go's default client, which waits forever.
> The crawler sets a 15-second timeout because websites can hang. Here, the
> server is your own machine, and a big batch on a slow laptop can
> legitimately take a minute, so no timeout is the simpler choice.

---

## Step 3: Turn Ollama's errors into Go errors

```go
// apiError builds an error from a failed Ollama response.
func apiError(resp *http.Response) error
```

**Your task:** write it.

1. Declare a struct with one field, `Error string` tagged `json:"error"`.
2. Decode `resp.Body` into it. If decoding works **and** the field isn't
   empty, return `fmt.Errorf("ollama: %s", e.Error)`.
3. Otherwise, the body wasn't the JSON you expected (a proxy error page,
   say), so fall back to the status: `fmt.Errorf("ollama: %s", resp.Status)`.
   `resp.Status` is the whole status line, like `404 Not Found`.

Now a missing model gives
`ollama: model 'nope' not found` instead of a bare
`404`, which tells you exactly what to do.

---

## Step 4: Test the client against a fake Ollama

You've used `httptest` to fake websites. Here the fake server also checks
**what the client sent**, then replies like Ollama would.

Create `internal/ollama/ollama_test.go`:

```go
package ollama

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
)

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
			t.Errorf("request = %s %s, want POST /api/embed", r.Method, r.URL.Path)
		}
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if req.Model != "test-model" || !slices.Equal(req.Input, []string{"a", "b"}) {
			t.Errorf("request body = %+v", req)
		}
		w.Write([]byte(`{"model":"test-model","embeddings":[[1,2],[3,4.5]],"total_duration":12345}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	got, err := c.Embed("test-model", []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	// your checks here
}
```

- The handler decodes the request into the same `embedRequest` type the
  client encoded. If a struct tag is wrong, the field arrives empty and the
  test fails.
- The reply includes `model` and `total_duration`, which `embedResponse`
  doesn't declare, to prove they're ignored.
- The test is in `package ollama`, so it can use the lowercase types.

**Your task:**

1. Finish `TestEmbed`: check `got` has 2 vectors and that they're `[1 2]` and
   `[3 4.5]` (`slices.Equal` works on `[]float32` too).
2. Write `TestEmbedError`. Its handler replies with
   ```go
   w.WriteHeader(http.StatusNotFound)
   w.Write([]byte(`{"error":"model \"nope\" not found, try pulling it first"}`))
   ```
   Check that `Embed` returns an error and that `err.Error()` contains
   `not found` (`strings.Contains`). `WriteHeader` has to come before `Write`;
   once the body starts, the status has already been sent as 200.
3. Write `TestEmbedCountMismatch`: two inputs, but the server returns one
   embedding. Check for an error.

---

## Step 5: Vectors as bytes

SQLite has no "list of floats" type. It does have **BLOB**: a column that
holds raw bytes, whatever they mean. So Butler turns each `[]float32` into
`[]byte` before saving, and back after loading.

A `float32` is exactly 4 bytes. `math.Float32bits(f)` gives you those 32 bits
as a `uint32`, unchanged, and `math.Float32frombits` turns them back. Then
`encoding/binary` writes the `uint32` as 4 bytes:

```
[]float32{1.5, -2}

1.5  → Float32bits → 0x3FC00000 → bytes 00 00 C0 3F
-2   → Float32bits → 0xC0000000 → bytes 00 00 00 C0

[]byte{0x00, 0x00, 0xC0, 0x3F, 0x00, 0x00, 0x00, 0xC0}   (8 bytes)
```

**Byte order.** A `uint32` spans 4 bytes, and something has to decide which
comes first. **Little-endian** puts the smallest byte first, as above. It's
what Apple Silicon and Intel chips use internally. Big-endian is the reverse.
Either works, as long as writing and reading agree, and the only way to
guarantee that is to name one explicitly: `binary.LittleEndian`.

In `store.go`:

```go
// encodeVector packs v into 4 little-endian bytes per float.
func encodeVector(v []float32) []byte {
	b := make([]byte, 0, len(v)*4)
	for _, f := range v {
		b = binary.LittleEndian.AppendUint32(b, math.Float32bits(f))
	}
	return b
}
```

`make([]byte, 0, len(v)*4)` makes an empty slice with room for all the bytes,
so `append` never has to grow it. The third argument to `make` is the
**capacity**: how much space is set aside, as opposed to the length, how much
is in use.

**Your task:** write the reverse:

```go
// decodeVector unpacks bytes written by encodeVector.
func decodeVector(b []byte) ([]float32, error)
```

1. If `len(b) % 4 != 0`, it can't be a vector. Return an error.
2. `make([]float32, len(b)/4)`. This time the length is known, so make the
   slice that long and assign with `v[i] = ...` rather than `append`.
3. For each `i`, read 4 bytes starting at `i*4`:
   `math.Float32frombits(binary.LittleEndian.Uint32(b[i*4:]))`.
   `Uint32` reads the first 4 bytes of the slice you give it, which is why a
   slice expression starting at `i*4` is all you need.

Nothing outside `store` sees bytes. Milestone 7 adds a method that loads
chunks and returns `[]float32`, using `decodeVector`.

**Test it.** Add `TestVectorRoundTrip` to `store_test.go`: encode
`[]float32{1.5, -2, 0, 3.14159, -0.0001}`, check the result is 20 bytes,
decode it and check you get the same slice back. Also check that
`decodeVector` of a 5-byte slice returns an error.

---

## Step 6: The `chunks` table

Add a second table to the `schema` constant, after `pages`. Separate the two
`CREATE TABLE` statements with a `;`. One `Exec` runs them both.

```sql
CREATE TABLE IF NOT EXISTS chunks (
	id        INTEGER PRIMARY KEY,
	page_id   INTEGER NOT NULL,
	seq       INTEGER NOT NULL,
	text      TEXT NOT NULL,
	embedding BLOB NOT NULL,
	UNIQUE (page_id, seq)
)
```

- `page_id` is the `id` of the page the chunk came from. This is the link
  milestone 4 promised.
- `seq` is the chunk's position in its page: 0, 1, 2, ...
- `UNIQUE (page_id, seq)` says no page can have two chunk 3s.
- `text` stores the chunk without anything added for the model (see Step 9),
  because it's what milestone 7 shows the chat model and you.

Your existing `butler.db` gets the new table the next time it's opened.
`IF NOT EXISTS` skips `pages`, which is already there, and creates `chunks`.
Your crawled pages stay.

Add a type for what's saved:

```go
// Chunk is one piece of a page's text and its embedding.
type Chunk struct {
	Text      string
	Embedding []float32
}
```

---

## Step 7: Save a page's chunks in a transaction

```go
// SaveChunks replaces all chunks of the page at url. It returns ErrNotFound
// if there's no such page.
func (s *Store) SaveChunks(url string, chunks []Chunk) error
```

Saving a page's chunks is several statements: delete its old chunks, then
insert each new one. If Butler stopped halfway, a page could end up with
half its chunks, or old and new ones mixed. A **transaction** groups
statements so they all happen, or none do:

```go
tx, err := s.db.Begin()
if err != nil {
	return err
}
defer tx.Rollback()

// tx.Exec(...), tx.QueryRow(...) – the same methods as s.db

return tx.Commit()
```

- `s.db.Begin()` starts a transaction and returns a `*sql.Tx`. Run every
  statement on `tx`, not `s.db`, or it happens outside the transaction.
- `tx.Commit()` makes all the changes permanent at once.
- `defer tx.Rollback()` undoes everything if you return early with an error.
  After a successful `Commit`, `Rollback` does nothing, so deferring it
  unconditionally is safe. It's the usual pattern.

Transactions are also much faster here. Outside a transaction, SQLite makes
every `INSERT` durable on disk before the next one starts. Inside one, it does
that once, at `Commit`. 241 inserts for the spec become one disk sync.

**Your task:** write `SaveChunks`:

1. Begin, `defer tx.Rollback()`.
2. Look up the page's id:
   `tx.QueryRow("SELECT id FROM pages WHERE url = ?", url).Scan(&pageID)`,
   with `pageID` an `int64`. `sql.ErrNoRows` becomes `ErrNotFound`, as in
   `GetPage`.
3. `DELETE FROM chunks WHERE page_id = ?`.
4. For each chunk, with `i` as its `seq`:
   `INSERT INTO chunks (page_id, seq, text, embedding) VALUES (?, ?, ?, ?)`
   with `pageID, i, c.Text, encodeVector(c.Embedding)`. A `[]byte` passed to
   a placeholder is stored as a BLOB.
5. Wrap any error with the URL, for example
   `fmt.Errorf("saving chunks for %s: %w", url, err)`, and return it.
6. `return tx.Commit()`.

Also add:

```go
// CountChunks returns how many chunks are stored.
func (s *Store) CountChunks() (int, error)

// DeleteAllChunks removes every chunk, so the next index redoes every page.
func (s *Store) DeleteAllChunks() error
```

Both are one line of SQL, like `CountPages`.

---

## Step 8: Find what needs indexing

`index` should only embed pages that don't have chunks yet. That's a
**subquery**, a `SELECT` inside another one:

```sql
SELECT url, title, text, fetched_at FROM pages
WHERE id NOT IN (SELECT page_id FROM chunks)
ORDER BY id
```

The inner `SELECT` lists every page id that has chunks; the outer one keeps
the pages whose id isn't in that list.

```go
// PagesToIndex returns the pages that have no chunks yet, oldest first.
func (s *Store) PagesToIndex() ([]Page, error)
```

This would be the whole of `ListPages` again with a different query. Instead,
move `ListPages`'s body into an unexported helper that takes the query:

```go
// queryPages runs query, which must select url, title, text, fetched_at,
// and returns the pages.
func (s *Store) queryPages(query string) ([]Page, error)

func (s *Store) ListPages() ([]Page, error) {
	return s.queryPages(`SELECT url, title, text, fetched_at FROM pages ORDER BY id`)
}
```

**Your task:** make the helper, rewrite `ListPages` to use it, and write
`PagesToIndex` the same way. `TestListPages` should pass without changes:
that's how you know the refactor didn't break anything.

**Stale chunks.** There's a gap: if you crawl a page again and its text has
changed, its old chunks stay, so `PagesToIndex` skips it and `ask` would
answer from the old text. Fix it in `SavePage`. Before the upsert, delete
the chunks of this URL **if its stored text is different**:

```go
_, err := s.db.Exec(`
	DELETE FROM chunks WHERE page_id =
		(SELECT id FROM pages WHERE url = ? AND text <> ?)`,
	p.URL, p.Text)
```

`<>` is SQL for "not equal". If the page is new, or its text is unchanged,
the subquery finds no row, and the `DELETE` matches nothing. If the text
changed, the chunks go, and the next `index` picks the page up again.

This runs as its own statement, before the upsert, not in a transaction. Is
that safe? If Butler stopped between the two, the chunks would be gone and
the page would still have its old text. The next `index` would then re-embed
the old text: wasted work, but not wrong. That's the question to ask whenever
you leave out a transaction.

---

## Step 9: Test the store

Add `TestChunks` to `store_test.go`. Make a helper so the vectors are short:

```go
func testChunks(texts ...string) []Chunk {
	var cs []Chunk
	for _, t := range texts {
		cs = append(cs, Chunk{Text: t, Embedding: []float32{1, 2, 3}})
	}
	return cs
}
```

`texts ...string` makes this a **variadic** function: it takes any number of
strings, and inside, `texts` is a `[]string`. `fmt.Println` works the same
way.

**Your task:** check, in order:

1. Save two pages, `a` and `b`. `PagesToIndex` returns both.
2. `SaveChunks(a, testChunks("x", "y"))`. `CountChunks` is 2, and
   `PagesToIndex` returns only `b`.
3. `SaveChunks(a, testChunks("z"))`. `CountChunks` is 1: the new chunks
   replace the old ones.
4. `SaveChunks` for a URL that was never saved returns `ErrNotFound`
   (`errors.Is`).
5. Save page `a` again with the **same** text. `PagesToIndex` still returns
   only `b`.
6. Save page `a` again with **different** text. `PagesToIndex` returns both,
   and `CountChunks` is 0.
7. `DeleteAllChunks`, then `CountChunks` is 0.

You'll need to compare URLs in steps 1, 2, 5 and 6. A small helper
`urls(pages []Page) []string` keeps the test short.

---

## Step 10: `butler index` for real

Change `runIndex`:

1. **New flags:**
   - `-model` (default `"nomic-embed-text"`), `"Ollama embedding model"`
   - `-ollama` (default `ollama.DefaultURL`), `"Ollama server URL"`
   - `-force` (`fs.Bool`, default `false`), `"re-embed every page, not just new ones"`
2. **Keep `-show`** as it is. It's still useful for looking at chunks, and it
   doesn't need Ollama.
3. **Replace** the counting loop. After opening the store:
   - If `*force`, call `DeleteAllChunks`.
   - `pages, err := st.PagesToIndex()`.
   - `client := &ollama.Client{BaseURL: *ollamaURL}`.
4. For each page, with `i` for the `[i/n]` counter:
   1. `texts, err := chunk.Split(...)`. If there are no texts, `continue`.
      (`crawl` never saves empty pages, so this shouldn't happen, but
      `Embed` with no inputs would be a wasted request.)
   2. Build the inputs for the model, each chunk with a prefix:
      ```go
      inputs := make([]string, len(texts))
      for j, t := range texts {
      	inputs[j] = "search_document: " + t
      }
      ```
   3. `vecs, err := client.Embed(*model, inputs)`. Wrap an error:
      `fmt.Errorf("embedding %s: %w", p.URL, err)`.
   4. Build a `[]store.Chunk` from `texts[j]` and `vecs[j]`. Save the
      original text, not the input with the prefix.
   5. `st.SaveChunks(p.URL, chunks)`, return its error.
   6. Print `[1/7] https://go.dev/doc/  11 chunks`.
5. At the end, print
   `indexed 7 pages, 263 chunks (263 in total)` using `CountChunks` for the
   total.

`main.go` now also imports `butler/internal/ollama`.

> **Why `search_document: `?** `nomic-embed-text` was trained with a prefix
> saying what each text is for: `search_document: ` for text to be found, and
> `search_query: ` for the question doing the finding. Using them noticeably
> improves which chunks match. Milestone 7 adds `search_query: ` to your
> question. This is specific to this model; other embedding models have their
> own conventions or none, so check the model's page before switching.

> **One page, one request.** Sending all of a page's chunks in one `Embed`
> call is far faster than one call per chunk. The spec's 241 chunks in one
> request is fine for Ollama. Very large batches can be split up in milestone
> 8 if they ever cause trouble.

Since each page is saved as soon as it's embedded, stopping `index` with
Ctrl-C halfway loses nothing: the next run continues with the pages that are
left.

Run the **Done when** commands. Then stop Ollama
(`brew services stop ollama`), run `index -force`, and check you get the "is
Ollama running?" error. Start it again afterwards.

> ⚠️ `-force` deletes every chunk **before** embedding starts. If it then
> fails, you have no chunks until you run `index` again. That's fine for
> Butler, because nothing is lost that can't be rebuilt from `pages`.

---

## Errors you'll likely see

| Error / symptom | Meaning |
|---|---|
| `is Ollama running? ... connection refused` | Ollama isn't running. `brew services start ollama`, or `ollama serve` in another terminal. |
| `ollama: model 'nomic-embed-text' not found` | `ollama pull nomic-embed-text`. |
| `ollama: 404 Not Found` (no message) | The URL path is wrong, for example `/embed` instead of `/api/embed`, or `-ollama` has an extra path on it. |
| Test: `request body = {Model: Input:[]}` | A struct tag is wrong or a field is lowercase, so the JSON key doesn't match. |
| Test: `got 0 embeddings` with no error | The response field's tag doesn't say `embeddings`, so nothing is decoded into it. Missing fields aren't errors. |
| `asked for 3 embeddings, got 0` | Same as above, outside the test. |
| `index` says `indexed 0 pages` on the first run | `chunks` already has rows for every page, from an earlier run. Use `-force`. |
| `UNIQUE constraint failed: chunks.page_id, chunks.seq` | The `DELETE` in `SaveChunks` is missing, or runs on `s.db` instead of `tx`. |
| `database is locked` | A statement inside the transaction used `s.db` instead of `tx`. It runs on a different connection, which is blocked by the transaction's own lock. |
| `length(embedding)` is 6144 | You're encoding `float64`s, 8 bytes each. Use `float32` throughout. |
| Embedding round trip gives different numbers | Encoding and decoding use different byte orders. Both must use `binary.LittleEndian`. |
| A re-crawled page never gets new chunks | The stale-chunk `DELETE` in `SavePage` is missing, or runs after the upsert (when the text already matches). |

---

## Check yourself

- What does a struct tag do? What happens to a lowercase field in
  `json.Marshal`?
- Why doesn't `embedResponse` declare every field Ollama sends?
- What's the difference between `http.Post` returning an error and returning
  a 404 response?
- Why does `Embed` check the number of embeddings it got back?
- How many bytes does one 768-number embedding take, and why?
- Why does the byte order have to be named explicitly?
- What does `defer tx.Rollback()` do after a successful `Commit`?
- What could go wrong if `SaveChunks` didn't use a transaction?
- Why must the stale-chunk `DELETE` in `SavePage` run before the upsert, not
  after?
- Why does the database store the chunk text without `search_document: `?

When it's working, send me `ollama.go`, `ollama_test.go`, `store.go`,
`store_test.go` and `main.go` for review. Milestone 7 is `butler ask`: embed
the question, find the closest chunks, and have the chat model answer from
them.

<details>
<summary>Stuck? Skeleton of <code>Embed</code> (spoiler, not the full answer)</summary>

```go
func (c *Client) Embed(model string, inputs []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: model, Input: inputs})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(c.BaseURL+"/api/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("is Ollama running? %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var out embedResponse
	// decode resp.Body into &out; error? wrap it
	// len(out.Embeddings) != len(inputs)? error
	return out.Embeddings, nil
}
```

Imports for `ollama.go`: `bytes`, `encoding/json`, `fmt`, `net/http`.

</details>

<details>
<summary>Stuck? Skeleton of <code>SaveChunks</code> (spoiler, not the full answer)</summary>

```go
func (s *Store) SaveChunks(url string, chunks []Chunk) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	var pageID int64
	err = tx.QueryRow(`SELECT id FROM pages WHERE url = ?`, url).Scan(&pageID)
	// sql.ErrNoRows? return ErrNotFound. Other error? return it.

	if _, err := tx.Exec(`DELETE FROM chunks WHERE page_id = ?`, pageID); err != nil {
		return fmt.Errorf("saving chunks for %s: %w", url, err)
	}
	for i, c := range chunks {
		// tx.Exec the INSERT with pageID, i, c.Text, encodeVector(c.Embedding)
	}
	return tx.Commit()
}
```

New imports for `store.go`: `encoding/binary`, `math`.

</details>
