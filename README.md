# Butler

Butler is a local, private knowledge bank. It crawls sites you choose, stores the
text in a SQLite file on your machine, and answers questions about it using a
local LLM through [Ollama](https://ollama.com).

It uses retrieval (RAG): pages are split into chunks, each chunk is embedded,
and a question is answered from the closest chunks, with their URLs cited.

## Requirements

- Go 1.26+
- Ollama running locally (default `http://localhost:11434`) with these models:

```sh
ollama pull nomic-embed-text   # embeddings
ollama pull qwen3:8b           # chat
```

## Build

```sh
go build -o butler .
go test ./...
```

## Quick start

```sh
./butler crawl https://go.dev/doc/       # crawl a site and save page text
./butler index                           # chunk and embed the saved pages
./butler ask "how do Go modules work?"   # answer from the saved pages, with sources
```

## Commands

Every command takes `-db <file>` (default `butler.db`).

### crawl

Crawls one site, extracts the readable text of each page and saves it. The site
is remembered so `refresh` can crawl it again.

```sh
./butler crawl https://example.com
./butler crawl -max-pages 200 -max-depth 4 https://example.com
./butler crawl -workers 8 -delay 500ms https://example.com
./butler crawl -db notes.db https://example.com
```

| Flag         | Default | Meaning                                      |
| ------------ | ------- | -------------------------------------------- |
| `-max-pages` | 50      | stop after fetching this many pages          |
| `-max-depth` | 3       | how many links away from the start URL to go |
| `-workers`   | 4       | how many pages to fetch at once              |
| `-delay`     | 1s      | wait between starting requests on workers    |

### refresh

Crawls every saved site again and reports how many pages are new or changed.
Takes the same flags as `crawl`.

```sh
./butler refresh
./butler refresh -max-pages 100
```

### index

Splits pages into chunks and embeds them. Only new or changed pages are embedded
unless `-force` is given.

```sh
./butler index
./butler index -force                                # re-embed every page
./butler index -size 300 -overlap 60                 # bigger chunks
./butler index -show https://example.com/about       # print one page's chunks, no embedding
./butler index -model nomic-embed-text -ollama http://localhost:11434
```

| Flag       | Default                  | Meaning                                |
| ---------- | ------------------------ | -------------------------------------- |
| `-size`    | 200                      | words per chunk                        |
| `-overlap` | 40                       | words shared by neighbouring chunks    |
| `-model`   | `nomic-embed-text`       | Ollama embedding model                 |
| `-ollama`  | `http://localhost:11434` | Ollama server URL                      |
| `-force`   | false                    | re-embed every page, not just new ones |
| `-show`    |                          | print the chunks of this one URL       |

### ask

Finds the chunks closest to the question and asks the chat model, streaming the
answer and listing the sources.

```sh
./butler ask "what is this site about?"
./butler ask -k 8 "how do I configure logging?"
./butler ask -chunks "rate limits"                   # show matching chunks only, no LLM
./butler ask -model llama3.1:8b "summarise the pricing page"
```

| Flag           | Default                  | Meaning                                               |
| -------------- | ------------------------ | ----------------------------------------------------- |
| `-k`           | 5                        | how many chunks to give the chat model                |
| `-model`       | `qwen3:8b`               | Ollama chat model                                     |
| `-embed-model` | `nomic-embed-text`       | embedding model; must match the one `index` used      |
| `-ollama`      | `http://localhost:11434` | Ollama server URL                                     |
| `-chunks`      | false                    | print the matching chunks instead of asking the model |

### stats

Shows page, chunk and site counts, the oldest page, and pages per host.

```sh
./butler stats
```

## Keeping it up to date

```sh
./butler refresh && ./butler index
```

## Layout

| Path               | What it does                                 |
| ------------------ | -------------------------------------------- |
| `main.go`          | subcommands and flags                        |
| `internal/crawler` | concurrent crawler, link finding, robots.txt |
| `internal/extract` | readable text from HTML                      |
| `internal/store`   | SQLite storage for sites, pages and chunks   |
| `internal/chunk`   | splits text into overlapping word chunks     |
| `internal/ollama`  | Ollama embed and chat client                 |
| `internal/rag`     | top-k search and prompt building             |

See [GUIDE.md](GUIDE.md) and [milestones/](milestones/) for how Butler was built.
