// Package tolvyn provides Go SDKs for OpenAI, Anthropic, and Google
// that route through the TOLVYN financial control plane.
//
// Usage:
//
//	import "github.com/tolvyn/tolvyn-go/openai"
//
//	client := openai.NewClient(openai.ClientOptions{
//	    TolvynAPIKey: "tlv_live_...",
//	    Team:         "engineering",
//	    Service:      "chatbot-api",
//	})
package tolvyn

import (
	"net/http"
	"os"
	"strings"
	"time"
)

const Version = "0.1.0"

// ClientOptions configures a TOLVYN SDK client.
// All fields are optional except TolvynAPIKey (or TOLVYN_API_KEY env var).
type ClientOptions struct {
	// TolvynAPIKey is your TOLVYN API key (tlv_live_... or tlv_test_...).
	// Falls back to TOLVYN_API_KEY environment variable.
	TolvynAPIKey string

	// ProxyURL overrides the default TOLVYN proxy URL.
	// Defaults to https://proxy.tolvyn.io/v1/proxy/{provider}/
	ProxyURL string

	// Attribution headers — injected on every request.
	Team        string // X-Tolvyn-Team
	Service     string // X-Tolvyn-Service
	Feature     string // X-Tolvyn-Feature
	Agent       string // X-Tolvyn-Agent
	User        string // X-Tolvyn-User
	EndCustomer string // X-Tolvyn-End-Customer

	// FailOpen enables automatic fallback to provider direct if TOLVYN is unreachable.
	// NOTE: zero value is false — you must set FailOpen: true to enable.
	// Use NewClientFromEnv() for a client with FailOpen pre-set to true.
	FailOpen bool

	// ProviderAPIKey is the provider's API key used for fail-open fallback.
	// Required for fail-open to work.
	// Falls back to OPENAI_API_KEY / ANTHROPIC_API_KEY / GOOGLE_API_KEY env vars.
	ProviderAPIKey string
}

// Defaults fills in zero values with environment variables and defaults.
func (o *ClientOptions) Defaults(providerEnvKey, defaultProxyURL string) {
	if o.TolvynAPIKey == "" {
		o.TolvynAPIKey = os.Getenv("TOLVYN_API_KEY")
	}
	if o.TolvynAPIKey == "" {
		panic("tolvyn: TolvynAPIKey is required (or set TOLVYN_API_KEY env var)")
	}
	if o.ProxyURL == "" {
		envURL := os.Getenv("TOLVYN_PROXY_URL")
		if envURL != "" {
			o.ProxyURL = envURL
		} else {
			o.ProxyURL = defaultProxyURL
		}
	}
	// Ensure ProxyURL ends with /
	if !strings.HasSuffix(o.ProxyURL, "/") {
		o.ProxyURL += "/"
	}
	if o.ProviderAPIKey == "" {
		o.ProviderAPIKey = os.Getenv(providerEnvKey)
	}
}

// TolvynHeaders returns the attribution headers to inject on every request.
func (o *ClientOptions) TolvynHeaders() map[string]string {
	headers := map[string]string{
		"Authorization": "Bearer " + o.TolvynAPIKey,
	}
	if o.Team != "" {
		headers["X-Tolvyn-Team"] = o.Team
	}
	if o.Service != "" {
		headers["X-Tolvyn-Service"] = o.Service
	}
	if o.Feature != "" {
		headers["X-Tolvyn-Feature"] = o.Feature
	}
	if o.Agent != "" {
		headers["X-Tolvyn-Agent"] = o.Agent
	}
	if o.User != "" {
		headers["X-Tolvyn-User"] = o.User
	}
	if o.EndCustomer != "" {
		headers["X-Tolvyn-End-Customer"] = o.EndCustomer
	}
	return headers
}

// IsProxyError returns true if the error indicates the TOLVYN proxy
// is unreachable (not a provider-side error).
func IsProxyError(err error, statusCode int) bool {
	if err != nil {
		msg := strings.ToLower(err.Error())
		return strings.Contains(msg, "connection refused") ||
			strings.Contains(msg, "no such host") ||
			strings.Contains(msg, "timeout") ||
			strings.Contains(msg, "dial tcp") ||
			strings.Contains(msg, "eof") ||
			strings.Contains(msg, "reset by peer")
	}
	return statusCode == http.StatusServiceUnavailable // 503
}

// ShouldNotFailOpen returns true for 4xx errors (except 503) —
// these are real API errors, not proxy failures.
func ShouldNotFailOpen(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != 503
}

// failOpenTransport is an http.RoundTripper that retries against the
// provider directly if the TOLVYN proxy is unreachable.
type failOpenTransport struct {
	base           http.RoundTripper
	tolvynProxyURL string
	providerURL    string
	providerAPIKey string
	headers        map[string]string
}

// NewFailOpenTransport creates an http.RoundTripper that retries against the
// provider directly if the TOLVYN proxy is unreachable.
func NewFailOpenTransport(proxyURL, providerURL, providerAPIKey string, headers map[string]string) http.RoundTripper {
	return &failOpenTransport{
		base: &http.Transport{
			ResponseHeaderTimeout: 30 * time.Second,
		},
		tolvynProxyURL: proxyURL,
		providerURL:    providerURL,
		providerAPIKey: providerAPIKey,
		headers:        headers,
	}
}

func (t *failOpenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Clone request and inject TOLVYN headers.
	proxyReq := req.Clone(req.Context())
	for k, v := range t.headers {
		proxyReq.Header.Set(k, v)
	}

	resp, err := t.base.RoundTrip(proxyReq)

	statusCode := 0
	if resp != nil {
		statusCode = resp.StatusCode
	}

	if t.providerAPIKey == "" {
		return resp, err
	}

	if ShouldNotFailOpen(statusCode) {
		// 4xx from provider — do not fail open.
		return resp, err
	}

	if !IsProxyError(err, statusCode) {
		// Not a proxy error — return as-is.
		return resp, err
	}

	// Fail open: retry directly against provider.
	fallbackReq := req.Clone(req.Context())

	if t.providerURL != "" {
		path := req.URL.Path
		for _, prefix := range []string{"/v1/proxy/openai", "/v1/proxy/anthropic", "/v1/proxy/google"} {
			if strings.HasPrefix(path, prefix) {
				path = path[len(prefix):]
				break
			}
		}
		fallbackURL := strings.TrimSuffix(t.providerURL, "/") + "/" + strings.TrimPrefix(path, "/")
		parsed, parseErr := fallbackReq.URL.Parse(fallbackURL)
		if parseErr == nil {
			fallbackReq.URL = parsed
		}
	}

	// Set provider API key and strip TOLVYN-specific headers.
	fallbackReq.Header.Set("Authorization", "Bearer "+t.providerAPIKey)
	for k := range t.headers {
		if strings.HasPrefix(strings.ToLower(k), "x-tolvyn-") {
			fallbackReq.Header.Del(k)
		}
	}

	return t.base.RoundTrip(fallbackReq)
}
