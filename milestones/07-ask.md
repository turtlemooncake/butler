# Milestone 7: Ask a question

**Goal:** `butler ask` does what Butler is for. It embeds your question, finds
the stored chunks whose embeddings are closest to it, hands those chunks to
the chat model as numbered sources, and prints the model's answer followed by
the sources it was given. `-chunks` shows what retrieval found without
calling the chat model, so you can tell a bad _search_ apart from a bad
_answer_.

**Done when:**

```sh
$ go run . ask what is a slice in Go
A slice is a descriptor for a contiguous segment of an underlying array, and
it gives access to a numbered sequence of elements from that array [1]. Unlike
an array, its length can change while the program runs [1]. You can make a
new slice with the built-in make function or with a slice literal [2].

Sources:
  [1] 0.82  https://go.dev/ref/spec
  [2] 0.79  https://go.dev/ref/spec
  [3] 0.71  https://go.dev/ref/spec
  [4] 0.66  https://go.dev/doc/
  [5] 0.64  https://go.dev/learn/
```

(The answer, the scores and the pages will differ, and the model words things
differently every time.) The numbers in brackets are the model citing its
sources. The number after each source is its **similarity score**: how close
that chunk's embedding is to the question's.

```sh
$ go run . ask -chunks -k 2 what is a slice in Go
[1] 0.82  https://go.dev/ref/spec  (chunk 114)
Slice types A slice is a descriptor for a contiguous segment of an underlying
array and provides access to a numbered sequence of elements from that array.
...

[2] 0.79  https://go.dev/ref/spec  (chunk 115)
...
```

A question the pages don't answer should get an "I don't know":

```sh
$ go run . ask what is the capital of France
The sources don't say what the capital of France is.

Sources:
  ...
```

Errors, not crashes:

```sh
$ go run . ask
usage: butler ask [flags] <question>
  -chunks
    	print the matching chunks instead of asking the chat model
  ...
$ echo $?
1

$ go run . ask -db empty.db hello
ask: no chunks in empty.db; run "butler index" first

$ go run . ask -model nope hello
ask: asking nope: ollama: model 'nope' not found
```

```sh
$ go test ./...
?   	butler	[no test files]
ok  	butler/internal/chunk	0.233s
ok  	butler/internal/crawler	0.415s
ok  	butler/internal/extract	0.602s
ok  	butler/internal/ollama	0.318s
ok  	butler/internal/rag	0.207s
ok  	butler/internal/store	0.744s
```

**Concepts:** cosine similarity, `math.Sqrt`, `float32` → `float64`
conversion, float division by zero and `NaN`, `sort.Slice`, closures that
capture a slice, `strings.Builder`, `fmt.Fprintf` into anything that's an
`io.Writer`, a first look at interfaces, `any`, SQL `JOIN`, chat roles
(`system`, `user`, `assistant`), why `"stream": false` matters,
`json.Unmarshal`, decoding into `map[string]any`.

---

## How retrieval works

Milestone 6 gave every chunk an embedding: 768 numbers, a point in a
768-dimensional space, placed so that texts with similar meanings point in
similar directions. A question is just another piece of text, so:

1. Embed the question (with the `search_query: ` prefix).
2. Score every chunk by how closely its vector points the same way as the
   question's.
3. Keep the best `k`.

That's the whole search. There's no index structure and no clever data
structure: Butler compares the question against **every** chunk, every time.
That sounds slow, but a comparison is 768 multiplications and additions, and a
laptop does tens of millions of those a second. 263 chunks take well under a
millisecond. Even 50,000 chunks, a few hundred big sites, take a fraction of a
second. Vector databases exist for millions of vectors. Butler doesn't need
one.

**Then generation.** The chat model has never seen your pages, and it can't
search them. So Butler pastes the best chunks into the prompt as numbered
sources and asks it to answer from those, citing the numbers. The model's
job shrinks from "know everything" to "read five paragraphs and summarise
the relevant part", which an 8B model is good at.

---

## How the pieces fit

```
main.go  runAsk(args)
  ├─ flags: -db, -k, -model, -embed-model, -ollama, -chunks
  ├─ st := store.Open(...)
  ├─ chunks := st.AllChunks()                         ← new: JOIN pages, decode vectors
  ├─ q := client.Embed(embedModel, ["search_query: " + question])
  ├─ hits := rag.TopK(q, chunks, k)                   ← new, internal/rag
  │            └─ rag.Cosine(q, chunk) for each chunk, sort, keep k
  │
  ├─ -chunks?  print hits, stop
  │
  ├─ prompt := rag.BuildPrompt(question, hits)        ← numbered sources + question
  ├─ answer := client.Chat(model, [system, user])     ← new, POST /api/chat
  └─ print answer, then the sources
```

