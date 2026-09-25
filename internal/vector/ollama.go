package vector

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"

	ollama "github.com/ollama/ollama/api"
)

// OllamaEmbedder generates embeddings from a local Ollama instance.
type OllamaEmbedder struct {
	baseURL   string
	client    *ollama.Client
	model     string
	keepAlive *ollama.Duration
}

const defaultOllamaBase = "http://127.0.0.1:11434"

// NewOllamaEmbedder creates an embedder targeting the given Ollama base URL
// and embedding model. An empty base URL falls back to 127.0.0.1:11434 to
// avoid localhost DNS/IPv6 resolution issues in sandboxed environments.
func NewOllamaEmbedder(baseURL, model string) (*OllamaEmbedder, error) {
	trimmed := strings.TrimSpace(baseURL)
	if trimmed == "" {
		trimmed = defaultOllamaBase
	}
	u, err := url.Parse(trimmed)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" {
		u.Scheme = "http"
	}
	client := ollama.NewClient(u, http.DefaultClient)
	return &OllamaEmbedder{
		baseURL: trimmed,
		client:  client,
		model:   strings.TrimSpace(model),
	}, nil
}

// EmbedText returns a single embedding vector for the provided text.
func (e *OllamaEmbedder) EmbedText(ctx context.Context, text string) ([]float32, error) {
	if e == nil || e.client == nil {
		return nil, ErrNotConfigured
	}
	req := &ollama.EmbedRequest{
		Model: e.model,
		Input: text,
	}
	if e.keepAlive != nil {
		req.KeepAlive = e.keepAlive
	}
	resp, err := e.client.Embed(ctx, req)
	if err != nil {
		// Fallback: if the configured endpoint contains "localhost" and the
		// request failed at the network/DNS layer, retry once against
		// 127.0.0.1 to bypass localhost resolution/IPv6 mismatches.
		if isNetworkError(err) && strings.Contains(e.baseURL, "localhost") {
			fallback, _ := NewOllamaEmbedder(strings.Replace(e.baseURL, "localhost", "127.0.0.1", 1), e.model)
			if fallback != nil {
				resp, err = fallback.client.Embed(ctx, req)
				if err == nil && len(resp.Embeddings) > 0 {
					return resp.Embeddings[0], nil
				}
			}
		}
		return nil, err
	}
	if len(resp.Embeddings) == 0 {
		return nil, ErrEmptyEmbedding
	}
	return resp.Embeddings[0], nil
}

func isNetworkError(err error) bool {
	if err == nil {
		return false
	}
	var dnsErr *net.DNSError
	var opErr *net.OpError
	return errors.Is(err, context.DeadlineExceeded) ||
		errors.As(err, &dnsErr) ||
		errors.As(err, &opErr) ||
		strings.Contains(err.Error(), "no such host") ||
		strings.Contains(err.Error(), "dial tcp")
}
