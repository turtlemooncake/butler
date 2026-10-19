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

	fs.Parse(args)

	if fs.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "usage: butler index [flags]")
		fs.PrintDefaults()
		os.Exit(1)
	}

	// Check the sizes before opening the database
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

	pages, err := st.ListPages()
	if err != nil {
		return err
	}
	if len(pages) == 0 {
		fmt.Printf("no pages in %s; run \"butler crawl\" first\n", *dbPath)
		return nil
	}

	total := 0
	for _, p := range pages {
		chunks, err := chunk.Split(p.Text, *size, *overlap)
		if err != nil {
			return err
		}
		total += len(chunks)
		fmt.Printf("%3d chunks  %s  (%d words)\n", len(chunks), p.URL, len(strings.Fields(p.Text)))
	}

	fmt.Printf("%d pages, %d chunks (size %d words, overlap %d)\n", len(pages), total, *size, *overlap)
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
		if len(args) == 0 {
			fmt.Fprintln(os.Stderr, "usage: butler ask <question>")
			os.Exit(1)
		}
		question := strings.Join(args, " ")
		fmt.Println("ask:", question)
	default:
		fmt.Fprintf(os.Stderr, "unknown command %q\n", cmd)
		usage()
	}
}