`rag` is the new package. It does no I/O at all: vectors and text in, scores
and a prompt out. Like `chunk`, that makes it easy to test thoroughly. It
imports `store` for the chunk type, but never opens a database.

---

## Step 0: Pull the chat model and try it by hand

```sh
ollama pull qwen3:8b      # about 5 GB; llama3.1:8b also works
```

Then ask it something directly:

```sh
$ curl -s http://localhost:11434/api/chat -d '{
  "model": "qwen3:8b",
  "messages": [
    {"role": "system", "content": "Answer in one sentence."},
    {"role": "user", "content": "Why is the sky blue?"}
  ],
  "stream": false,
  "think": false
}'
{"model":"qwen3:8b","created_at":"...","message":{"role":"assistant","content":"The sky looks blue because ..."},"done":true,...}
```

The first request takes a while: Ollama has to load 5 GB into memory. After
that it stays loaded for a few minutes, and answers come much faster.

**Messages and roles.** A chat is a list of messages, each with a `role`:

| Role        | Who                           | Butler uses it for                           |
| ----------- | ----------------------------- | -------------------------------------------- |
| `system`    | The rules of the conversation | "Answer only from the sources, cite them"    |
| `user`      | The person asking             | The sources and the question                 |
| `assistant` | The model's earlier replies   | Not used: each `ask` is a fresh conversation |

The model doesn't remember anything between requests. Every request carries
the whole conversation. A chat app sends all earlier messages each time,
including the model's own replies as `assistant` messages.

**Now try it without `"stream": false`:**

```sh
$ curl -s http://localhost:11434/api/chat -d '{
  "model": "qwen3:8b", "think": false,
  "messages": [{"role": "user", "content": "Why is the sky blue?"}]
}'
{"model":"qwen3:8b",...,"message":{"role":"assistant","content":"The"},"done":false}
{"model":"qwen3:8b",...,"message":{"role":"assistant","content":" sky"},"done":false}
{"model":"qwen3:8b",...,"message":{"role":"assistant","content":" looks"},"done":false}
...
```

By default Ollama **streams**: it sends one small JSON object per piece of the
answer, as the model produces it. That's what makes chat apps show text as
it's written. It's also a trap: `json.NewDecoder(...).Decode` reads **one**
JSON value and stops, so a client that forgets `"stream": false` gets an answer
of one word, with no error. Milestone 8 uses streaming on purpose. For now,
turn it off.

**`"think": false`.** qwen3 is a _thinking_ model: by default it writes out
its reasoning before it answers, which can take longer than the answer itself.
For answering from five short sources you don't need that, so turn it off.

---

## Step 1: Load the chunks

Milestone 6 promised a store method that hands back chunks with their vectors
as `[]float32`. Retrieval also needs to know which page each chunk came from,
and that lives in the `pages` table. The chunks table only has `page_id`.

A **JOIN** combines rows from two tables in one query:

```sql
SELECT pages.url, pages.title, chunks.seq, chunks.text, chunks.embedding
FROM chunks
JOIN pages ON pages.id = chunks.page_id
ORDER BY chunks.id
```

For every chunk, `JOIN ... ON` finds the page whose `id` matches the chunk's
`page_id` and puts that page's columns next to the chunk's. `pages.url` means
"the `url` column of `pages`". Writing the table name in front is required
when both tables have a column with that name (both have `id` and `text`)
and makes the query easier to read in any case.

Add a type for what comes back to `store.go`:

```go
// IndexedChunk is a stored chunk together with the page it came from.
type IndexedChunk struct {
	URL       string
	Title     string
	Seq       int
	Text      string
	Embedding []float32
}
```

`store.Chunk` from milestone 6 stays as it is. It's what you _save_, and the
store works out the page from the URL you pass to `SaveChunks`.

**Your task:** write

```go
// AllChunks returns every stored chunk with its page's URL and title, in the
// order they were saved.
func (s *Store) AllChunks() ([]IndexedChunk, error)
```

