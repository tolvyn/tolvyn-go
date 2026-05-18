// Package google provides a TOLVYN-wrapped Google Generative AI client.
// It wraps github.com/google/generative-ai-go/genai.
package google

import (
	"context"
	"net/http"

	"github.com/google/generative-ai-go/genai"
	tolvyn "github.com/tolvyn/tolvyn-go"
	"google.golang.org/api/option"
)

const defaultProxyURL = "https://proxy.tolvyn.io/v1/proxy/google/"
const providerDirectURL = "https://generativelanguage.googleapis.com/"

// Client wraps the Google GenAI client with TOLVYN attribution headers.
type Client struct {
	*genai.Client
	opts tolvyn.ClientOptions
}

// NewClient creates a TOLVYN-wrapped Google GenAI client.
// If TolvynAPIKey is empty, falls back to TOLVYN_API_KEY env var.
// If ProviderAPIKey is empty, falls back to GOOGLE_API_KEY env var.
func NewClient(ctx context.Context, opts tolvyn.ClientOptions) (*Client, error) {
	opts.Defaults("GOOGLE_API_KEY", defaultProxyURL)

	headers := opts.TolvynHeaders()

	// GO-02: wire fail-open transport for Google when ProviderAPIKey is set
	// and DisableFailOpen is false (matching OpenAI/Anthropic semantics).
	// The path-strip in tolvyn.NewFailOpenTransport handles /v1/proxy/google/
	// removal; remaining path (e.g. /v1beta/models/...) is valid for
	// generativelanguage.googleapis.com.
	var httpClient *http.Client
	if !opts.DisableFailOpen && opts.ProviderAPIKey != "" {
		httpClient = &http.Client{
			Transport: tolvyn.NewFailOpenTransport(
				opts.ProxyURL,
				providerDirectURL,
				opts.ProviderAPIKey,
				headers,
			),
		}
	} else {
		httpClient = &http.Client{
			Transport: &headerInjectTransport{
				base:    http.DefaultTransport,
				headers: headers,
			},
		}
	}

	client, err := genai.NewClient(ctx,
		option.WithAPIKey(opts.TolvynAPIKey),
		option.WithHTTPClient(httpClient),
		option.WithEndpoint(opts.ProxyURL),
	)
	if err != nil {
		return nil, err
	}

	return &Client{Client: client, opts: opts}, nil
}

// NewClientFromEnv creates a client using only environment variables.
// Required: TOLVYN_API_KEY
// Optional: GOOGLE_API_KEY (for fail-open), TOLVYN_PROXY_URL
func NewClientFromEnv(ctx context.Context) (*Client, error) {
	return NewClient(ctx, tolvyn.ClientOptions{})
}

// headerInjectTransport injects TOLVYN attribution headers on every request.
type headerInjectTransport struct {
	base    http.RoundTripper
	headers map[string]string
}

func (t *headerInjectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	r := req.Clone(req.Context())
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.base.RoundTrip(r)
}
