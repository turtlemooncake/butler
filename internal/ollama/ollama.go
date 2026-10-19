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

// Embed returns one embedding per input, in the same order, using model.
func (c *Client) Embed(model string, inputs []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: model, Input: inputs})
	if err != nil {
		return nil, err
	}

	resp, err := http.Post(c.BaseURL+"/api/embed", "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("check if Ollama is running %w", err)
	}

	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, apiError(resp)
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding embed response: %w", err)
	}

	if len(out.Embeddings) != len(inputs) {
		return nil, fmt.Errorf("asked for %d embeddings, got %d", len(inputs), len(out.Embeddings))
	}

	return out.Embeddings, nil
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
