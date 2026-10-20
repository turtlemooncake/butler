package crawler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

var client = &http.Client{
	Timeout: 15 * time.Second,
}

const maxBodyBytes = 5 << 20 // 5 MiB

type Page struct {
	URL         string
	Status      string
	ContentType string
	Body        []byte
}

type Options struct {
	MaxPages int
	MaxDepth int
	Delay    time.Duration
	Workers  int
}

type queued struct {
	url   string
	depth int
}

type result struct {
	page Page
	err  error
}

const userAgent = "butler/0.1"

// fetch downloads one URL and returns it as a Page.
func fetch(rawURL string) (Page, error) {
	u, err := url.Parse(rawURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return Page{}, fmt.Errorf("invalid URL %q: must start with http:// or https://", rawURL)
	}

	req, err := http.NewRequest(http.MethodGet, rawURL, nil)
	if err != nil {
		return Page{}, err
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return Page{}, fmt.Errorf("GET %s: %w", rawURL, err)
	}

	// need to manually close connection
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return Page{}, fmt.Errorf("GET %s: %s", rawURL, resp.Status)
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBodyBytes))
	if err != nil {
		return Page{}, fmt.Errorf("GET %s: reading body: %w", rawURL, err)
	}

	result := Page{
		URL:         resp.Request.URL.String(),
		Status:      resp.Status,
		ContentType: resp.Header.Get("Content-Type"),
		Body:        body,
	}

	return result, nil
}

func Crawl(start string, opts Options, handle func(Page) error) error {
	site, err := url.Parse(start)
	if err != nil || (site.Scheme != "http" && site.Scheme != "https") || site.Host == "" {
		return fmt.Errorf("invalid URL %q: must start with http:// or https://", start)
	}

	robots, err := fetchRobots(site)
	if err != nil {
		return err
	}

	if !robots.Test(site.RequestURI()) {
		return fmt.Errorf("robots.txt disallows %s", start)
	}

	queue := []queued{{url: start, depth: 0}}
	seen := map[string]bool{start: true}
	fetched, failed := 0, 0
	delay := max(robots.CrawlDelay, opts.Delay)
	workers := max(opts.Workers, 1)

	for len(queue) > 0 && fetched < opts.MaxPages {
		n := min(workers, len(queue), opts.MaxPages-fetched)
		batch := queue[:n]
		queue = queue[n:]

		results := make([]result, n)
		var wg sync.WaitGroup
		for i, item := range batch {
			if fetched > 0 {
				time.Sleep(delay)
			}
			fetched++
			wg.Go(func() {
				results[i].page, results[i].err = fetch(item.url)
			})
		}

		wg.Wait()

		for i, item := range batch {
			page, err := results[i].page, results[i].err
			if err != nil {
				if item.depth == 0 {
					return err
				}
				fmt.Printf("  error: %v\n", err)
				failed++
				continue
			}

			seen[page.URL] = true

			base, err := url.Parse(page.URL)
			if err != nil {
				return err
			}
			if base.Host != site.Host {
				fmt.Printf("skip: %s redirected off-site to %s\n", item.url, page.URL)
				continue
			}

			if !strings.HasPrefix(page.ContentType, "text/html") {
				fmt.Printf("[%d/%d] depth %d  %s  (not HTML)\n", fetched, opts.MaxPages, item.depth, page.URL)
				continue
			}

			newLinks, err := extractLinks(base, page.Body)
			if err != nil {
				return fmt.Errorf("extracting links from %s: %w", page.URL, err)
			}

			fmt.Printf("[%d/%d] depth %d  %s  (%d links)\n", fetched, opts.MaxPages, item.depth, page.URL, len(newLinks))

			if err := handle(page); err != nil {
				return err
			}

			if item.depth >= opts.MaxDepth {
				continue
			}

			for _, link := range newLinks {
				if seen[link] {
					continue
				}

				newlink, err := url.Parse(link)
				if err != nil || newlink.Host != site.Host {
					continue
				}

				if !robots.Test(newlink.RequestURI()) {
					continue
				}

				seen[link] = true
				queue = append(queue, queued{url: link, depth: item.depth + 1})
			}
		}
	}

	fmt.Printf("done: %d fetched, %d errors, %d still queued\n", fetched, failed, len(queue))
	return nil
}
