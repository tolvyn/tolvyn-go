// Package openai provides a TOLVYN-wrapped OpenAI client.
// It is a drop-in replacement for github.com/openai/openai-go.
package openai

import (
	"net/http"
	"os"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	tolvyn "github.com/tolvyn/tolvyn-go"
)

const defaultProxyURL = "https://proxy.tolvyn.io/v1/proxy/openai/"
const providerDirectURL = "https://api.openai.com/"

// Client is a TOLVYN-wrapped OpenAI client.
// Use it exactly as you would use openai.Client.
type Client struct {
	*oai.Client
}

// NewClient creates a new TOLVYN OpenAI client.
// If TolvynAPIKey is empty, falls back to TOLVYN_API_KEY env var.
// If ProviderAPIKey is empty, falls back to OPENAI_API_KEY env var.
// NOTE: FailOpen must be set explicitly to true to enable fail-open behavior.
func NewClient(opts tolvyn.ClientOptions) *Client {
	opts.Defaults("OPENAI_API_KEY", defaultProxyURL)

	headers := opts.TolvynHeaders()

	var httpClient *http.Client
	if opts.FailOpen && opts.ProviderAPIKey != "" {
		httpClient = &http.Client{
			Transport: tolvyn.NewFailOpenTransport(
				opts.ProxyURL,
				providerDirectURL,
				opts.ProviderAPIKey,
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
// Optional: OPENAI_API_KEY (for fail-open), TOLVYN_PROXY_URL
// FailOpen is enabled by default when using this constructor.
func NewClientFromEnv() *Client {
	return NewClient(tolvyn.ClientOptions{
		TolvynAPIKey:   os.Getenv("TOLVYN_API_KEY"),
		ProviderAPIKey: os.Getenv("OPENAI_API_KEY"),
		FailOpen:       true,
	})
}
