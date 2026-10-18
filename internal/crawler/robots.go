package crawler

import (
	"fmt"
	"net/http"
	"net/url"

	"github.com/temoto/robotstxt"
)

// downloads site's /robots.txt and returns the rules for Butler.
func fetchRobots(site *url.URL) (*robotstxt.Group, error) {
	robotsURL := site.Scheme + "://" + site.Host + "/robots.txt"

	req, err := http.NewRequest(http.MethodGet, robotsURL, nil)
	if err != nil {
		return nil, err
	}

	req.Header.Set("User-Agent", userAgent)

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", robotsURL, err)
	}

	defer resp.Body.Close()

	data, err := robotstxt.FromResponse(resp)
	if err != nil {
		return nil, fmt.Errorf("parsing %s: %w", robotsURL, err)
	}

	return data.FindGroup(userAgent), nil
}
