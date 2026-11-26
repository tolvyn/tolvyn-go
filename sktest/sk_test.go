// The SK round's tests for tolvyn-go, over loopback only (run in a no-network sandbox). 127.0.0.1 is "the proxy",
// localhost is "another host".
//   - BE-610, BE-611: a 302 or 307 from the proxy to another host gives that host nothing, for every wrapper,
//     fail-open on and off; the transports set TOLVYN headers only on the proxy's host.
//   - BE-612: a proxy URL that is not https is refused, except to localhost and 127.0.0.1; by option and by
//     TOLVYN_PROXY_URL.
//   - BE-661: a proxy host other than proxy.tolvyn.io is logged once, naming it.
package sktest

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/google/generative-ai-go/genai"
	oai "github.com/openai/openai-go"
	tolvyn "github.com/tolvyn/tolvyn-go"
	tolvynanthropic "github.com/tolvyn/tolvyn-go/anthropic"
	tolvyndeepseek "github.com/tolvyn/tolvyn-go/deepseek"
	tolvyngoogle "github.com/tolvyn/tolvyn-go/google"
	tolvynopenai "github.com/tolvyn/tolvyn-go/openai"
)

const tlv, pk = "tlv_dummy_key", "provider-dummy-key"

type rec struct {
	mu   sync.Mutex
	reqs []http.Header
}

func (r *rec) wrap(answer http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, q *http.Request) {
		io.Copy(io.Discard, q.Body) //nolint:errcheck
		r.mu.Lock()
		r.reqs = append(r.reqs, q.Header.Clone())
		r.mu.Unlock()
		answer(w, q)
	}
}

func (r *rec) n() int { r.mu.Lock(); defer r.mu.Unlock(); return len(r.reqs) }

func ok(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	io.WriteString(w, `{"id":"x","object":"chat.completion","created":0,"model":"m","choices":[],`+ //nolint:errcheck
		`"type":"message","role":"assistant","content":[],"stop_reason":"end_turn","usage":{"input_tokens":0,"output_tokens":0},`+
		`"candidates":[{"content":{"parts":[{"text":"ok"}],"role":"model"}}]}`)
}

func opts(proxy string, failOpen bool) tolvyn.ClientOptions {
	o := tolvyn.ClientOptions{TolvynAPIKey: tlv, ProxyURL: proxy, Team: "t"}
	if failOpen {
		o.ProviderAPIKey = pk
	} else {
		o.DisableFailOpen = true
	}
	return o
}

var ctx = context.Background()

func chatParams() oai.ChatCompletionNewParams {
	return oai.ChatCompletionNewParams{
		Model: oai.F(oai.ChatModelGPT4oMini),
		Messages: oai.F([]oai.ChatCompletionMessageParamUnion{oai.ChatCompletionUserMessageParam{
			Role:    oai.F(oai.ChatCompletionUserMessageParamRoleUser),
			Content: oai.F([]oai.ChatCompletionContentPartUnionParam{oai.ChatCompletionContentPartTextParam{Text: oai.F("hi"), Type: oai.F(oai.ChatCompletionContentPartTextTypeText)}}),
		}}),
	}
}

// Each wrapper's call, against a proxy base URL (no path).
var calls = map[string]func(base string, failOpen bool) error{
	"openai": func(b string, fo bool) error {
		_, err := tolvynopenai.NewClient(opts(b+"/v1/proxy/openai/", fo)).Chat.Completions.New(ctx, chatParams())
		return err
	},
	"deepseek": func(b string, fo bool) error {
		_, err := tolvyndeepseek.NewClient(opts(b+"/v1/proxy/deepseek/", fo)).Chat.Completions.New(ctx, chatParams())
		return err
	},
	"anthropic": func(b string, fo bool) error {
		_, err := tolvynanthropic.NewClient(opts(b+"/v1/proxy/anthropic/", fo)).Messages.New(ctx, sdk.MessageNewParams{
			Model: sdk.F(sdk.ModelClaude3_5HaikuLatest), MaxTokens: sdk.F(int64(1)),
			Messages: sdk.F([]sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock("hi"))}),
		})
		return err
	},
	"google": func(b string, fo bool) error {
		c, err := tolvyngoogle.NewClient(ctx, opts(b+"/v1/proxy/google/", fo))
		if err != nil {
			return err
		}
		_, err = c.GenerativeModel("gemini-x").GenerateContent(ctx, genai.Text("hi"))
		return err
	},
}

