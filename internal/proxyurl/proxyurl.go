// Package proxyurl keeps keys on the proxy's host for every client tolvyn-go builds: the proxy URL rule, applied to
// every request URL, the redirect refusal, and the transport for a client with fail-open off. It is internal: none of
// it is part of the module's API.
package proxyurl

import (
	"fmt"
	"log"
	"net/http"
	"net/url"
	"sync"
)

const defaultHost = "proxy.tolvyn.io"

var warnedHosts sync.Map

// Validate refuses a proxy URL that is not https, except http to this machine (localhost, 127.0.0.1, ::1):
// anywhere else the TOLVYN key and the prompts would cross the network readable, and whoever is on the path could
// forge TOLVYN's outage answer and send calls around TOLVYN. A URL naming a host other than TOLVYN's is logged once.
func Validate(raw string) error {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return fmt.Errorf("tolvyn: the proxy URL is not a valid URL: %s", shown(raw))
	}
	h := u.Hostname()
	loopback := h == "localhost" || h == "127.0.0.1" || h == "::1"
	if u.Scheme != "https" && !(u.Scheme == "http" && loopback) {
		return fmt.Errorf("tolvyn: the proxy URL must start with https:// (http:// is accepted only for localhost): %s",
			shown(raw))
	}
	if h != defaultHost {
		if _, seen := warnedHosts.LoadOrStore(h, true); !seen {
			log.Printf("tolvyn: the proxy URL points at %s, not %s. Your TOLVYN key and your prompts are sent to %s.",
				h, defaultHost, h)
		}
	}
	return nil
}

// shown is the URL as an error may show it: no user info, no query, no fragment.
func shown(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return "(not a URL)"
	}
	u.User, u.RawQuery, u.Fragment = nil, "", ""
	return u.String()
}

// NoRedirect is the CheckRedirect of every client the SDK builds: a redirect is an error, so no key follows it to
// another address. Go drops Authorization on a hop to another host, but not X-Api-Key or X-Goog-Api-Key, and the
// transports would set TOLVYN headers again on the next hop. TOLVYN's proxy and the providers' APIs do not redirect.
func NoRedirect(req *http.Request, _ []*http.Request) error {
	return fmt.Errorf("tolvyn: a redirect to %s was not followed, so no key was sent there", req.URL.Host)
}

// HostOf is the host[:port] of a URL, or "" when it does not parse.
func HostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	return u.Host
}

// transport is the transport when fail-open is off: it refuses a proxy URL that breaks the rule, and sets TOLVYN
// headers only on requests to the proxy's host.
type transport struct {
	base      http.RoundTripper
	proxyHost string
	proxyErr  error
	headers   map[string]string
}

// NewTransport returns the transport for a client with fail-open off.
func NewTransport(proxyURL string, headers map[string]string) http.RoundTripper {
	return &transport{base: http.DefaultTransport, proxyHost: HostOf(proxyURL), proxyErr: Validate(proxyURL),
		headers: headers}
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.proxyErr != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		return nil, t.proxyErr
	}
	if err := CheckRequest(req); err != nil { // a per-request base URL is held to the rule too
		return nil, err
	}
	if len(t.headers) == 0 || req.URL.Host != t.proxyHost {
		return t.base.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	for k, v := range t.headers {
		r.Header.Set(k, v)
	}
	return t.base.RoundTrip(r)
}

// CheckRequest holds a request's own URL to the rule, so a base URL given per request (option.WithBaseURL) is refused
// like a proxy URL. The request's body is closed when it is refused.
func CheckRequest(req *http.Request) error {
	if err := Validate(req.URL.String()); err != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		return err
	}
	return nil
}