It's the same `Query` / `rows.Next` / `rows.Scan` / `rows.Err` shape as
`queryPages`. The new part: scan the `embedding` column into a `[]byte`, then
`decodeVector` it into `c.Embedding`. Return an error from `decodeVector`
wrapped with the chunk's URL and seq, so a corrupt row is easy to find.

This loads every vector into memory: 3 KB per chunk, so about 800 KB for 263
chunks, or 150 MB for 50,000. Fine for Butler. If it ever isn't, the fix is to
score rows as they're read instead of collecting them first.

**Test it.** Add `TestAllChunks` to `store_test.go`:

1. A fresh store: `AllChunks` returns no chunks and no error.
2. Save pages `a` and `b`. `SaveChunks(b, testChunks("b0", "b1"))`, then
   `SaveChunks(a, testChunks("a0"))`.
3. `AllChunks` returns 3 chunks in the order they were saved: `b0`, `b1`, `a0`.
   Check each one's `URL`, `Seq` and `Text`, and that its `Embedding` is
   `[1 2 3]` (`slices.Equal`). That last check proves the bytes made the round
   trip through SQLite.

---

## Step 2: Cosine similarity

How do you measure whether two vectors "point the same way"? Look at two
dimensions first, where you can draw it:

```
        ↑ y
        │   b = (1, 1)
        │  ╱
        │ ╱  45°
        │╱________ a = (1, 0)
        └──────────→ x
```

The angle between `a` and `b` is 45°. The **cosine** of an angle is 1 when
it's 0° (same direction), 0 at 90° (unrelated), and −1 at 180° (opposite). That
makes it a good similarity score, and you can compute it from the numbers
without ever finding the angle:

```
            a · b              a[0]×b[0] + a[1]×b[1] + ...
cos  =  ───────────  =  ─────────────────────────────────────────────
         |a| × |b|      √(a[0]² + a[1]² + ...)  ×  √(b[0]² + b[1]² + ...)
```

- `a · b` is the **dot product**: multiply matching numbers, add them all up.
- `|a|` is the vector's **length**: the square root of the sum of its squares
  (Pythagoras, in as many dimensions as you like).

For the picture: `a · b = 1×1 + 0×1 = 1`, `|a| = 1`, `|b| = √2`, so
`cos = 1/√2 ≈ 0.707`, which is the cosine of 45°.

Dividing by the lengths is what makes this about **direction only**. `(1, 1)`
and `(5, 5)` score exactly 1. (Many embedding APIs, Ollama's among them,
already return vectors of length 1, so for Butler the division changes almost
nothing. Keep it anyway: it makes `Cosine` correct for any vectors, including
the ones in your tests.)

Create `internal/rag/rag.go`:

```go
// Package rag finds the chunks that best match a question and builds the
// prompt that asks a chat model to answer from them.
package rag

// Cosine returns the cosine similarity of a and b: 1 when they point the same
// way, 0 when they're unrelated, -1 when they're opposite. It returns 0 if
// either vector is all zeros. a and b must be the same length.
func Cosine(a, b []float32) float64
```

**Your task:** write it. One loop over the indexes computes all three sums at
once: `dot`, `normA` (the sum of `a[i]²`) and `normB`. Then
`dot / (math.Sqrt(normA) * math.Sqrt(normB))`.

Two details:

- **Add up in `float64`.** Declare the sums as `float64` and convert each
  number before multiplying: `float64(a[i]) * float64(b[i])`. Go never
  converts between number types for you: `float64 * float32` is a compile
  error. A `float32` keeps about 7 significant digits, and over 768 additions
  the rounding errors add up. The embeddings themselves don't need more
  precision, but the sum does.
- **Zero vectors.** If `normA` or `normB` is 0, return 0. Without that check
  you'd compute `0 / 0`. With integers, Go would **panic** on division by
  zero. With floats it doesn't: `0.0 / 0.0` is **NaN** ("not a number"), and
  NaN is worse than a panic, because it's quiet. Every comparison with NaN is
  false, even `NaN == NaN`, so one NaN score makes sorting in Step 3 put
  chunks in a meaningless order, and nothing tells you.

**Test it.** Create `internal/rag/rag_test.go` with a table-driven
`TestCosine`:

