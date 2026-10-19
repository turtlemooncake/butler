package chunk

import (
	"fmt"
	"slices"
	"strings"
	"testing"
)

// words returns "w0 w1 ... w(n-1)".
func words(n int) string {
	var ws []string
	for i := range n {
		ws = append(ws, fmt.Sprintf("w%d", i))
	}
	return strings.Join(ws, " ")
}

func TestSplit(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		size, overlap int
		want          []string
	}{
		{"empty", "", 5, 2, nil},
		{"whitespace only", "  \n\t  ", 5, 2, nil},
		{"messy whitespace", "a\n\nb   c\t d", 5, 2, []string{"a b c d"}},
		{"fewer words than size", words(3), 5, 2, []string{"w0 w1 w2"}},
		{"exactly size", words(5), 5, 2, []string{"w0 w1 w2 w3 w4"}},
		{"one word over", words(6), 5, 2, []string{"w0 w1 w2 w3 w4", "w3 w4 w5"}},
		{"ends on last word", words(8), 5, 2, []string{"w0 w1 w2 w3 w4", "w3 w4 w5 w6 w7"}},
		{"short last chunk", words(9), 5, 2, []string{"w0 w1 w2 w3 w4", "w3 w4 w5 w6 w7", "w6 w7 w8"}},
		{"no overlap", words(5), 2, 0, []string{"w0 w1", "w2 w3", "w4"}},
		{"largest overlap", words(4), 3, 2, []string{"w0 w1 w2", "w1 w2 w3"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Split(tc.text, tc.size, tc.overlap)
			if err != nil {
				t.Fatalf("Split: %v", err)
			}
			if !slices.Equal(got, tc.want) {
				t.Errorf("Split(%q, %d, %d)\n got %q\nwant %q",
					tc.text, tc.size, tc.overlap, got, tc.want)
			}
		})
	}
}

func TestSplitBadArgs(t *testing.T) {
	tests := []struct {
		size, overlap int
	}{
		{0, 0},
		{-1, 0},
		{5, -1},
		{5, 5},
		{5, 6},
	}
	for _, tc := range tests {
		if _, err := Split(words(10), tc.size, tc.overlap); err == nil {
			t.Errorf("Split(size %d, overlap %d) error = nil, want an error", tc.size, tc.overlap)
		}
	}
}
