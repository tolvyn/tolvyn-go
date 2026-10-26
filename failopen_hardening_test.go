package tolvyn

// Fail-open hardening (BE-507..BE-515): fail open ONLY when the proxy was never
// reached, or answered 503 with X-Tolvyn-Unavailable: 1; never forward any
// X-Tolvyn-* header; replay the body; send the provider's own Host; close the 503.
//
// Real sockets: a stand-in provider records every request; stand-in proxies
// refuse, answer 503 (marked or not), enforce (451/429), or forward and then
// hang or reset — where a direct retry would be a second, unmetered call.

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

type seenReq struct {
	via, method, path, host string
	header                  http.Header
	body                    string
}

type standins struct {
	mu       sync.Mutex
	seen     []seenReq
	provider *httptest.Server
	proxies  map[string]string // mode → base URL
}

func (s *standins) direct() []seenReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []seenReq
	for _, r := range s.seen {
		if r.via == "direct" {
			out = append(out, r)
		}
	}
	return out
}

func (s *standins) all() []seenReq {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]seenReq(nil), s.seen...)
}

func newStandins(t *testing.T) *standins {
	s := &standins{proxies: map[string]string{}}
	s.provider = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		via := r.Header.Get("X-Via")
		if via == "" {
			via = "direct"
		}
		s.mu.Lock()
		s.seen = append(s.seen, seenReq{via, r.Method, r.URL.RequestURI(), r.Host, r.Header.Clone(), string(b)})
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"ok":true}`)) //nolint:errcheck
	}))
	t.Cleanup(s.provider.Close)

	forward := func(r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		fr, _ := http.NewRequest(r.Method, s.provider.URL+r.URL.RequestURI(), bytes.NewReader(b))
		fr.Header.Set("X-Via", "proxy")
		resp, err := http.DefaultClient.Do(fr)
		if err == nil {
			resp.Body.Close()
		}
	}
	status := func(code int, marked bool) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			io.Copy(io.Discard, r.Body) //nolint:errcheck
			if marked {
				w.Header().Set(TolvynUnavailableHeader, "1")
			}
			w.WriteHeader(code)
			w.Write([]byte(`{"error":"stand-in"}`)) //nolint:errcheck
		}
	}
	handlers := map[string]http.HandlerFunc{
		"503":        status(503, false),
		"503-tolvyn": status(503, true),
		"451":        status(451, false),
		"429":        status(429, false),
		"timeout": func(w http.ResponseWriter, r *http.Request) {
			forward(r)
			select {
			case <-r.Context().Done():
			case <-time.After(3 * time.Second):
			}
		},
		"reset": func(w http.ResponseWriter, r *http.Request) {
			forward(r)
			conn, _, err := w.(http.Hijacker).Hijack()
			if err == nil {
				conn.(*net.TCPConn).SetLinger(0) //nolint:errcheck
				conn.Close()
			}
		},
	}
	for mode, h := range handlers {
		srv := httptest.NewServer(h)
		t.Cleanup(srv.Close)
		s.proxies[mode] = srv.URL
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s.proxies["refused"] = "http://" + ln.Addr().String()
	ln.Close()
	return s
}

var hardeningProviders = []struct{ name, proxyPath, directBase string }{
	{"openai", "/v1/proxy/openai/chat/completions", "/v1/"},
	{"anthropic", "/v1/proxy/anthropic/v1/messages", "/"},
	{"google", "/v1/proxy/google/v1beta/models/gemini-x:generateContent", "/"},
	{"deepseek", "/v1/proxy/deepseek/chat/completions", "/v1/"},
}

const hardeningBody = `{"model":"m","messages":[{"role":"user","content":"hi"}]}`

var clientAttr = map[string]string{"X-Tolvyn-Team": "t-team", "X-Tolvyn-User": "pii-user@example.com", "X-Tolvyn-End-Customer": "pii-customer-42"}
var perRequestAttr = map[string]string{"X-Tolvyn-User": "pii-user@example.com", "x-tolvyn-end-customer": "pii-customer-42", "X-TOLVYN-FEATURE": "t-feature"}

func doHardening(s *standins, provider, proxyPath, directBase, mode, hdrMode string) (*http.Response, error) {
	var headers map[string]string
	if hdrMode == "client" {
		headers = clientAttr
	}
	tr := &failOpenTransport{
		base:           &http.Transport{ResponseHeaderTimeout: 500 * time.Millisecond},
		providerURL:    s.provider.URL + directBase,
		providerAPIKey: "sk-direct",
		provider:       provider,
		headers:        headers,
	}
	req, _ := http.NewRequestWithContext(context.Background(), "POST", s.proxies[mode]+proxyPath, strings.NewReader(hardeningBody))
	req.GetBody = nil // as a caller that does not set it: the transport must buffer
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer tlv_x")
	req.Header.Set("X-Api-Key", "tlv_x")
	if hdrMode == "request" {
		for k, v := range perRequestAttr {
			req.Header[k] = []string{v} // the caller's exact casing
		}
	}
	return tr.RoundTrip(req)
}

func TestFailOpenHardening(t *testing.T) {
	s := newStandins(t)
	for _, p := range hardeningProviders {
		for _, hdr := range []string{"client", "request"} {
			for _, mode := range []string{"refused", "503-tolvyn", "503", "451", "429", "timeout", "reset"} {
				p, hdr, mode := p, hdr, mode
				t.Run(p.name+"/"+hdr+"/"+mode, func(t *testing.T) {
					before := len(s.all())
					resp, err := doHardening(s, p.name, p.proxyPath, p.directBase, mode, hdr)
					if resp != nil {
						defer resp.Body.Close()
					}
					got := s.all()[before:]
					var direct []seenReq
					for _, r := range got {
						if r.via == "direct" {
							direct = append(direct, r)
						}
					}
					switch mode {
					case "refused", "503-tolvyn":
						if err != nil || resp.StatusCode != 200 {
							t.Fatalf("want fail-open 200, got resp=%v err=%v", resp, err)
						}
						if len(direct) != 1 {
							t.Fatalf("want 1 direct call, got %d", len(direct))
						}
						d := direct[0]
						for k, vs := range d.header {
							if strings.HasPrefix(strings.ToLower(k), "x-tolvyn-") {
								t.Errorf("X-Tolvyn header reached the provider: %s", k)
							}
							for _, v := range vs {
								if strings.Contains(v, "pii-") || strings.Contains(v, "tlv_") {
									t.Errorf("%s=%q reached the provider", k, v)
								}
							}
						}
						if d.body != hardeningBody {
							t.Errorf("body not replayed: %q", d.body)
						}
						if d.host != strings.TrimPrefix(s.provider.URL, "http://") {
							t.Errorf("Host = %q, want the provider's", d.host)
						}
						wantPath := strings.TrimSuffix(p.directBase, "/") + "/" + strings.TrimPrefix(p.proxyPath[len("/v1/proxy/"+p.name):], "/")
						if d.path != wantPath {
							t.Errorf("path = %q, want %q", d.path, wantPath)
						}
					case "503", "451", "429":
						if err != nil || resp.StatusCode != atoi(mode) {
							t.Fatalf("want %s passed through, got resp=%v err=%v", mode, resp, err)
						}
						if len(got) != 0 {
							t.Fatalf("provider was called: %+v", got)
						}
					case "timeout", "reset":
						if err == nil {
							t.Fatalf("want an error, got %d", resp.StatusCode)
						}
						if len(direct) != 0 {
							t.Fatalf("duplicate: %d direct call(s) after the proxy forwarded", len(direct))
						}
					}
				})
			}
		}
	}
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}

// closeRecorder wraps a body and records Close.
type closeRecorder struct {
	io.ReadCloser
	closed *bool
}

func (c closeRecorder) Close() error { *c.closed = true; return c.ReadCloser.Close() }

type rtFunc func(*http.Request) (*http.Response, error)

func (f rtFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestMarked503IsClosedBeforeFallback(t *testing.T) {
	s := newStandins(t)
	closed := false
	inner := &http.Transport{}
	tr := &failOpenTransport{
		base: rtFunc(func(r *http.Request) (*http.Response, error) {
			resp, err := inner.RoundTrip(r)
			if err == nil && resp.StatusCode == 503 {
				resp.Body = closeRecorder{resp.Body, &closed}
			}
			return resp, err
		}),
		providerURL: s.provider.URL + "/v1/", providerAPIKey: "sk-direct", provider: "openai",
	}
	req, _ := http.NewRequest("POST", s.proxies["503-tolvyn"]+"/v1/proxy/openai/chat/completions", strings.NewReader(hardeningBody))
	resp, err := tr.RoundTrip(req)
	if err != nil || resp.StatusCode != 200 {
		t.Fatalf("resp=%v err=%v", resp, err)
	}
	resp.Body.Close()
	if !closed {
		t.Fatal("the discarded 503 body was never closed")
	}
}

func TestProxyNotReached(t *testing.T) {
	ln, _ := net.Listen("tcp", "127.0.0.1:0")
	addr := ln.Addr().String()
	ln.Close()
	_, dialErr := (&http.Transport{}).RoundTrip(mustReq("http://" + addr + "/"))
	if !ProxyNotReached(dialErr) {
		t.Errorf("refused dial: want true, got false (%v)", dialErr)
	}
	if !ProxyNotReached(&net.DNSError{Err: "no such host", Name: "x.invalid"}) {
		t.Error("DNS error: want true")
	}
	for _, err := range []error{io.EOF, io.ErrUnexpectedEOF, &net.OpError{Op: "read", Err: io.EOF}, nil} {
		if ProxyNotReached(err) {
			t.Errorf("%v: want false", err)
		}
	}
}

func mustReq(u string) *http.Request {
	r, _ := http.NewRequest("GET", u, nil)
	return r
}