| Case                   | `a`         | `b`         | Want               |
| ---------------------- | ----------- | ----------- | ------------------ |
| same                   | `[1, 2, 3]` | `[1, 2, 3]` | 1                  |
| same direction, longer | `[1, 1]`    | `[5, 5]`    | 1                  |
| 45°                    | `[1, 0]`    | `[1, 1]`    | `1 / math.Sqrt(2)` |
| unrelated              | `[1, 0]`    | `[0, 1]`    | 0                  |
| opposite               | `[1, 2]`    | `[-1, -2]`  | -1                 |
| zero vector            | `[0, 0]`    | `[1, 1]`    | 0                  |

⚠️ **Don't compare floats with `==`.** `Cosine([1, 2, 3], [1, 2, 3])` might
come out as `0.9999999999999998`, because `math.Sqrt` rounds too. Check that
the difference is tiny instead:

```go
if math.Abs(got-tc.want) > 1e-9 {
	t.Errorf("Cosine(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
}
```

`1e-9` is 0.000000001, written in scientific notation. Also check the zero
case with `math.IsNaN(got)`: if your zero check is missing, `got` is NaN, and
`math.Abs(NaN - 0) > 1e-9` is false, because NaN compares false with
everything. The test would pass. That's the quietness from above, in action.

---

## Step 3: Keep the best `k`

```go
// Hit is a chunk and how similar it is to the question.
type Hit struct {
	Chunk store.IndexedChunk
	Score float64
}

// TopK scores every chunk against query and returns the k best, highest
// score first. It returns an error if a chunk's embedding is a different
// length from query's.
func TopK(query []float32, chunks []store.IndexedChunk, k int) ([]Hit, error)
```

**Score every chunk.** Build a `[]Hit` with one entry per chunk. Before
calling `Cosine`, check `len(c.Embedding) != len(query)` and return an error
like:

```
chunk 3 of https://go.dev/ has 1024 numbers, the question has 768: was it indexed with a different model?
```

Every embedding model has its own number of dimensions. If you indexed with
one model and ask with another, the vectors don't even have the same length,
and comparing them would be meaningless even if they did. Different models
place meanings differently. This error is the only warning you'd get.

**Sort them.** The `sort` package can sort any slice, if you tell it how to
compare two elements:

```go
sort.Slice(hits, func(i, j int) bool {
	return hits[i].Score > hits[j].Score
})
```

`sort.Slice` doesn't know what a `Hit` is. It knows how to move elements
around, and every time it needs to decide an order, it calls your function
with two **indexes** and asks: "should element `i` come before element `j`?"
`>` puts higher scores first.

The function literal is a **closure**, like the ones in milestone 4. It takes
only `i` and `j`, so how does it see the scores? It **captures** `hits` from
the code around it. `sort.Slice` swaps elements inside that same slice as it
works, and your function, looking through the same variable, always sees the
current order. That's why the parameters are indexes rather than two `Hit`s:
the closure reads the slice itself.

> **The newer way.** Since Go 1.21, `slices.SortFunc` does the same with
> elements instead of indexes, and an `int` result instead of `bool`:
>
> ```go
> slices.SortFunc(hits, func(a, b Hit) int { return cmp.Compare(b.Score, a.Score) })
> ```
>
> (`b` before `a` sorts highest first.) Either is fine. `sort.Slice` is in a
> great deal of existing Go code, so it's worth knowing first.

**Cut to `k`.** `hits[:k]` panics if there are fewer than `k` hits. You've
solved this before, in `chunk.Split`: `hits[:min(k, len(hits))]`.

**Test it.** In two dimensions you can choose the scores by picking
directions. Add `TestTopK`:

```go
func testChunk(text string, emb ...float32) store.IndexedChunk {
	return store.IndexedChunk{URL: "https://example.com/" + text, Text: text, Embedding: emb}
}

func TestTopK(t *testing.T) {
	chunks := []store.IndexedChunk{
		testChunk("north", 0, 1),
		testChunk("east", 1, 0),
		testChunk("west", -1, 0),
		testChunk("northeast", 1, 1),
	}
	query := []float32{1, 0.1} // mostly east, a little north

	// your cases here
}
```

**Your task:** check that

1. `k = 2` gives `east`, then `northeast`.
2. `k = 10` gives all four, in the order `east`, `northeast`, `north`,
   `west`, without panicking.
3. Each `Score` is between -1 and 1, and the scores never go up from one hit
   to the next.
4. Adding `testChunk("odd", 1, 2, 3)` to the chunks makes `TopK` return an error.

Work out the four scores roughly by hand first: which direction is closest
to "mostly east"?

