package deepseek_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	oai "github.com/openai/openai-go"
	tolvyn "github.com/tolvyn/tolvyn-go"
	tolvyndeepseek "github.com/tolvyn/tolvyn-go/deepseek"
)

// Requests go to <proxy>/chat/completions with the TOLVYN key and attribution.
func TestRoutesThroughProxy(t *testing.T) {
	var path, auth, team string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path, auth, team = r.URL.Path, r.Header.Get("Authorization"), r.Header.Get("X-Tolvyn-Team")
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"id":"x","object":"chat.completion","choices":[{"message":{"role":"assistant","content":"ok"},"finish_reason":"stop","index":0}],"model":"deepseek-chat"}`)) //nolint:errcheck
	}))
	defer server.Close()

	client := tolvyndeepseek.NewClient(tolvyn.ClientOptions{
		TolvynAPIKey:    "tlv_live_test",
		ProxyURL:        server.URL + "/v1/proxy/deepseek/",
		Team:            "t-team",
		DisableFailOpen: true,
	})
	_, err := client.Chat.Completions.New(context.Background(), oai.ChatCompletionNewParams{
		Model:    oai.F(oai.ChatModel("deepseek-chat")),
		Messages: oai.F([]oai.ChatCompletionMessageParamUnion{oai.UserMessage("hi")}),
	})
	if err != nil {
		t.Fatal(err)
	}
	if path != "/v1/proxy/deepseek/chat/completions" || auth != "Bearer tlv_live_test" || team != "t-team" {
		t.Fatalf("path=%q auth=%q team=%q", path, auth, team)
	}
}
