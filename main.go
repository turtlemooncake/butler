package main

import (
	"flag"
	"fmt"
	"os"
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
	fmt.Fprintln(os.Stderr, "usage: butler <crawl|index|ask> [args]")
	os.Exit(1)
	return
}

func runCrawl(args []string) error {
	var opts crawler.Options
	fs := flag.NewFlagSet("crawl", flag.ExitOnError)
	fs.IntVar(&opts.MaxPages, "max-pages", 50, "stop after fetching this many pages")
	fs.IntVar(&opts.MaxDepth, "max-depth", 3, "how many links away from the start URL to go")
	fs.DurationVar(&opts.Delay, "delay", time.Second, "wait between requests")
	dbPath := fs.String("db", "butler.db", "SQLite database file")

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

	saved := 0

	err = crawler.Crawl(fs.Arg(0), opts, func(p crawler.Page) error {
		title, text, err := extract.Extract(p.URL, p.Body)
		if err != nil {
			fmt.Printf("not saved: %s: %s\n", p.URL, err)
			return nil
		}

		if text == "" {
			fmt.Printf("not saved: %s: no text found\n", p.URL)
			return nil
		}

		saved++
		return st.SavePage(store.Page{URL: p.URL, Title: title, Text: text, FetchedAt: time.Now()})
	})

	if err != nil {
		return err
	}

	pagesTotal, err := st.CountPages()
	if err != nil {
		return err
	}

	fmt.Printf("saved %d pages to %s (%d in total)\n", saved, *dbPath, pagesTotal)

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
	answer, err := client.Chat(*model, messages)
	if err != nil {
		return fmt.Errorf("asking %s: %w", *model, err)
	}

	fmt.Println(strings.TrimSpace(answer))
	fmt.Println()
	fmt.Println("Sources:")
	for i, h := range hits {
		fmt.Printf("  [%d] %.2f  %s\n", i+1, h.Score, h.Chunk.URL)
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
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
	}
}
