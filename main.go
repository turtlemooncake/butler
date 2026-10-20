package main

import (
	"errors"
	"flag"
	"fmt"
	"maps"
	"net/url"
	"os"
	"slices"
	"strings"
	"time"

	"butler/internal/chunk"
	"butler/internal/crawler"
	"butler/internal/extract"
	"butler/internal/ollama"
	"butler/internal/rag"
	"butler/internal/store"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: butler <crawl|refresh|index|ask> [args]")
	os.Exit(1)
	return
}

// used by crawl and refresh
func crawlFlags(fs *flag.FlagSet) (opts *crawler.Options, dbPath *string) {
	opts = &crawler.Options{}
	fs.IntVar(&opts.MaxPages, "max-pages", 50, "stop after fetching this many pages")
	fs.IntVar(&opts.MaxDepth, "max-depth", 3, "how many links away from the start URL to go")
	fs.DurationVar(&opts.Delay, "delay", time.Second, "wait between starting requests on workers")
	fs.IntVar(&opts.Workers, "workers", 4, "how many pages to fetch at once")
	dbPath = fs.String("db", "butler.db", "SQLite database file")
	return opts, dbPath
}

// crawlSite crawls start and saves each page's text. It returns how many
// pages it saved, and how many of those were new or had changed.
func crawlSite(st *store.Store, start string, opts crawler.Options) (saved, changed int, err error) {
	err = crawler.Crawl(start, opts, func(p crawler.Page) error {
		title, text, err := extract.Extract(p.URL, p.Body)
		if err != nil {
			fmt.Printf("not saved: %s: %s\n", p.URL, err)
			return nil
		}
		if text == "" {
			fmt.Printf("not saved: %s: no text found\n", p.URL)
			return nil
		}

		// A new page gives ErrNotFound and an empty old, so it counts as changed.
		old, err := st.GetPage(p.URL)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			return err
		}
		if old.Text != text {
			changed++
		}

		saved++
		return st.SavePage(store.Page{URL: p.URL, Title: title, Text: text, FetchedAt: time.Now()})
	})
	return saved, changed, err
}