> Avoid ties in sort tests. `sort.Slice` isn't **stable**: two hits with the
> same score can come out in either order, and a test that expects one order
> will fail sometimes.

---

## Step 4: Build the prompt

The prompt is what the chat model sees in the `user` message:

```
Sources:

[1] The Go Programming Language Specification (https://go.dev/ref/spec)
Slice types A slice is a descriptor for a contiguous segment of ...

[2] The Go Programming Language Specification (https://go.dev/ref/spec)
Making slices, maps and channels The built-in function make takes ...

Question: what is a slice in Go
```

The question goes **last**. Models pay most attention to the end of the
prompt, and reading the sources first means the question lands with the
context already in place.

**`strings.Builder`.** You could build this with `prompt += ...` in a loop,
but Go strings can't be changed. Each `+=` makes a brand new string and copies
everything so far into it. Five sources is fine, but it's a habit worth
avoiding. A `strings.Builder` keeps a growing buffer and makes the string
once, at the end:

```go
var b strings.Builder
b.WriteString("Sources:\n\n")
fmt.Fprintf(&b, "[%d] %s (%s)\n", n, title, url)
return b.String()
```

`fmt.Fprintf` is `Printf` that writes somewhere other than the screen. You've
used it with `os.Stderr`. How can the same function write to a terminal and
to a `strings.Builder`? Because its first parameter is an `io.Writer`, which
is an **interface**:

```go
type Writer interface {
	Write(p []byte) (n int, err error)
}
```

An interface lists methods. **Any** type that has those methods can be used
where the interface is expected. You don't declare it anywhere. `os.Stderr`
(a `*os.File`) has a `Write` method, and so does `*strings.Builder`, so both
are `io.Writer`s. So are network connections, files, HTTP response bodies
being written and `bytes.Buffer`. `Fprintf` doesn't know or care which one it
has.

Note the `&b`. The `Write` method belongs to `*strings.Builder`, with a pointer
receiver like your `*Store` methods, so it's the pointer that's an `io.Writer`.

⚠️ Never copy a `strings.Builder` once you've written to it (by passing `b`
instead of `&b` to a function, say). The copy shares the original's buffer,
and the next write to it panics with `illegal use of non-zero Builder copied
by value`.

**Your task:** add to `rag.go`:

```go
// SystemPrompt tells the chat model how to answer from the sources.
const SystemPrompt = `You answer questions using only the numbered sources you are given.
Cite the sources you use, like [1] or [2][3], right after the facts that come from them.
If the sources don't contain the answer, say that you don't know. Don't use anything else you know.
Keep the answer short.`

// BuildPrompt returns the user message for the chat model: the hits as
// numbered sources, then the question.
func BuildPrompt(question string, hits []Hit) string
```

Number the sources from 1, not 0. People and models both count from 1, and
`main` prints the same numbers in the sources list so the citations line up.

**Test it.** `TestBuildPrompt` with two hits:

1. The prompt contains `"[1] Title A (https://example.com/a)\nalpha text"`.
2. It contains `"[2] "` and the second hit's text.
3. It contains `"Question: what is alpha?"`, and that comes **after** both
   sources: `strings.Index(prompt, "Question:")` is greater than
   `strings.Index(prompt, "[2]")`.

Use `strings.Contains` rather than comparing the whole prompt. That way you
can tweak blank lines later without rewriting the test.

---

## Step 5: Share the plumbing in the Ollama client

`Chat` will do exactly what `Embed` does: marshal a request, `http.Post` it,
check the status, decode the reply. Only the path and the types differ. So
before writing `Chat`, pull the shared part out of `Embed`:

```go
// post sends in as JSON to path and decodes the JSON reply into out, which
// must be a pointer.
func (c *Client) post(path string, in, out any) error
```

