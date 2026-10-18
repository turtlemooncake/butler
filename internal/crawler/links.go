package crawler

import (
	"bytes"
	"errors"
	"io"
	"net/url"
	"strings"

	"golang.org/x/net/html"
)

func extractLinks(base *url.URL, body []byte) ([]string, error) {
	var links []string
	seen := map[string]bool{}

	z := html.NewTokenizer(bytes.NewReader(body))
	for {
		tt := z.Next()

		switch tt {
		case html.ErrorToken:
			if errors.Is(z.Err(), io.EOF) { // end of file
				return links, nil
			}
			return nil, z.Err()
		case html.StartTagToken, html.SelfClosingTagToken:
			t := z.Token()
			if t.Data != "a" {
				continue
			}

			for _, attr := range t.Attr {
				if attr.Key != "href" { // key, val -- href, /about
					continue
				}

				href := strings.TrimSpace(attr.Val)

				if href == "" {
					continue
				}

				u, err := base.Parse(href)
				if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
					continue
				}
				u.Fragment = "" // force remove sub fragments within a link i.e. /faq#q1 /faq#q2

				link := u.String()

				if !seen[link] {
					seen[link] = true
					links = append(links, link)
				}
			}
		}
	}
}
