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

	req.Header.Set("User-Agent", "butler/0.1")

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

func Crawl(start string) error {
	page, err := fetch(start)
	if err != nil {
		return err
	}

	fmt.Printf("GET %s\n", page.URL)
	fmt.Printf("status:       %s\n", page.Status)
	fmt.Printf("content-type: %s\n", page.ContentType)
	fmt.Printf("size:         %d bytes\n", len(page.Body))

	if !strings.HasPrefix(page.ContentType, "text/html") {
		fmt.Println("Not HTML, no links")
		return nil
	}

	base, err := url.Parse(page.URL)
	if err != nil {
		return err
	}

	newLinks, err := extractLinks(base, page.Body)
	if err != nil {
		return fmt.Errorf("extracting links from %s: %w", page.URL, err)
	}

	fmt.Printf("links:        %d\n", len(newLinks))
	for _, link := range newLinks {
		fmt.Printf("  %s\n", link)
	}

	return nil
}
