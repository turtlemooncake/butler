package main

import (
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"butler/internal/crawler"
)

func usage() {
	fmt.Fprintln(os.Stderr, "usage: butler <crawl|index|ask> [args]")
	os.Exit(1)
	return
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}

	cmd := os.Args[1]
	args := os.Args[2:]

	switch cmd {
	case "crawl":
		var opts crawler.Options
		fs := flag.NewFlagSet("crawl", flag.ExitOnError)
		fs.IntVar(&opts.MaxPages, "max-pages", 50, "stop after fetching this many pages")
		fs.IntVar(&opts.MaxDepth, "max-depth", 3, "how many links away from the start URL to go")
		fs.DurationVar(&opts.Delay, "delay", time.Second, "wait between requests")
		fs.Parse(args)

		if fs.NArg() != 1 {
			fmt.Fprintln(os.Stderr, "usage: butler crawl [flags] <url>")
			fs.PrintDefaults()
			os.Exit(1)
		}

		if _, err := crawler.Crawl(fs.Arg(0), opts); err != nil {
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
