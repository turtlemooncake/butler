package rag

import (
	"butler/internal/store"
	"cmp"
	"fmt"
	"math"
	"slices"
	"strings"
)

type Hit struct {
	Chunk store.IndexedChunk
	Score float64
}

// Cosine returns the cosine similarity of a and b: 1 when they point the same
// way, 0 when they're unrelated, -1 when they're opposite. It returns 0 if
// either vector is all zeros. a and b must be the same length.
func Cosine(a, b []float32) float64 {
	var sums float64
	var normA float64
	var normB float64

	for i, _ := range a {
		sums += float64(a[i]) * float64(b[i])
		normA += math.Pow(float64(a[i]), 2)
		normB += math.Pow(float64(b[i]), 2)
	}

	if normA == 0 || normB == 0 {
		return 0
	}

	return sums / (math.Sqrt(normA) * math.Sqrt(normB))
}

func TopK(query []float32, chunks []store.IndexedChunk, k int) ([]Hit, error) {
	var hits []Hit
	for _, c := range chunks {
		if len(c.Embedding) != len(query) {
			return nil, fmt.Errorf("chunk %d of %s has %d. query has %d numbers. indexed by different model?", c.Seq, c.URL, len(c.Embedding), len(query))
		}

		cosScore := Cosine(query, c.Embedding)

		hits = append(hits, Hit{Chunk: c, Score: cosScore})
	}

	slices.SortFunc(hits, func(a, b Hit) int { return cmp.Compare(b.Score, a.Score) })

	return hits[:min(k, len(hits))], nil
}

// SystemPrompt tells the chat model how to answer from the sources.
const SystemPrompt = `You answer questions using only the numbered sources you are given.
Cite the sources you use, like [1] or [2][3], right after the facts that come from them.
If the sources don't contain the answer, say that you don't know. Don't use anything else you know.
Keep the answer short.`

func BuildPrompt(question string, hits []Hit) string {
	var b strings.Builder
	b.WriteString("Sources:\n\n")

	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] %s (%s)\n%s\n\n", i+1, h.Chunk.Title, h.Chunk.URL, h.Chunk.Text)
	}

	fmt.Fprintf(&b, "Question: %s", question)

	return b.String()
}
