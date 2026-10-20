package rag

import (
	"math"
	"slices"
	"strings"
	"testing"

	"butler/internal/store"
)

func TestCosine(t *testing.T) {
	tests := []struct {
		name string
		a, b []float32
		want float64
	}{
		{"same", []float32{1, 2, 3}, []float32{1, 2, 3}, 1},
		{"same direction, longer", []float32{1, 1}, []float32{5, 5}, 1},
		{"45 degrees", []float32{1, 0}, []float32{1, 1}, 1 / math.Sqrt(2)},
		{"unrelated", []float32{1, 0}, []float32{0, 1}, 0},
		{"opposite", []float32{1, 2}, []float32{-1, -2}, -1},
		{"zero vector", []float32{0, 0}, []float32{1, 1}, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Cosine(tc.a, tc.b)
			// NaN compares false with everything, so math.Abs(NaN-want) > 1e-9
			// would be false too. Check for it on its own.
			if math.IsNaN(got) {
				t.Fatalf("Cosine(%v, %v) = NaN, want %v", tc.a, tc.b, tc.want)
			}
			if math.Abs(got-tc.want) > 1e-9 {
				t.Errorf("Cosine(%v, %v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}

// testChunk makes a chunk whose URL and text are both named after text.
func testChunk(text string, emb ...float32) store.IndexedChunk {
	return store.IndexedChunk{URL: "https://example.com/" + text, Text: text, Embedding: emb}
}

// texts returns the text of each hit, in order.
func texts(hits []Hit) []string {
	var ts []string
	for _, h := range hits {
		ts = append(ts, h.Chunk.Text)
	}
	return ts
}

func TestTopK(t *testing.T) {
	chunks := []store.IndexedChunk{
		testChunk("north", 0, 1),
		testChunk("east", 1, 0),
		testChunk("west", -1, 0),
		testChunk("northeast", 1, 1),
	}
	query := []float32{1, 0.1} // mostly east, a little north

	tests := []struct {
		name string
		k    int
		want []string
	}{
		{"top 2", 2, []string{"east", "northeast"}},
		{"top 1", 1, []string{"east"}},
		{"k equals len", 4, []string{"east", "northeast", "north", "west"}},
		{"k bigger than len", 10, []string{"east", "northeast", "north", "west"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hits, err := TopK(query, chunks, tc.k)
			if err != nil {
				t.Fatalf("TopK: %v", err)
			}
			if got := texts(hits); !slices.Equal(got, tc.want) {
				t.Errorf("TopK(k=%d) = %q, want %q", tc.k, got, tc.want)
			}
			for i, h := range hits {
				if h.Score < -1 || h.Score > 1 {
					t.Errorf("hit %d (%s) score = %v, want between -1 and 1", i, h.Chunk.Text, h.Score)
				}
				if i > 0 && h.Score > hits[i-1].Score {
					t.Errorf("hit %d (%s) scores %v, higher than the hit before it (%v)",
						i, h.Chunk.Text, h.Score, hits[i-1].Score)
				}
			}
		})
	}

	t.Run("no chunks", func(t *testing.T) {
		hits, err := TopK(query, nil, 5)
		if err != nil {
			t.Fatalf("TopK: %v", err)
		}
		if len(hits) != 0 {
			t.Errorf("TopK with no chunks = %q, want none", texts(hits))
		}
	})

	t.Run("different length", func(t *testing.T) {
		odd := append(slices.Clone(chunks), testChunk("odd", 1, 2, 3))
		if _, err := TopK(query, odd, 2); err == nil {
			t.Error("TopK with a 3-number embedding and a 2-number query: got no error, want one")
		}
	})
}

func TestBuildPrompt(t *testing.T) {
	hits := []Hit{
		{Chunk: store.IndexedChunk{URL: "https://example.com/a", Title: "Title A", Text: "alpha text"}, Score: 0.9},
		{Chunk: store.IndexedChunk{URL: "https://example.com/b", Title: "Title B", Text: "beta text"}, Score: 0.8},
	}
	prompt := BuildPrompt("what is alpha?", hits)

	// Each source is numbered from 1, with its title and URL, then its text.
	for _, want := range []string{
		"[1] Title A (https://example.com/a)\nalpha text",
		"[2] Title B (https://example.com/b)\nbeta text",
		"Question: what is alpha?",
	} {
		if !strings.Contains(prompt, want) {
			t.Errorf("prompt is missing %q. Prompt:\n%s", want, prompt)
		}
	}

	// The question comes after every source.
	if q, last := strings.Index(prompt, "Question:"), strings.Index(prompt, "beta text"); q < last {
		t.Errorf("the question comes before the last source. Prompt:\n%s", prompt)
	}

	// There's no [0] and no [3]: numbering starts at 1 and stops at len(hits).
	for _, bad := range []string{"[0]", "[3]"} {
		if strings.Contains(prompt, bad) {
			t.Errorf("prompt contains %q. Prompt:\n%s", bad, prompt)
		}
	}
}

func TestBuildPromptNoHits(t *testing.T) {
	prompt := BuildPrompt("anything?", nil)
	if !strings.Contains(prompt, "Question: anything?") {
		t.Errorf("prompt with no hits is missing the question. Prompt:\n%s", prompt)
	}
	if strings.Contains(prompt, "[1]") {
		t.Errorf("prompt with no hits has a source. Prompt:\n%s", prompt)
	}
}
