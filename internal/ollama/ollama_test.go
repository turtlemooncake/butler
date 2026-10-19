package ollama

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
)

func TestEmbed(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/embed" {
			t.Errorf("request = %s %s, want POST /api/embed", r.Method, r.URL.Path)
		}
		var req embedRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if req.Model != "test-model" || !slices.Equal(req.Input, []string{"a", "b"}) {
			t.Errorf("request body = %+v", req)
		}
		w.Write([]byte(`{"model":"test-model","embeddings":[[1,2],[3,4.5]],"total_duration":12345}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	got, err := c.Embed("test-model", []string{"a", "b"})
	if err != nil {
		t.Fatalf("Embed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("got %d embeddings, want 2", len(got))
	}
	if !slices.Equal(got[0], []float32{1, 2}) || !slices.Equal(got[1], []float32{3, 4.5}) {
		t.Errorf("Embed = %v, want [[1 2] [3 4.5]]", got)
	}
}

func TestEmbedError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"model \"nope\" not found, try pulling it first"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	_, err := c.Embed("nope", []string{"a"})
	if err == nil {
		t.Fatal("Embed: got no error, want one")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain %q", err, "not found")
	}
}

func TestEmbedCountMismatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"embeddings":[[1,2]]}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if _, err := c.Embed("test-model", []string{"a", "b"}); err == nil {
		t.Error("Embed with 2 inputs and 1 embedding: got no error, want one")
	}
}