// crawls one site and saves each page's text
func runCrawl(args []string) error {
	fs := flag.NewFlagSet("crawl", flag.ExitOnError)
	opts, dbPath := crawlFlags(fs)
	fs.Parse(args)

	if fs.NArg() != 1 {
		fmt.Fprintln(os.Stderr, "usage: butler crawl [flags] <url>")
		fs.PrintDefaults()
		os.Exit(1)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	saved, changed, err := crawlSite(st, fs.Arg(0), *opts)
	if err != nil {
		return err
	}

	// save site after good crawl
	if err := st.SaveSite(fs.Arg(0)); err != nil {
		return err
	}

	pagesTotal, err := st.CountPages()
	if err != nil {
		return err
	}
	fmt.Printf("saved %d pages to %s, %d new or changed (%d in total)\n", saved, *dbPath, changed, pagesTotal)
	return nil
}

// crawls every saved site again
func runRefresh(args []string) error {
	fs := flag.NewFlagSet("refresh", flag.ExitOnError)
	opts, dbPath := crawlFlags(fs)
	fs.Parse(args)

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: butler refresh [flags]")
		fs.PrintDefaults()
		os.Exit(1)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	sites, err := st.ListSites()
	if err != nil {
		return err
	}
	if len(sites) == 0 {
		fmt.Printf("no sites in %s; run \"butler crawl\" first\n", *dbPath)
		return nil
	}

	changed := 0
	for _, site := range sites {
		fmt.Println("refreshing", site)
		_, c, err := crawlSite(st, site, *opts)
		if err != nil {
			fmt.Printf("  error: %v\n", err) // one broken site shouldn't stop the rest
		}
		changed += c
	}

	fmt.Printf("refreshed %d sites, %d pages new or changed\n", len(sites), changed)
	if changed > 0 {
		fmt.Println(`run "butler index" to embed them`)
	}
	return nil
}

// splits every stored page into chunks
func runIndex(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	dbPath := fs.String("db", "butler.db", "SQLite database file")
	size := fs.Int("size", 200, "words per chunk")
	overlap := fs.Int("overlap", 40, "words shared by neighbouring chunks")
	show := fs.String("show", "", "print the chunks of this one URL")
	model := fs.String("model", "nomic-embed-text", "Ollama embedding model")
	ollamaURL := fs.String("ollama", ollama.DefaultURL, "Ollama server URL")
	force := fs.Bool("force", false, "re-embed every page, not just new ones")

	fs.Parse(args)

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: butler index [flags]")
		fs.PrintDefaults()
		os.Exit(1)
	}

	if _, err := chunk.Split("", *size, *overlap); err != nil {
		return err
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	if *show != "" {
		p, err := st.GetPage(*show)
		if err != nil {
			return fmt.Errorf("%s: %w", *show, err)
		}
		chunks, err := chunk.Split(p.Text, *size, *overlap)
		if err != nil {
			return err
		}

		fmt.Printf("%s: %d words, %d chunks\n", p.URL, len(strings.Fields(p.Text)), len(chunks))
		for i, c := range chunks {
			fmt.Printf("\n--- chunk %d of %d ---\n%s\n", i+1, len(chunks), c)
		}
		return nil
	}

	if *force {
		if err := st.DeleteAllChunks(); err != nil {
			return err
		}
	}

	pages, err := st.PagesToIndex()
	if err != nil {
		return err
	}

	client := &ollama.Client{BaseURL: *ollamaURL}
	indexed, added := 0, 0
	for i, p := range pages {
		texts, err := chunk.Split(p.Text, *size, *overlap)
		if err != nil {
			return err
		}
		if len(texts) == 0 {
			continue
		}

		// nomic-embed-text expects this prefix on text that will be searched.
		inputs := make([]string, len(texts))
		for j, t := range texts {
			inputs[j] = "search_document: " + t
		}

		vecs, err := client.Embed(*model, inputs)
		if err != nil {
			return fmt.Errorf("embedding %s: %w", p.URL, err)
		}

		chunks := make([]store.Chunk, len(texts))
		for j := range texts {
			chunks[j] = store.Chunk{Text: texts[j], Embedding: vecs[j]}
		}
		if err := st.SaveChunks(p.URL, chunks); err != nil {
			return err
		}

		indexed++
		added += len(chunks)
		fmt.Printf("[%d/%d] %s  %d chunks\n", i+1, len(pages), p.URL, len(chunks))
	}

	total, err := st.CountChunks()
	if err != nil {
		return err
	}
	fmt.Printf("indexed %d pages, %d chunks (%d in total)\n", indexed, added, total)
	return nil
}

// finds the chunks closest to the question and asks the chat model
func runAsk(args []string) error {
	fs := flag.NewFlagSet("ask", flag.ExitOnError)
	dbPath := fs.String("db", "butler.db", "SQLite database file")
	k := fs.Int("k", 5, "how many chunks to give the chat model")
	model := fs.String("model", "qwen3:8b", "Ollama chat model")
	embedModel := fs.String("embed-model", "nomic-embed-text", "Ollama embedding model; must match the one index used")
	ollamaURL := fs.String("ollama", ollama.DefaultURL, "Ollama server URL")
	showChunks := fs.Bool("chunks", false, "print the matching chunks instead of asking the chat model")

	fs.Parse(args)

	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: butler ask [flags] <question>")
		fs.PrintDefaults()
		os.Exit(1)
	}
	question := strings.Join(fs.Args(), " ")

	if *k < 1 {
		return fmt.Errorf("-k must be at least 1, got %d", *k)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	chunks, err := st.AllChunks()
	if err != nil {
		return err
	}
	if len(chunks) == 0 {
		return fmt.Errorf("no chunks in %s; run \"butler index\" first", *dbPath)
	}

	// nomic-embed-text expects this prefix on the text doing the searching.
	client := &ollama.Client{BaseURL: *ollamaURL}
	vecs, err := client.Embed(*embedModel, []string{"search_query: " + question})
	if err != nil {
		return fmt.Errorf("embedding the question: %w", err)
	}

	hits, err := rag.TopK(vecs[0], chunks, *k)
	if err != nil {
		return err
	}

	if *showChunks {
		for i, h := range hits {
			fmt.Printf("[%d] %.2f  %s  (chunk %d)\n%s\n\n", i+1, h.Score, h.Chunk.URL, h.Chunk.Seq, h.Chunk.Text)
		}
		return nil
	}

	messages := []ollama.Message{
		{Role: "system", Content: rag.SystemPrompt},
		{Role: "user", Content: rag.BuildPrompt(question, hits)},
	}
	err = client.Chat(*model, messages, func(s string) { fmt.Print(s) })
	fmt.Println()
	if err != nil {
		return fmt.Errorf("asking %s: %w", *model, err)
	}

	if err != nil {
		return fmt.Errorf("asking %s: %w", *model, err)
	}

	fmt.Println()
	fmt.Println("Sources:")
	for i, h := range hits {
		fmt.Printf("  [%d] %.2f  %s\n", i+1, h.Score, h.Chunk.URL)
	}
	return nil
}

func runsStats(args []string) error {
	fs := flag.NewFlagSet("index", flag.ExitOnError)
	dbPath := fs.String("db", "butler.db", "SQLite database file")
	fs.Parse(args)

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: butler refresh [flags]")
		fs.PrintDefaults()
		os.Exit(1)
	}

	st, err := store.Open(*dbPath)
	if err != nil {
		return err
	}
	defer st.Close()

	pages, err := st.ListPages()
	if err != nil {
		return err
	}

	chunkCount, err := st.CountChunks()
	if err != nil {
		return err
	}

	sites, err := st.ListSites()
	if err != nil {
		return err
	}

	fmt.Printf("Pages: %d, Chunks: %d, Sites: %d \n", len(pages), chunkCount, len(sites))

	var oldest time.Time
	perHost := map[string]int{}
	for _, page := range pages {
		if u, err := url.Parse(page.URL); err == nil {
			perHost[u.Host]++
		}
		if oldest.IsZero() || page.FetchedAt.Before(oldest) {
			oldest = page.FetchedAt
		}
	}
	if len(pages) > 0 {
		fmt.Printf("Oldest page: %s\n", oldest.Local().Format(time.DateTime))
	}

	sortedPerHost := slices.Sorted(maps.Keys(perHost))
	fmt.Println("Hosts and their page counts:")
	for _, host := range sortedPerHost {
		fmt.Printf("%s: %d \n", host, perHost[host])
	}

	return nil
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "crawl":
		if err := runCrawl(args); err != nil {
			fmt.Fprintln(os.Stderr, "crawl:", err)
			os.Exit(1)
		}
	case "refresh":
		if err := runRefresh(args); err != nil {
			fmt.Fprintln(os.Stderr, "refresh:", err)
			os.Exit(1)
		}
	case "index":
		if err := runIndex(args); err != nil {
			fmt.Fprintln(os.Stderr, "index:", err)
			os.Exit(1)
		}
	case "ask":
		if err := runAsk(args); err != nil {
			fmt.Fprintln(os.Stderr, "ask:", err)
			os.Exit(1)
		}
	case "stats":
		if err := runsStats(args); err != nil {
			fmt.Fprintln(os.Stderr, "stats:", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
	}
}
