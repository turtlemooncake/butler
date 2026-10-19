package chunk

import (
	"fmt"
	"strings"
)

// Split breaks text into chunks of size words. Each chunk after the first
// starts with the last overlap words of the chunk before it. Whitespace
// inside a chunk is collapsed to single spaces. Text with no words gives no
// chunks.
func Split(text string, size, overlap int) ([]string, error) {
	if size < 1 {
		return nil, fmt.Errorf("size must be at least 1, got %d", size)
	}

	if overlap < 0 {
		return nil, fmt.Errorf("overlap must not be negative, got %d", overlap)
	}

	if overlap >= size {
		return nil, fmt.Errorf("overlap (%d) must be smaller than size (%d)", overlap, size)
	}

	words := strings.Fields(text)
	if len(words) == 0 {
		return nil, nil
	}

	step := size - overlap
	var chunks []string
	for start := 0; ; start += step {
		end := min(start+size, len(words))
		chunks = append(chunks, strings.Join(words[start:end], " "))

		if end == len(words) {
			break
		}
	}
	return chunks, nil

}