func TestRedirectFromTheProxyGivesTheOtherHostNothing(t *testing.T) {
	other := &rec{}
	o := httptest.NewServer(other.wrap(ok))
	defer o.Close()
	otherURL := strings.Replace(o.URL, "127.0.0.1", "localhost", 1)
	for _, code := range []int{http.StatusFound, http.StatusTemporaryRedirect} {
		proxy := &rec{}
		p := httptest.NewServer(proxy.wrap(func(w http.ResponseWriter, q *http.Request) {
			http.Redirect(w, q, otherURL+q.URL.Path, code)
		}))
		for name, call := range calls {
			for _, fo := range []bool{true, false} {
				before := other.n()
				err := call(p.URL, fo)
				if err == nil || other.n() != before {
					t.Errorf("%s, %d, fail-open %v: err=%v, the other host got %d request(s)", name, code, fo, err, other.n()-before)
				}
			}
		}
		if proxy.n() == 0 {
			t.Errorf("%d: the proxy saw nothing", code)
		}
		p.Close()
	}
}

func TestTheTransportsSetTolvynHeadersOnlyOnTheProxysHost(t *testing.T) {
	other := &rec{}
	o := httptest.NewServer(other.wrap(ok))
	defer o.Close()
	headers := map[string]string{"Authorization": "Bearer " + tlv, "X-Tolvyn-Team": "t"}
	for name, tr := range map[string]http.RoundTripper{
		"fail-open":   tolvyn.NewFailOpenTransport("http://127.0.0.1:1/v1/proxy/openai/", "https://api.openai.com/v1", pk, "openai", headers),
		"proxy-only":  tolvyn.NewProxyTransport("http://127.0.0.1:1/v1/proxy/openai/", headers),
	} {
		req, _ := http.NewRequest("GET", strings.Replace(o.URL, "127.0.0.1", "localhost", 1)+"/x", nil)
		resp, err := tr.RoundTrip(req)
		if err == nil {
			resp.Body.Close()
		}
		other.mu.Lock()
		h := other.reqs[len(other.reqs)-1]
		other.mu.Unlock()
		if h.Get("Authorization") != "" || h.Get("X-Tolvyn-Team") != "" {
			t.Errorf("%s: a request to another host carried %q / %q", name, h.Get("Authorization"), h.Get("X-Tolvyn-Team"))
		}
	}
}

func TestAProxyURLThatIsNotHttpsIsRefused(t *testing.T) {
	for name, call := range calls {
		if err := call("http://example.com", false); err == nil || !strings.Contains(err.Error(), "https://") {
			t.Errorf("%s: http://example.com accepted (err=%v)", name, err)
		}
	}
	t.Setenv("TOLVYN_PROXY_URL", "http://203.0.113.9/v1/proxy/openai/")
	_, err := tolvynopenai.NewClient(tolvyn.ClientOptions{TolvynAPIKey: tlv, DisableFailOpen: true}).Chat.Completions.New(ctx, chatParams())
	if err == nil || !strings.Contains(err.Error(), "https://") {
		t.Errorf("TOLVYN_PROXY_URL http://203.0.113.9 accepted (err=%v)", err)
	}
}

func TestHttpToThisMachineIsAccepted(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(ok))
	defer s.Close()
	for _, host := range []string{"127.0.0.1", "localhost"} {
		base := strings.Replace(s.URL, "127.0.0.1", host, 1)
		if err := calls["openai"](base, false); err != nil {
			t.Errorf("%s: %v", host, err)
		}
	}
}

func TestAHostOtherThanTolvynsIsLoggedOnce(t *testing.T) {
	var buf bytes.Buffer
	log.SetOutput(&buf)
	defer log.SetOutput(io.Discard)
	tolvynopenai.NewClient(opts("https://gw-once.example.com/v1/proxy/openai/", false))
	tolvynanthropic.NewClient(opts("https://gw-once.example.com/v1/proxy/anthropic/", false))
	tolvynopenai.NewClient(tolvyn.ClientOptions{TolvynAPIKey: tlv, DisableFailOpen: true}) // the default host
	if n := strings.Count(buf.String(), "gw-once.example.com, not proxy.tolvyn.io"); n != 1 {
		t.Errorf("warned %d times, want 1: %q", n, buf.String())
	}
	if strings.Contains(buf.String(), "proxy.tolvyn.io, not") {
		t.Errorf("the default host warned: %q", buf.String())
	}
}
