package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

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
		fmt.Println("index: not implemented yet")
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
