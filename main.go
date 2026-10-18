package main

import (
	"fmt"
	"os"
	"strings"

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
		if len(args) != 1 {
			fmt.Fprintln(os.Stderr, "usage: butler crawl <url>")
			os.Exit(1)
		}
		if err := crawler.Crawl(args[0]); err != nil {
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
