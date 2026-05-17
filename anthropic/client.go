// Package anthropic provides a TOLVYN-wrapped Anthropic client.
// It is a drop-in replacement for github.com/anthropics/anthropic-sdk-go.
package anthropic

import (
	"net/http"
	"os"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	tolvyn "github.com/tolvyn/tolvyn-go"
)

const defaultProxyURL = "https://proxy.tolvyn.io/v1/proxy/anthropic/"
const providerDirectURL = "https://api.anthropic.com/"

// Client is a TOLVYN-wrapped Anthropic client.
// Use it exactly as you would use anthropic.Client.
type Client struct {
	*sdk.Client
}

// NewClient creates a new TOLVYN Anthropic client.
// If TolvynAPIKey is empty, falls back to TOLVYN_API_KEY env var.
// If ProviderAPIKey is empty, falls back to ANTHROPIC_API_KEY env var.
// NOTE: FailOpen must be set explicitly to true to enable fail-open behavior.
func NewClient(opts tolvyn.ClientOptions) *Client {
	opts.Defaults("ANTHROPIC_API_KEY", defaultProxyURL)

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

	anthropicOpts := []option.RequestOption{
		option.WithBaseURL(opts.ProxyURL),
		option.WithAPIKey(opts.TolvynAPIKey),
		option.WithHTTPClient(httpClient),
		// Required by Anthropic API.
		option.WithHeader("anthropic-version", "2023-06-01"),
	}

	// Inject attribution headers (Authorization is handled by WithAPIKey).
	for k, v := range headers {
		if k != "Authorization" {
			anthropicOpts = append(anthropicOpts, option.WithHeader(k, v))
		}
	}

	return &Client{
		Client: sdk.NewClient(anthropicOpts...),
	}
}

// NewClientFromEnv creates a client using only environment variables.
// Required: TOLVYN_API_KEY
// Optional: ANTHROPIC_API_KEY (for fail-open), TOLVYN_PROXY_URL
// FailOpen is enabled by default when using this constructor.
func NewClientFromEnv() *Client {
	return NewClient(tolvyn.ClientOptions{
		TolvynAPIKey:   os.Getenv("TOLVYN_API_KEY"),
		ProviderAPIKey: os.Getenv("ANTHROPIC_API_KEY"),
		FailOpen:       true,
	})
}
