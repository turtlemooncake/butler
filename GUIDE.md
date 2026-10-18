# Butler — Build Guide

Butler is a local, private knowledge bank. It crawls sites you choose, stores the
text on your machine, and answers questions about it using a local LLM. Nothing
leaves your computer except the crawler's requests to the sites themselves.

```
butler crawl https://example.com --max-pages 200   # crawl, extract text, save pages
butler index                                       # chunk + embed pages into the store
butler ask "how does X work?"                      # retrieve chunks → local LLM → answer + sources
```

Each milestone has its own file in [milestones/](milestones/). They're written
one at a time as you reach them.

---

## Approach: retrieval (RAG), not fine-tuning

Butler doesn't retrain the model. Instead:

1. The crawler saves pages as clean text.
2. The text is split into chunks, and each chunk is turned into an **embedding**
   (a list of numbers that captures its meaning).
3. When you ask a question, the question is embedded too. Butler finds the chunks
   with the closest embeddings and gives them to the model with the question.
4. The model answers from those chunks and cites their URLs.

This way, new crawls are searchable right away, answers come from real pages,
and it all runs comfortably on a laptop.

```
crawler ──> raw pages (HTML) ──> extract ──> chunks + metadata (url, title, date)
                                                  │
                                       Ollama: embedding model
                                                  │
                                         SQLite (vectors as BLOBs)
                                                  │
question ──> embed ──> top-k similar chunks ──> Ollama: chat model ──> answer + sources
```

---

## How Ollama fits in

Ollama is a **separate program** that runs on your machine as a small server. It:

- downloads and stores models (in `~/.ollama/models`)
- loads them onto your Mac's GPU (Apple Metal)
- exposes an **HTTP + JSON API** at `http://localhost:11434`

Butler never loads a model itself. It just sends HTTP requests to Ollama, the
same way it fetches web pages:

| Butler needs | Endpoint | Sends | Gets back |
|---|---|---|---|
| Embed text | `POST /api/embed` | `{"model":"nomic-embed-text","input":["chunk 1","chunk 2"]}` | `{"embeddings":[[0.12,-0.03,...],[...]]}` |
| Answer a question | `POST /api/chat` | `{"model":"qwen3:8b","messages":[{"role":"user","content":"..."}],"stream":false}` | `{"message":{"role":"assistant","content":"..."}}` |

**It works with any language.** Anything that can send HTTP requests can use
Ollama: Go, Python, JavaScript, even `curl`. You can try it from the terminal
before writing any Go:

```sh
curl http://localhost:11434/api/chat -d '{
  "model": "qwen3:8b",
  "messages": [{"role": "user", "content": "Say hi in five words."}],
  "stream": false
}'
```

(Ollama itself happens to be written in Go, and it has an official Go client at
`github.com/ollama/ollama/api`. We'll use plain `net/http` instead so you learn
how it works. Switching to the client later is easy.)

**Custody:** the models and your crawled data both stay on disk locally. The
only time Ollama goes online is when you run `ollama pull`.

### Setup

```sh
brew install ollama
brew services start ollama      # runs it in the background (or: `ollama serve` in a terminal)
ollama pull nomic-embed-text    # embedding model (~270 MB)
ollama pull qwen3:8b            # chat model (~5 GB); llama3.1:8b also works on 16 GB
```

---

## Project layout (target)

```
butler/
  go.mod
  main.go                 # reads the subcommand, calls into packages
  GUIDE.md
  milestones/
  internal/
    crawler/crawler.go    # fetch pages, follow links, limits, politeness
    extract/extract.go    # HTML → clean text + title
    store/store.go        # SQLite: pages, chunks, embeddings
    chunk/chunk.go        # split text into overlapping chunks
    ollama/ollama.go      # tiny HTTP client for embed + chat
    rag/rag.go            # similarity search + prompt building
```

Each folder is a Go **package**. Code under `internal/` can only be imported by
this project.

## Libraries

| Need | Choice | Why |
|---|---|---|
| HTTP | `net/http` (stdlib) | Built in |
| Link parsing | `golang.org/x/net/html` | Semi-official HTML tokenizer |
| Main-text extraction | `github.com/go-shiori/go-readability` | Strips nav, ads and boilerplate |
| robots.txt | `github.com/temoto/robotstxt` | Respect site rules |
| Storage | `modernc.org/sqlite` | Pure-Go SQLite, no C toolchain |
| Vector search | Hand-written cosine similarity | ~15 lines, fast enough for tens of thousands of chunks |
| LLM + embeddings | Ollama over HTTP | See above |
| CLI | `flag` (stdlib) | Enough for a few subcommands |

---

## Milestones

| # | Goal | You'll learn | File |
|---|---|---|---|
| 0 | `butler <subcommand>` prints which command ran; a stub `crawler` package | modules, `package main`, imports, `os.Args`, `switch`, exported names | [00-hello-subcommands.md](milestones/00-hello-subcommands.md) |
| 1 | Fetch one page; print status and size | multiple returns, `if err != nil`, `defer`, `io.ReadAll` | *coming* |
| 2 | Find all links on a page as absolute URLs | `net/url`, `x/net/html`, slices, `for range` | *coming* |
| 3 | Breadth-first crawl with same-host, max pages/depth, delay, User-Agent, robots.txt | structs, maps as sets, slice-as-queue, `time`, `flag` | *coming* |
| 4 | Extract main text and save pages to SQLite | `go get`, `database/sql`, placeholders | *coming* |
| 5 | Split pages into overlapping chunks | `strings.Fields`, slice expressions, edge cases | *coming* |
| 6 | Embed chunks via Ollama and store vectors | struct tags, `encoding/json`, `http.Post`, BLOBs | *coming* |
| 7 | `butler ask`: retrieve top-k, prompt the chat model, print answer + sources | `math`, `sort.Slice`, closures, `strings.Builder` | *coming* |
| 8 | Polish: concurrent crawl, streaming answers, re-crawl, `stats` | goroutines, channels, `sync`, rate limiting | *coming* |

---

## Go cheat sheet

```go
// Errors are values, returned last. Check them every time.
resp, err := http.Get(u)
if err != nil {
    return fmt.Errorf("fetching %s: %w", u, err) // wrap and return upward
}
defer resp.Body.Close() // runs when the surrounding function returns

// := declares + assigns (inside functions only). var is the long form.
count := 0
var visited = map[string]bool{} // map used as a set

// Structs + JSON tags
type EmbedRequest struct {
    Model string   `json:"model"`
    Input []string `json:"input"`
}

// Slices: append, range, and a slice as a queue
queue := []string{start}
for len(queue) > 0 {
    next := queue[0]
    queue = queue[1:]
    queue = append(queue, "another")
    _ = next
}

// Capitalized = exported (visible to other packages); lowercase = private.
func Crawl(start string) error { return nil } // callable as crawler.Crawl
```

## Habits

- `gofmt -w .` formats your code. Go has one style, so you never have to argue about it.
- `go vet ./...` catches common mistakes.
- `go run .` builds and runs; `go build` produces a `./butler` binary.
- Unused variables and imports are **compile errors** in Go. Use `_ = x` to
  silence one temporarily while you're working.
- When you get stuck, paste the code and the exact error.
