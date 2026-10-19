// Package extract turns a web page's HTML into its title and main text.
package extract

import (
	"bytes"
	"fmt"
	"net/url"
	"strings"

	"github.com/go-shiori/go-readability"
)

func cleanText(s string) string {
	splitted := strings.Split(s, "\n")
	var lines []string
	for _, line := range splitted {
		line = strings.TrimSpace(line)
		if line != "" {
			lines = append(lines, line)
		}
	}

	return strings.Join(lines, "\n")
}

// Title, cleaned text, error
func Extract(pageURL string, body []byte) (title string, text string, err error) {
	u, err := url.Parse(pageURL)
	if err != nil {
		return "", "", err
	}

	article, err := readability.FromReader(bytes.NewReader(body), u)
	if err != nil {
		return "", "", fmt.Errorf("readability: %w", err)
	}

	return strings.TrimSpace(article.Title), cleanText(article.TextContent), nil
}
