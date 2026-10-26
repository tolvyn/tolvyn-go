// Package deepseek provides a TOLVYN-wrapped DeepSeek client.
// DeepSeek is OpenAI-compatible, so this wraps github.com/openai/openai-go.
package deepseek

import (
	"net/http"
	"os"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	tolvyn "github.com/tolvyn/tolvyn-go"
)

const defaultProxyURL = "https://proxy.tolvyn.io/v1/proxy/deepseek/"
const providerDirectURL = "https://api.deepseek.com/v1/"

// Client is a TOLVYN-wrapped DeepSeek client.
// Use it exactly as you would use openai.Client, with DeepSeek model names.
type Client struct {
	*oai.Client
}

// NewClient creates a new TOLVYN DeepSeek client.
// If TolvynAPIKey is empty, falls back to TOLVYN_API_KEY env var.
// If ProviderAPIKey is empty, falls back to DEEPSEEK_API_KEY env var.
// Fail-open is enabled by default when ProviderAPIKey is set; set DisableFailOpen: true to disable.
func NewClient(opts tolvyn.ClientOptions) *Client {
	opts.Defaults("DEEPSEEK_API_KEY", defaultProxyURL)

	headers := opts.TolvynHeaders()

	var httpClient *http.Client
	if !opts.DisableFailOpen && opts.ProviderAPIKey != "" {
		httpClient = &http.Client{
			Transport: tolvyn.NewFailOpenTransport(
				opts.ProxyURL,
				providerDirectURL,
				opts.ProviderAPIKey,
				"deepseek",
				headers,
			),
		}
	} else {
		httpClient = &http.Client{}
	}

	openaiOpts := []option.RequestOption{
		option.WithBaseURL(opts.ProxyURL),
		option.WithAPIKey(opts.TolvynAPIKey),
		option.WithHTTPClient(httpClient),
	}

	// Inject attribution headers (Authorization is handled by WithAPIKey).
	for k, v := range headers {
		if k != "Authorization" {
			openaiOpts = append(openaiOpts, option.WithHeader(k, v))
		}
	}

	return &Client{
		Client: oai.NewClient(openaiOpts...),
	}
}

// NewClientFromEnv creates a client using only environment variables.
// Required: TOLVYN_API_KEY
// Optional: DEEPSEEK_API_KEY (for fail-open), TOLVYN_PROXY_URL
// Fail-open is enabled by default when DEEPSEEK_API_KEY is set.
func NewClientFromEnv() *Client {
	return NewClient(tolvyn.ClientOptions{
		TolvynAPIKey:   os.Getenv("TOLVYN_API_KEY"),
		ProviderAPIKey: os.Getenv("DEEPSEEK_API_KEY"),
	})
}
