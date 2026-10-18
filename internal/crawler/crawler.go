package crawler

import (
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
}

type queued struct {
	url   string
	depth int
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

func Crawl(start string, opts Options) ([]string, error) {
	site, err := url.Parse(start)
	if err != nil || (site.Scheme != "http" && site.Scheme != "https") || site.Host == "" {
		return nil, fmt.Errorf("invalid URL %q: must start with http:// or https://", start)
	}

	robots, err := fetchRobots(site)
	if err != nil {
		return nil, err
	}

	if !robots.Test(site.RequestURI()) {
		return nil, fmt.Errorf("robots.txt disallows %s", start)
	}

	var crawled []string
	queue := []queued{{url: start, depth: 0}}
	seen := map[string]bool{start: true}
	fetched, failed := 0, 0
	delay := max(robots.CrawlDelay, opts.Delay)

	for len(queue) > 0 && fetched < opts.MaxPages {
		item := queue[0]
		queue = queue[1:]

		if fetched > 0 {
			time.Sleep(delay)
		}
		fetched++

		page, err := fetch(item.url)
		if err != nil {
			if item.depth == 0 {
				return nil, err
			}
			fmt.Printf("  error: %v\n", err)
			failed++
			continue
		}

		seen[page.URL] = true

		base, err := url.Parse(page.URL)
		if err != nil {
			return nil, err
		}
		if base.Host != site.Host {
			fmt.Printf("skip: %s redirected off-site to %s\n", item.url, page.URL)
			continue
		}

		crawled = append(crawled, page.URL)

		if !strings.HasPrefix(page.ContentType, "text/html") {
			fmt.Printf("[%d/%d] depth %d  %s  (not HTML)\n", fetched, opts.MaxPages, item.depth, page.URL)
			continue
		}

		newLinks, err := extractLinks(base, page.Body)
		if err != nil {
			return nil, fmt.Errorf("extracting links from %s: %w", page.URL, err)
		}

		fmt.Printf("[%d/%d] depth %d  %s  (%d links)\n", fetched, opts.MaxPages, item.depth, page.URL, len(newLinks))

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

	fmt.Printf("done: %d fetched, %d errors, %d still queued\n", fetched, failed, len(queue))
	return crawled, nil
}
