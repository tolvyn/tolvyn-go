package openai_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	oai "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	tolvyn "github.com/tolvyn/tolvyn-go"
	tolvynopenai "github.com/tolvyn/tolvyn-go/openai"
)

func TestHeaderInjection(t *testing.T) {
	var capturedHeaders http.Header

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedHeaders = r.Header.Clone()
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		//nolint:errcheck
		w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}],"model":"gpt-4o-mini","usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`))
	}))
	defer server.Close()

	client := tolvynopenai.NewClient(tolvyn.ClientOptions{
		TolvynAPIKey: "tlv_live_test",
		ProxyURL:     server.URL + "/",
		Team:         "test-team",
		Service:      "test-service",
		Feature:      "test-feature",
		Agent:        "test-agent",
		User:         "test-user",
		EndCustomer:  "test-customer",
		FailOpen:     false,
	})

	// Make a request to trigger header capture.
	ctx := context.Background()
	//nolint:errcheck
	client.Chat.Completions.New(ctx, oai.ChatCompletionNewParams{
		Model: oai.F(oai.ChatModelGPT4oMini),
		Messages: oai.F([]oai.ChatCompletionMessageParamUnion{
			oai.ChatCompletionUserMessageParam{
				Role: oai.F(oai.ChatCompletionUserMessageParamRoleUser),
				Content: oai.F([]oai.ChatCompletionContentPartUnionParam{
					oai.ChatCompletionContentPartTextParam{
						Text: oai.F("test"),
						Type: oai.F(oai.ChatCompletionContentPartTextTypeText),
					},
				}),
			},
		}),
	}, option.WithMaxRetries(0))

	tests := []struct {
		header string
		want   string
	}{
		{"X-Tolvyn-Team", "test-team"},
		{"X-Tolvyn-Service", "test-service"},
		{"X-Tolvyn-Feature", "test-feature"},
		{"X-Tolvyn-Agent", "test-agent"},
		{"X-Tolvyn-User", "test-user"},
		{"X-Tolvyn-End-Customer", "test-customer"},
	}

	if capturedHeaders == nil {
		t.Fatal("no request received by test server")
	}

	for _, tt := range tests {
		if got := capturedHeaders.Get(tt.header); got != tt.want {
			t.Errorf("header %s = %q, want %q", tt.header, got, tt.want)
		}
	}
}

func TestAuthorizationHeader(t *testing.T) {
	var capturedAuth string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		capturedAuth = r.Header.Get("Authorization")
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(200)
		//nolint:errcheck
		w.Write([]byte(`{"id":"chatcmpl-test","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}],"model":"gpt-4o-mini","usage":{"prompt_tokens":5,"completion_tokens":2,"total_tokens":7}}`))
	}))
	defer server.Close()

	client := tolvynopenai.NewClient(tolvyn.ClientOptions{
		TolvynAPIKey: "tlv_live_testkey123",
		ProxyURL:     server.URL + "/",
		FailOpen:     false,
	})

	ctx := context.Background()
	//nolint:errcheck
	client.Chat.Completions.New(ctx, oai.ChatCompletionNewParams{
		Model: oai.F(oai.ChatModelGPT4oMini),
		Messages: oai.F([]oai.ChatCompletionMessageParamUnion{
			oai.ChatCompletionUserMessageParam{
				Role: oai.F(oai.ChatCompletionUserMessageParamRoleUser),
				Content: oai.F([]oai.ChatCompletionContentPartUnionParam{
					oai.ChatCompletionContentPartTextParam{
						Text: oai.F("test"),
						Type: oai.F(oai.ChatCompletionContentPartTextTypeText),
					},
				}),
			},
		}),
	}, option.WithMaxRetries(0))

	if capturedAuth != "Bearer tlv_live_testkey123" {
		t.Errorf("Authorization = %q, want %q", capturedAuth, "Bearer tlv_live_testkey123")
	}
}

func TestFailOpenEnvVar(t *testing.T) {
	t.Setenv("TOLVYN_API_KEY", "tlv_live_test")
	t.Setenv("OPENAI_API_KEY", "sk-test")
	client := tolvynopenai.NewClientFromEnv()
	if client == nil {
		t.Fatal("NewClientFromEnv returned nil")
	}
}
