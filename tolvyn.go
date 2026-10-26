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
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"strings"
	"time"
)

const Version = "0.1.4"

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

	// DisableFailOpen disables automatic fallback to provider direct if TOLVYN is unreachable.
	// Default (false) = fail-open enabled — matches Python and Node SDK defaults.
	// Set to true to force all requests through TOLVYN (hard failure on TOLVYN outages).
	DisableFailOpen bool

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
		// GO-03: log.Fatal is more idiomatic Go than panic for misconfiguration.
		// Still process-exit (NewClient signature is locked for backwards compat),
		// but emits a clean error message without a stack trace.
		log.Fatal("tolvyn: TolvynAPIKey is required (set TOLVYN_API_KEY env var)")
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
//
// Deprecated: the fail-open transport no longer uses this. Its substring match
// ("timeout", "eof", "reset by peer") and its 503 rule also fire AFTER the proxy
// forwarded the request, or on the provider's own 503 passed through, and a
// direct retry then made a second, unmetered provider call. Kept for API
// compatibility; see ProxyNotReached and IsTolvynUnavailable.
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
//
// Deprecated: the fail-open transport no longer uses this; it fails open only
// when ProxyNotReached or IsTolvynUnavailable.
func ShouldNotFailOpen(statusCode int) bool {
	return statusCode >= 400 && statusCode < 500 && statusCode != 503
}

// TolvynUnavailableHeader marks a 503 as TOLVYN's own ("TOLVYN is unavailable")
// rather than the provider's 503 passed through the proxy.
const TolvynUnavailableHeader = "X-Tolvyn-Unavailable"

// ProxyNotReached reports whether err shows the request never reached the
// proxy: the dial failed (refused, unreachable, timeout while connecting), the
// name did not resolve, or the TLS handshake failed. In each case nothing can
// have been forwarded to the provider, so a direct retry cannot double-bill.
//
// Deliberately excluded: response-header timeouts, resets and EOF after the
// request was written. Each can follow a forwarded request.
func ProxyNotReached(err error) bool {
	if err == nil {
		return false
	}
	var opErr *net.OpError
	if errors.As(err, &opErr) && opErr.Op == "dial" {
		return true
	}
	var dnsErr *net.DNSError
	if errors.As(err, &dnsErr) {
		return true
	}
	var (
		recErr  tls.RecordHeaderError
		verErr  *tls.CertificateVerificationError
		authErr x509.UnknownAuthorityError
		hostErr x509.HostnameError
		invErr  x509.CertificateInvalidError
	)
	return errors.As(err, &recErr) || errors.As(err, &verErr) || errors.As(err, &authErr) ||
		errors.As(err, &hostErr) || errors.As(err, &invErr)
}

// IsTolvynUnavailable reports whether resp is a 503 TOLVYN marked as its own.
// A plain 503 on the proxy path is the provider's, passed through — the server
// never writes 503 on /v1/proxy/ — and enforcement (451, 429) never fails open.
func IsTolvynUnavailable(resp *http.Response) bool {
	return resp != nil && resp.StatusCode == http.StatusServiceUnavailable &&
		strings.TrimSpace(resp.Header.Get(TolvynUnavailableHeader)) == "1"
}

// failOpenTransport is an http.RoundTripper that retries against the
// provider directly if the TOLVYN proxy is unreachable.
type failOpenTransport struct {
	base           http.RoundTripper
	tolvynProxyURL string
	providerURL    string
	providerAPIKey string
	provider       string // "openai" | "anthropic" | "google" | "deepseek" — selects fallback auth header
	headers        map[string]string
}

// NewFailOpenTransport creates an http.RoundTripper that retries against the
// provider directly only if the TOLVYN proxy was never reached, or answered 503
// with X-Tolvyn-Unavailable: 1.
//
// provider selects the auth header used on the direct fallback request
// ("openai"/"deepseek" → Authorization: Bearer, "anthropic" → X-Api-Key,
// "google" → X-Goog-Api-Key).
func NewFailOpenTransport(proxyURL, providerURL, providerAPIKey, provider string, headers map[string]string) http.RoundTripper {
	return &failOpenTransport{
		base: &http.Transport{
			ResponseHeaderTimeout: 30 * time.Second,
		},
		tolvynProxyURL: proxyURL,
		providerURL:    providerURL,
		providerAPIKey: providerAPIKey,
		provider:       provider,
		headers:        headers,
	}
}

// providerAuthHeader maps a provider to the header its DIRECT API reads the API
// key from. The TOLVYN proxy accepts Authorization/X-Api-Key/X-Goog-Api-Key
// interchangeably, but the providers do NOT: Anthropic authenticates only via
// X-Api-Key and Google only via X-Goog-Api-Key — sending Bearer to them 401s.
var providerAuthHeader = map[string]string{
	"openai":    "Authorization",
	"anthropic": "X-Api-Key",
	"google":    "X-Goog-Api-Key",
	"deepseek":  "Authorization", // OpenAI-compatible
}

