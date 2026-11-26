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
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"io/fs"
	"log"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"

	sdk "github.com/anthropics/anthropic-sdk-go"
	sdkopt "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/google/generative-ai-go/genai"
	oai "github.com/openai/openai-go"
	oaiopt "github.com/openai/openai-go/option"
	tolvyn "github.com/tolvyn/tolvyn-go"
	tolvynanthropic "github.com/tolvyn/tolvyn-go/anthropic"
	tolvyndeepseek "github.com/tolvyn/tolvyn-go/deepseek"
	tolvyngoogle "github.com/tolvyn/tolvyn-go/google"
	"github.com/tolvyn/tolvyn-go/internal/proxyurl"
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
		"fail-open":  tolvyn.NewFailOpenTransport("http://127.0.0.1:1/v1/proxy/openai/", "https://api.openai.com/v1", pk, "openai", headers),
		"proxy-only": proxyurl.NewTransport("http://127.0.0.1:1/v1/proxy/openai/", headers),
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
		for _, fo := range []bool{true, false} {
			if err := call("http://example.com", fo); err == nil || !strings.Contains(err.Error(), "https://") {
				t.Errorf("%s, fail-open %v: http://example.com accepted (err=%v)", name, fo, err)
			}
		}
	}
	// google.NewClient refuses at once, before any request.
	if _, err := tolvyngoogle.NewClient(ctx, opts("http://example.com/v1/proxy/google/", false)); err == nil {
		t.Errorf("google.NewClient accepted http://example.com")
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

// D1: the provider SDKs retry a failed request (X-Stainless-Retry-Count 1, 2, ...), and a retry may follow an attempt
// that was sent. Only a first attempt goes direct.
func TestARetryOfASentRequestNeverGoesDirect(t *testing.T) {
	provider := &rec{}
	p := httptest.NewServer(provider.wrap(ok))
	defer p.Close()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	refused := "http://" + l.Addr().String()
	l.Close()
	tr := tolvyn.NewFailOpenTransport(refused+"/v1/proxy/openai/", p.URL+"/v1/", pk, "openai", nil)
	for _, c := range []struct {
		count  string
		direct int
	}{{"1", 0}, {"2", 0}, {"0", 1}, {"", 1}} {
		before := provider.n()
		req, _ := http.NewRequest("POST", refused+"/v1/proxy/openai/chat/completions", strings.NewReader("{}"))
		if c.count != "" {
			req.Header.Set("X-Stainless-Retry-Count", c.count)
		}
		resp, err := tr.RoundTrip(req)
		if resp != nil {
			resp.Body.Close()
		}
		if got := provider.n() - before; got != c.direct {
			t.Errorf("retry count %q: %d direct request(s), want %d", c.count, got, c.direct)
		}
		if c.direct == 0 && err == nil {
			t.Errorf("retry count %q: no error", c.count)
		}
	}
}

// Decision 1 (Gate 2): v0.1.6 exports nothing v0.1.5 did not. The redirect refusal, the proxy transport and the URL
// rule live in an internal package. The list is v0.1.5's exported API, read from ecbe2c9.
func TestNoNewExportedAPI(t *testing.T) {
	want := map[string][]string{
		"..":           {"ClientOptions", "ClientOptions.Defaults", "ClientOptions.TolvynHeaders", "IsProxyError", "IsTolvynUnavailable", "NewFailOpenTransport", "ProxyNotReached", "ShouldNotFailOpen", "TolvynUnavailableHeader", "Version"},
		"../openai":    {"Client", "NewClient", "NewClientFromEnv"},
		"../anthropic": {"Client", "NewClient", "NewClientFromEnv"},
		"../deepseek":  {"Client", "NewClient", "NewClientFromEnv"},
		"../google":    {"Client", "NewClient", "NewClientFromEnv"},
	}
	for dir, names := range want {
		got := exportedNames(t, dir)
		if strings.Join(got, " ") != strings.Join(names, " ") {
			t.Errorf("%s exports %v, want %v", dir, got, names)
		}
	}
}

func exportedNames(t *testing.T, dir string) []string {
	t.Helper()
	pkgs, err := parser.ParseDir(token.NewFileSet(), dir, func(fi fs.FileInfo) bool { return !strings.HasSuffix(fi.Name(), "_test.go") }, 0)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, p := range pkgs {
		for _, f := range p.Files {
			for _, d := range f.Decls {
				switch d := d.(type) {
				case *ast.FuncDecl:
					if !d.Name.IsExported() {
						continue
					}
					if d.Recv == nil {
						names = append(names, d.Name.Name)
						continue
					}
					rt := d.Recv.List[0].Type
					if s, ok := rt.(*ast.StarExpr); ok {
						rt = s.X
					}
					if id, ok := rt.(*ast.Ident); ok && id.IsExported() {
						names = append(names, id.Name+"."+d.Name.Name)
					}
				case *ast.GenDecl:
					for _, s := range d.Specs {
						switch s := s.(type) {
						case *ast.TypeSpec:
							if s.Name.IsExported() {
								names = append(names, s.Name.Name)
							}
						case *ast.ValueSpec:
							for _, n := range s.Names {
								if n.IsExported() {
									names = append(names, n.Name)
								}
							}
						}
					}
				}
			}
		}
	}
	sort.Strings(names)
	return names
}

// Decision 2 (Gate 2): the URL rule holds for every URL a client sends to, a per-request option.WithBaseURL included.
// 127.0.0.2 is this machine but not one of the names the rule accepts over http, so a request there is refused, and
// the stand-in listening there shows whether anything arrived.
func TestAPerRequestBaseURLFollowsTheRule(t *testing.T) {
	other := &rec{}
	l, err := net.Listen("tcp", "127.0.0.2:0")
	if err != nil {
		t.Fatal(err)
	}
	o := httptest.NewUnstartedServer(other.wrap(ok))
	o.Listener.Close()
	o.Listener = l
	o.Start()
	defer o.Close()
	proxy := httptest.NewServer((&rec{}).wrap(ok))
	defer proxy.Close()
	for _, fo := range []bool{true, false} {
		calls := map[string]func() error{
			"openai": func() error {
				_, err := tolvynopenai.NewClient(opts(proxy.URL+"/v1/proxy/openai/", fo)).Chat.Completions.New(ctx, chatParams(),
					oaiopt.WithBaseURL(o.URL+"/v1/"), oaiopt.WithMaxRetries(0))
				return err
			},
			"deepseek": func() error {
				_, err := tolvyndeepseek.NewClient(opts(proxy.URL+"/v1/proxy/deepseek/", fo)).Chat.Completions.New(ctx, chatParams(),
					oaiopt.WithBaseURL(o.URL+"/v1/"), oaiopt.WithMaxRetries(0))
				return err
			},
			"anthropic": func() error {
				_, err := tolvynanthropic.NewClient(opts(proxy.URL+"/v1/proxy/anthropic/", fo)).Messages.New(ctx, sdk.MessageNewParams{
					Model: sdk.F(sdk.ModelClaude3_5HaikuLatest), MaxTokens: sdk.F(int64(1)),
					Messages: sdk.F([]sdk.MessageParam{sdk.NewUserMessage(sdk.NewTextBlock("hi"))}),
				}, sdkopt.WithBaseURL(o.URL+"/"), sdkopt.WithMaxRetries(0))
				return err
			},
		}
		for name, call := range calls {
			before := other.n()
			err := call()
			if err == nil || !strings.Contains(err.Error(), "must start with https://") {
				t.Errorf("%s, fail-open %v: err=%v", name, fo, err)
			}
			if n := other.n() - before; n != 0 {
				t.Errorf("%s, fail-open %v: the per-request base URL got %d request(s)", name, fo, n)
			}
		}
	}
}

// The module's Version constant is the version the CHANGELOG is about to release, and the release workflow refuses a
// tag that is not "v" + Version: v0.1.6 must not ship saying 0.1.5.
func TestVersionIsTheChangelogsAndTheWorkflowChecksTheTag(t *testing.T) {
	src, _ := os.ReadFile("../tolvyn.go")
	log, _ := os.ReadFile("../CHANGELOG.md")
	wf, _ := os.ReadFile("../.github/workflows/release.yml")
	v := regexp.MustCompile(`(?m)^const Version = "([^"]+)"`).FindSubmatch(src)
	top := regexp.MustCompile(`(?m)^## v?([0-9][0-9.]*)`).FindSubmatch(log)
	if v == nil || top == nil || string(v[1]) != string(top[1]) {
		t.Errorf("Version %q, CHANGELOG's top %q", v, top)
	}
	if !strings.Contains(string(wf), `test "v$(sed -n 's/^const Version = "\(.*\)"$/\1/p' tolvyn.go)" = "$GITHUB_REF_NAME"`) {
		t.Errorf("the release workflow does not check the tag against Version")
	}
}
