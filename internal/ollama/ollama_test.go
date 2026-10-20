package ollama

import (
	"encoding/json"
	"io"
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

func TestChat(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/api/chat" {
			t.Errorf("request = %s %s, want POST /api/chat", r.Method, r.URL.Path)
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Fatalf("reading request: %v", err)
		}

		var req chatRequest
		if err := json.Unmarshal(body, &req); err != nil {
			t.Errorf("decoding request: %v", err)
		}
		if req.Model != "test-model" {
			t.Errorf("model = %q, want %q", req.Model, "test-model")
		}
		if len(req.Messages) != 2 {
			t.Fatalf("got %d messages, want 2: %+v", len(req.Messages), req.Messages)
		}
		if req.Messages[0] != (Message{Role: "system", Content: "be brief"}) {
			t.Errorf("message 0 = %+v, want the system message", req.Messages[0])
		}
		if req.Messages[1] != (Message{Role: "user", Content: "hi?"}) {
			t.Errorf("message 1 = %+v, want the user message", req.Messages[1])
		}

		// req.Stream is false whether the client sent false or nothing at all.
		// The map tells the two apart: a missing key gives nil, and nil != false.
		var raw map[string]any
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Errorf("decoding request as a map: %v", err)
		}
		for _, key := range []string{"stream", "think"} {
			if raw[key] != false {
				t.Errorf("%q = %v, want false", key, raw[key])
			}
		}

		w.Write([]byte(`{"model":"test-model","message":{"role":"assistant","content":"hi [1]"},"done":true}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	got, err := c.Chat("test-model", []Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hi?"},
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got != "hi [1]" {
		t.Errorf("Chat = %q, want %q", got, "hi [1]")
	}
}

func TestChatError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		w.Write([]byte(`{"error":"model 'nope' not found"}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	_, err := c.Chat("nope", []Message{{Role: "user", Content: "hi"}})
	if err == nil {
		t.Fatal("Chat: got no error, want one")
	}
	if !strings.Contains(err.Error(), "not found") {
		t.Errorf("error = %q, want it to contain %q", err, "not found")
	}
}

func TestChatEmpty(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"model":"test-model","message":{"role":"assistant","content":""},"done":true}`))
	}))
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	if _, err := c.Chat("test-model", []Message{{Role: "user", Content: "hi"}}); err == nil {
		t.Error("Chat with an empty reply: got no error, want one")
	}
}