// proxyPrefixes are stripped from the request path to build the direct URL.
var proxyPrefixes = []string{"/v1/proxy/openai", "/v1/proxy/anthropic", "/v1/proxy/google", "/v1/proxy/deepseek"}

// notForwarded are never copied onto the direct request: Host names the PROXY,
// and the rest are recomputed or hop-by-hop.
var notForwarded = map[string]bool{
	"Host": true, "Content-Length": true, "Transfer-Encoding": true, "Connection": true,
	"Keep-Alive": true, "Te": true, "Trailer": true, "Upgrade": true, "Proxy-Connection": true,
}

// applyFallbackAuth rewrites h in place for a direct provider call. It strips
// every inbound auth header (each may carry the TOLVYN key) and sets the single
// header the provider's direct API expects, with the provider's own key.
// Fixes GO-08 (Bearer sent to Anthropic/Google → 401) and GO-09 (the TOLVYN key
// in X-Api-Key/X-Goog-Api-Key leaking to the provider on fallback).
func applyFallbackAuth(h http.Header, provider, providerKey string) {
	h.Del("Authorization")
	h.Del("X-Api-Key")
	h.Del("X-Goog-Api-Key")
	header := providerAuthHeader[strings.ToLower(provider)]
	if header == "" {
		header = "Authorization"
	}
	if header == "Authorization" {
		h.Set("Authorization", "Bearer "+providerKey)
	} else {
		h.Set(header, providerKey)
	}
}

// fallbackHeader is the direct request's header set: every X-Tolvyn-* header
// is dropped, in any casing, whether set on the client or per request — they
// carry attribution, and X-Tolvyn-User / X-Tolvyn-End-Customer can be personal
// data — then auth is replaced with the provider's own key.
func fallbackHeader(src http.Header, provider, providerKey string) http.Header {
	out := make(http.Header, len(src))
	for k, vs := range src {
		if notForwarded[http.CanonicalHeaderKey(k)] || strings.HasPrefix(strings.ToLower(k), "x-tolvyn-") {
			continue
		}
		out[k] = append([]string(nil), vs...)
	}
	applyFallbackAuth(out, provider, providerKey)
	return out
}

// replayableBody returns a function that yields a fresh copy of req's body for
// each attempt. The first attempt consumes req.Body, so the fallback used to go
// out with an empty body and fail ("ContentLength=N with Body length 0").
func replayableBody(req *http.Request) (func() (io.ReadCloser, error), error) {
	if req.Body == nil || req.Body == http.NoBody {
		return func() (io.ReadCloser, error) { return http.NoBody, nil }, nil
	}
	if req.GetBody != nil {
		return req.GetBody, nil
	}
	b, err := io.ReadAll(req.Body)
	req.Body.Close()
	if err != nil {
		return nil, err
	}
	return func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }, nil
}

func (t *failOpenTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	getBody, err := replayableBody(req)
	if err != nil {
		return nil, err
	}

	// Clone request and inject TOLVYN headers.
	proxyReq := req.Clone(req.Context())
	for k, v := range t.headers {
		proxyReq.Header.Set(k, v)
	}
	if proxyReq.Body, err = getBody(); err != nil {
		return nil, err
	}
	proxyReq.GetBody = getBody

	resp, err := t.base.RoundTrip(proxyReq)

	if t.providerAPIKey == "" {
		return resp, err
	}
	if err != nil {
		if !ProxyNotReached(err) {
			return resp, err
		}
	} else {
		if !IsTolvynUnavailable(resp) {
			return resp, nil
		}
		// Discard TOLVYN's 503 before going direct.
		io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)) //nolint:errcheck
		resp.Body.Close()
	}

	// Fail open: retry directly against provider.
	target := req.URL.String()
	if t.providerURL != "" {
		path := req.URL.Path
		for _, prefix := range proxyPrefixes {
			if strings.HasPrefix(path, prefix) {
				path = path[len(prefix):]
				break
			}
		}
		target = strings.TrimSuffix(t.providerURL, "/") + "/" + strings.TrimPrefix(path, "/")
		if req.URL.RawQuery != "" {
			target += "?" + req.URL.RawQuery
		}
	}
	body, err := getBody()
	if err != nil {
		return nil, err
	}
	// A NEW request, not a clone: a clone kept req.Host, which named the proxy.
	fallbackReq, err := http.NewRequestWithContext(req.Context(), req.Method, target, body)
	if err != nil {
		body.Close()
		return nil, err
	}
	fallbackReq.ContentLength = req.ContentLength
	fallbackReq.GetBody = getBody
	fallbackReq.Header = fallbackHeader(req.Header, t.provider, t.providerAPIKey)

	log.Printf("TOLVYN proxy unreachable — routing direct to %s (fail-open)", t.provider)
	return t.base.RoundTrip(fallbackReq)
}