**`any`** is the empty interface: an interface with no methods at all, which
every type satisfies. A parameter of type `any` accepts any value. (It's a
name for `interface{}`, which you'll see in older code.) You can't do much with
an `any` yourself without first finding out what's inside, but you can pass it
on, and `json.Marshal(v any)` and `Decode(v any)` both take `any`. They look
inside at run time to see what they've been given.

**Your task:**

1. Move `Embed`'s marshal, post, status check and decode into `post`. Decode
   into `out` itself, not `&out`: `out` is already the pointer the caller
   passed.
2. Keep the hint on a failed post: `fmt.Errorf("is Ollama running? %w", err)`.
3. Wrap a decode error with the path: `decoding %s response: %w`.
4. Shrink `Embed` to:
   ```go
   var out embedResponse
   if err := c.post("/api/embed", embedRequest{Model: model, Input: inputs}, &out); err != nil {
   	return nil, err
   }
   // the count check, as before
   return out.Embeddings, nil
   ```

Run `go test ./internal/ollama/`. The three embed tests should pass without a
change. That's your proof the refactor kept the behaviour.

> **What `any` costs.** The compiler can no longer check the types you pass
> to `post`. Pass `out` instead of `&out` and it compiles fine, then fails when
> it runs, with `json: Decode(non-pointer ollama.embedResponse)`. That's the
> trade: one copy of the HTTP code, in exchange for one mistake the compiler
> can't catch. For a private helper with two callers, it's worth it.

---

## Step 6: `Chat`

```go
// Message is one turn of a chat.
type Message struct {
	Role    string `json:"role"` // "system", "user" or "assistant"
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Think    bool      `json:"think"`
}

type chatResponse struct {
	Message Message `json:"message"`
}

// Chat sends messages to model and returns its reply.
func (c *Client) Chat(model string, messages []Message) (string, error)
```

`Message` is exported, because `main` builds the list. The request and response
types stay lowercase, like the embed ones.

`Stream` and `Think` are always `false` here, and `false` is a `bool`'s zero
value, so why have the fields at all? Because a field that isn't in the struct
isn't in the JSON, and Ollama's default for a **missing** `stream` is `true`.
Declaring the field makes `json.Marshal` write `"stream":false` explicitly.

**Your task:** write `Chat` with `post`. If the reply's `Content` is empty,
return an error (`ollama: empty reply from %s`) rather than printing nothing.

**Test it.** Add to `ollama_test.go`:

```go
func TestChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s, want /api/chat", r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request: %v", err)
		}

		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		// your checks on req here

		var raw map[string]any
		json.Unmarshal(body, &raw)
		if raw["stream"] != false {
			t.Errorf(`"stream" = %v, want false`, raw["stream"])
		}

		w.Write([]byte(`{"model":"m","message":{"role":"assistant","content":"hi [1]"},"done":true}`))
	}))
	defer srv.Close()

	// call Chat, check the reply is "hi [1]"
}
```

New things:

- `json.Unmarshal(data, &v)` is `Decode` for a `[]byte` you already have.
  The handler reads the body once with `io.ReadAll`, then decodes it twice.
  A body is a stream, so it can only be read once.
- Why decode into a `map[string]any` as well? `req.Stream` is `false` both
  when the client sent `false` **and** when it sent nothing. The map can tell
  the difference: a missing key gives `nil`, and `nil != false`. Decoding JSON
  into a `map[string]any` keeps whatever was there, untyped: JSON booleans
  become `bool`, strings `string`, numbers `float64`. Comparing an `any` with
  `false` is true only if it holds a `bool` that's `false`.

**Your task:** check that `req.Model` is what you passed, that there are two
messages, and that the first has role `system`. Then write `TestChatError`,
like `TestEmbedError` but against `Chat`, and `TestChatEmpty`, where the server
replies with an empty `content` and `Chat` must return an error.

Try deleting the `Stream` field from `chatRequest` and running the test. It
should fail. Put it back.

---

## Step 7: `butler ask`

Replace the placeholder in the `"ask"` case with `runAsk`, called like
`runCrawl` and `runIndex`, printing `ask: <err>` and exiting with 1 on an error.

```go
// runAsk handles "butler ask": it finds the chunks closest to the question
// and has the chat model answer from them.
func runAsk(args []string) error
```

1. A `FlagSet` named `"ask"` with:
   - `-db` (default `"butler.db"`)
   - `-k` (default `5`), `"how many chunks to give the chat model"`
   - `-model` (default `"qwen3:8b"`), `"Ollama chat model"`
   - `-embed-model` (default `"nomic-embed-text"`), `"Ollama embedding model; must match the one index used"`
   - `-ollama` (default `ollama.DefaultURL`)
   - `-chunks` (`fs.Bool`), `"print the matching chunks instead of asking the chat model"`
2. If `fs.NArg() == 0`, print `usage: butler ask [flags] <question>` and the
   defaults, and exit 1. Otherwise join `fs.Args()` with spaces into the
   question, as milestone 0 did. (Flags go first again: in
   `ask what is -k 3`, the `-k 3` becomes part of the question.)
3. If `*k < 1`, return an error.
4. Open the store, `defer st.Close()`, call `AllChunks`. If there are none,
   return the `no chunks in butler.db; run "butler index" first` error.
5. Embed the question, with the prefix:
   ```go
   vecs, err := client.Embed(*embedModel, []string{"search_query: " + question})
   ```
   wrap an error as `embedding the question: %w`, and use `vecs[0]`.
6. `hits, err := rag.TopK(vecs[0], chunks, *k)`.
7. If `*chunks` (the flag. Call the variable `showChunks` so it doesn't
   clash with the `chunks` slice): print each hit as in **Done when**, with
   `%.2f` for the score (two decimal places), then `return nil`.
8. Build the messages:
   ```go
   messages := []ollama.Message{
   	{Role: "system", Content: rag.SystemPrompt},
   	{Role: "user", Content: rag.BuildPrompt(question, hits)},
   }
   ```
9. `answer, err := client.Chat(*model, messages)`, wrapping an error as
   `asking %s: %w` with the model name.
10. Print `strings.TrimSpace(answer)`, a blank line, `Sources:`, and one line
    per hit: `  [1] 0.82  https://go.dev/ref/spec`.

`main.go` now also imports `butler/internal/rag`.

> **It's slow.** The first `ask` after a while loads both models. After that,
> the embed takes a moment and the answer a few seconds, and you see nothing
> until all of it is done. Milestone 8 streams the answer, so it appears as
> it's written.

---

## Step 8: Try it, and judge it

Ask things you know the pages answer, things they half answer, and things
they don't. When an answer is wrong, find out which half failed:

```sh
go run . ask -chunks what is a slice in Go
```

- **The right chunks aren't in the top `k`:** retrieval failed. Try
  rephrasing with words the page would use, or raise `-k`.
- **The right chunks are there, but the answer is wrong:** generation
  failed. The model misread or ignored its sources. A different `-model`, or
  a clearer `SystemPrompt`, are the levers here.

Some experiments worth a few minutes:

- **The prefix.** Temporarily remove `"search_query: "` from step 5 and
  compare `-chunks` scores and order for the same question. Then put it back.
- **Off-topic questions.** Ask about something your pages never mention.
  Small models don't always say "I don't know", even when told to. Look at
  the scores: when even the best chunk scores low compared with your good
  questions, the pages probably don't have the answer.
- **`-k`.** Try 1, 5 and 20. Too few, and an answer spread over two chunks is
  missed. Too many, and the good sources are buried among weak ones. There's
  also a hard limit: Ollama gives the model a context window of a few thousand
  tokens by default, and a prompt longer than that is **cut without an
  error**. 20 chunks of 200 words is about 5,000 tokens.

---

## Errors you'll likely see

| Error / symptom                                                        | Meaning                                                                                                                 |
| ---------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------------------------- |
| `ollama: model 'qwen3:8b' not found`                                   | `ollama pull qwen3:8b`, or pass `-model` with one you have (`ollama list`).                                             |
| The answer is one word, or one sentence fragment                       | `"stream": false` isn't being sent, so you decoded only the first streamed piece. Check the `Stream` field and its tag. |
| The answer starts with `<think>` and a long monologue                  | `"think": false` isn't being sent.                                                                                      |
| An error mentioning `think` or thinking with a non-qwen model          | That model or your Ollama version rejects the field. Upgrade Ollama, or drop `Think` while you use that model.          |
| `has 1024 numbers, the question has 768`                               | `-embed-model` isn't the model `index` used. Use the same one, or `index -force` with the new one.                      |
| Scores are all `NaN`                                                   | `Cosine` divides by zero for an all-zero vector. Add the zero check.                                                    |
| Scores are all close to 0, and the chunks look random                  | The question was embedded with a different model than the chunks. Or `Cosine` mixes up `a` and `b` in one of the sums.  |
| `invalid operation: ... (mismatched types float64 and float32)`        | Convert each number with `float64(...)` before the multiplication.                                                      |
| `json: Decode(non-pointer ollama.embedResponse)`                       | You passed `out` instead of `&out` to `post`.                                                                           |
| `panic: runtime error: slice bounds out of range [:5] with capacity 3` | `hits[:k]` with fewer hits than `k`. Use `min`.                                                                         |
| `TopK` returns the _worst_ chunks first                                | The `less` function uses `<`. For highest first, it's `hits[i].Score > hits[j].Score`.                                  |
| `illegal use of non-zero Builder copied by value`                      | A `strings.Builder` was copied after writing. Pass `&b`.                                                                |
| `ambiguous column name: text`                                          | A `JOIN` query names a column both tables have. Write `chunks.text`.                                                    |
| `ask what is -k 3` asks about "-k 3"                                   | Flags must come before the question.                                                                                    |
| The model answers questions the sources don't cover                    | Expected sometimes with small models. Tighten `SystemPrompt`, or check the scores with `-chunks`.                       |

---

## Check yourself

- Why does Butler compare the question with every chunk, and roughly how many
  chunks could it handle before that gets slow?
- What do cosine scores of 1, 0 and −1 mean? Why does dividing by the lengths
  make the score about direction only?
- What happens when you divide by zero with integers, and with floats? Why is
  the float case more dangerous?
- Why compare floats in tests with a tolerance instead of `==`?
- `sort.Slice`'s function takes two indexes, not two `Hit`s. How does it see
  the scores?
- Why must the question be embedded with the same model as the chunks?
- What does `strings.Builder` avoid that `+=` in a loop does?
- Why can `fmt.Fprintf` write to both `os.Stderr` and a `*strings.Builder`?
- What does `any` gain `post`, and what does it cost?
- Why does `chatRequest` declare `Stream` when it's always `false`? How does
  the test prove it's sent?
- An answer is wrong. How do you tell whether retrieval or generation failed?

When it's working, send me `rag.go`, `rag_test.go`, `ollama.go`,
`ollama_test.go`, `store.go`, `store_test.go` and `main.go` for review.
Milestone 8 is polish: a concurrent crawl, answers streamed as they're written,
re-crawling, and a `stats` command.

<details>
<summary>Stuck? Skeleton of <code>Cosine</code> and <code>TopK</code> (spoiler, not the full answer)</summary>

```go
func Cosine(a, b []float32) float64 {
	var dot, normA, normB float64
	for i := range a {
		// dot += float64(a[i]) * float64(b[i]); same idea for normA, normB
	}
	if normA == 0 || normB == 0 {
		return 0
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB))
}

func TopK(query []float32, chunks []store.IndexedChunk, k int) ([]Hit, error) {
	hits := make([]Hit, 0, len(chunks))
	for _, c := range chunks {
		// lengths differ? return an error naming c.Seq and c.URL
		hits = append(hits, Hit{Chunk: c, Score: Cosine(query, c.Embedding)})
	}
	sort.Slice(hits, func(i, j int) bool {
		return hits[i].Score > hits[j].Score
	})
	return hits[:min(k, len(hits))], nil
}
```

Imports for `rag.go`: `fmt`, `math`, `sort`, `strings`,
`butler/internal/store`.

</details>

<details>
<summary>Stuck? Skeleton of <code>post</code> and <code>Chat</code> (spoiler, not the full answer)</summary>

```go
func (c *Client) post(path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}
	resp, err := http.Post(c.BaseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("is Ollama running? %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return apiError(resp)
	}
	// decode resp.Body into out (already a pointer); error? wrap it with path
	return nil
}

func (c *Client) Chat(model string, messages []Message) (string, error) {
	var out chatResponse
	err := c.post("/api/chat", chatRequest{Model: model, Messages: messages}, &out)
	// error? return it
	// out.Message.Content empty? return an error
	return out.Message.Content, nil
}
```

`Stream` and `Think` are left out of the `chatRequest` literal: they're
`false` by default, and `json.Marshal` still writes them.

</details>

<details>
<summary>Stuck? Skeleton of <code>AllChunks</code> (spoiler, not the full answer)</summary>

```go
func (s *Store) AllChunks() ([]IndexedChunk, error) {
	rows, err := s.db.Query(`
		SELECT pages.url, pages.title, chunks.seq, chunks.text, chunks.embedding
		FROM chunks JOIN pages ON pages.id = chunks.page_id
		ORDER BY chunks.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var chunks []IndexedChunk
	for rows.Next() {
		var c IndexedChunk
		var emb []byte
		// rows.Scan(&c.URL, &c.Title, &c.Seq, &c.Text, &emb); error? return it
		// c.Embedding, err = decodeVector(emb); error? wrap with c.URL and c.Seq
		chunks = append(chunks, c)
	}
	// rows.Err()
	return chunks, nil
}
```

</details>
