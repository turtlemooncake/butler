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

// chatServer starts a fake Ollama that answers every request with status and
// reply, and stores the request body it got in *body.
func chatServer(t *testing.T, status int, reply string, body *[]byte) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/chat" {
			t.Errorf("path = %s, want /api/chat", r.URL.Path)
		}
		b, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("reading request: %v", err)
		}
		*body = b
		w.WriteHeader(status)
		w.Write([]byte(reply))
	}))
}

func TestChat(t *testing.T) {
	// A streamed reply: one JSON object per line, the last with "done": true.
	// The line after it must be ignored.
	var body []byte
	srv := chatServer(t, http.StatusOK, `{"message":{"role":"assistant","content":"hi"},"done":false}
{"message":{"role":"assistant","content":" [1]"},"done":false}
{"message":{"role":"assistant","content":""},"done":true}
{"message":{"role":"assistant","content":"after done"},"done":false}
`, &body)
	defer srv.Close()

	c := &Client{BaseURL: srv.URL}
	var pieces []string
	err := c.Chat("test-model", []Message{
		{Role: "system", Content: "be brief"},
		{Role: "user", Content: "hi?"},
	}, func(s string) {
		pieces = append(pieces, s)
	})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if got := strings.Join(pieces, ""); got != "hi [1]" {
		t.Errorf("reply = %q, want %q (pieces %q)", got, "hi [1]", pieces)
	}

	var req chatRequest
	if err := json.Unmarshal(body, &req); err != nil {
		t.Fatalf("decoding request: %v", err)
	}
	if req.Model != "test-model" || len(req.Messages) != 2 || req.Messages[0].Role != "system" {
		t.Errorf("request = %+v", req)
	}

	// A map tells "sent false" apart from "not sent at all".
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatalf("decoding request as a map: %v", err)
	}
	if raw["stream"] != true {
		t.Errorf(`"stream" = %v, want true`, raw["stream"])
	}
	if raw["think"] != false {
		t.Errorf(`"think" = %v, want false`, raw["think"])
	}
}

func TestChatErrors(t *testing.T) {
	tests := []struct {
		name   string
		status int
		reply  string
		want   string // the error must contain this
	}{
		{"missing model", http.StatusNotFound, `{"error":"model 'nope' not found"}`, "not found"},
		{"error mid-stream", http.StatusOK, `{"message":{"content":"hi"},"done":false}` + "\n" + `{"error":"out of memory"}`, "out of memory"},
		{"empty reply", http.StatusOK, `{"message":{"content":""},"done":true}`, "empty reply"},
		{"cut off", http.StatusOK, `{"message":{"content":"hi"},"done":false}`, "EOF"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var body []byte
			srv := chatServer(t, tc.status, tc.reply, &body)
			defer srv.Close()

			c := &Client{BaseURL: srv.URL}
			err := c.Chat("test-model", []Message{{Role: "user", Content: "hi"}}, func(string) {})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Chat error = %v, want one containing %q", err, tc.want)
			}
		})
	}
}
