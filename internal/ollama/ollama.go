package ollama

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

const DefaultURL = "http://localhost:11434"

// Client talks to an Ollama server.
type Client struct {
	BaseURL string
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
}

// Message is one turn of a chat.
type Message struct {
	Role    string `json:"role"` // "system", "user" or "assistant"
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string    `json:"model"`
	Messages []Message `json:"messages"`
	Stream   bool      `json:"stream"`
	Think    bool      `json:"think"`
}

type chatResponse struct {
	Message Message `json:"message"`
}

func (c *Client) post(path string, in, out any) error {
	body, err := json.Marshal(in)
	if err != nil {
		return err
	}

	resp, err := http.Post(c.BaseURL+path, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("check if Ollama is running %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return apiError(resp)
	}

	if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
		return fmt.Errorf("decoding embed response: %w", err)
	}

	return nil
}

// Embed returns one embedding per input, in the same order, using model.
func (c *Client) Embed(model string, inputs []string) ([][]float32, error) {
	var out embedResponse
	if err := c.post("/api/embed", embedRequest{Model: model, Input: inputs}, &out); err != nil {
		return nil, err
	}

	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("asked for %d embeddings, got %d", len(inputs), len(out.Embeddings))
	}

	return out.Embeddings, nil
}

// Send chat messsage to model and return its reply
func (c *Client) Chat(model string, messages []Message) (string, error) {
	var out chatResponse
	if err := c.post("/api/chat", chatRequest{Model: model, Messages: messages, Stream: false, Think: false}, &out); err != nil {
		return "", err
	}

	if out.Message.Content == "" {
		return "", fmt.Errorf("ollama: empty reply from %s", model)
	}

	return out.Message.Content, nil
}

// apiError builds an error from a failed Ollama response.
func apiError(resp *http.Response) error {
	var e struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&e); err == nil && e.Error != "" {
		return fmt.Errorf("ollama: %s", e.Error)
	}
	return fmt.Errorf("ollama: %s", resp.Status)
}
