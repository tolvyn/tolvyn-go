# tolvyn-go

Go SDK for [TOLVYN](https://tolvyn.io) — the control and evidence layer for AI spend.

Drop-in replacement for OpenAI, Anthropic, and Google Go SDKs. Routes through TOLVYN for cost tracking, budget enforcement, and audit trails. Fails open automatically if TOLVYN is unreachable.

[![Go Reference](https://pkg.go.dev/badge/github.com/tolvyn/tolvyn-go.svg)](https://pkg.go.dev/github.com/tolvyn/tolvyn-go)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

## Install

```bash
go get github.com/tolvyn/tolvyn-go/openai
go get github.com/tolvyn/tolvyn-go/anthropic
go get github.com/tolvyn/tolvyn-go/google
go get github.com/tolvyn/tolvyn-go/deepseek
```

## Quick start

```go
import (
    tolvyn "github.com/tolvyn/tolvyn-go"
    tolvynopenai "github.com/tolvyn/tolvyn-go/openai"
    oai "github.com/openai/openai-go"
)

client := tolvynopenai.NewClient(tolvyn.ClientOptions{
    TolvynAPIKey:   "tlv_live_...",
    ProviderAPIKey: "sk-...",     // for fail-open fallback (enabled by default)
    Team:           "engineering",
    Service:        "chatbot-api",
})

// Use exactly like the official openai-go SDK
resp, err := client.Chat.Completions.New(ctx, oai.ChatCompletionNewParams{
    Model:    oai.F(oai.ChatModelGPT4o),
    Messages: oai.F([]oai.ChatCompletionMessageParamUnion{
        oai.ChatCompletionUserMessageParam{
            Role:    oai.F(oai.ChatCompletionUserMessageParamRoleUser),
            Content: oai.F([]oai.ChatCompletionContentPartUnionParam{
                oai.ChatCompletionContentPartTextParam{
                    Text: oai.F("Hello"),
                    Type: oai.F(oai.ChatCompletionContentPartTextTypeText),
                },
            }),
        },
    }),
})
```

## Environment variables

| Variable | Required | Description |
|----------|----------|-------------|
| `TOLVYN_API_KEY` | Yes | Your TOLVYN API key (`tlv_live_...`) |
| `OPENAI_API_KEY` | For fail-open | OpenAI key used when TOLVYN is unreachable |
| `ANTHROPIC_API_KEY` | For fail-open | Anthropic key used when TOLVYN is unreachable |
| `GOOGLE_API_KEY` | For fail-open | Google key used when TOLVYN is unreachable |
| `DEEPSEEK_API_KEY` | For fail-open | DeepSeek key used when TOLVYN is unreachable |
| `TOLVYN_PROXY_URL` | No | Override the proxy URL. It must start with `https://`; `http://` is accepted only for `localhost`, `127.0.0.1` and `[::1]`. A host other than `proxy.tolvyn.io` logs a one-time warning, because your TOLVYN key and your prompts are sent to it. Default: `https://proxy.tolvyn.io/v1/proxy/{provider}/`. |

## Attribution headers

Set these to get per-team, per-service, per-user breakdowns in the TOLVYN dashboard:

```go
tolvyn.ClientOptions{
    Team:        "engineering",     // X-Tolvyn-Team
    Service:     "chatbot-api",     // X-Tolvyn-Service
    Feature:     "summarization",   // X-Tolvyn-Feature
    Agent:       "claude-code",     // X-Tolvyn-Agent
    User:        "alice@co.com",    // X-Tolvyn-User
    EndCustomer: "acme-corp",       // X-Tolvyn-End-Customer
}
```

## Fail-Open Behavior

By default, the Go SDK fails open: if TOLVYN's proxy is unreachable, requests automatically retry against the provider directly. This matches the Python and Node SDK defaults.

Set `ProviderAPIKey` to enable fail-open fallback:

```go
client := openai.NewClient(tolvyn.ClientOptions{
    TolvynAPIKey:   "tlv_live_...",
    ProviderAPIKey: "sk-...",    // required — used as fallback when TOLVYN is unreachable
})
```

To disable fail-open and force all requests through TOLVYN (hard failure on TOLVYN outages):

```go
client := openai.NewClient(tolvyn.ClientOptions{
    TolvynAPIKey:    "tlv_live_...",
    DisableFailOpen: true,
})
```

Fail-open triggers only when the request never reached TOLVYN: connection refused,
DNS failure, a timeout while connecting, or a failed TLS handshake — or on an HTTP 503
carrying `X-Tolvyn-Unavailable: 1`.

Fail-open does NOT trigger on: 4xx errors (auth failures, budget and rate limits, kill
switch, bad requests), a plain 503 (the provider's own, passed through), or a timeout
or reset after the request was sent — TOLVYN may already have forwarded it, and a
direct retry would be a second, unmetered provider call. A retry made by the provider SDK itself does not go direct
either, since an earlier attempt may have been sent: only a request's first attempt fails open.

The direct request never carries `X-Tolvyn-*` headers, whether set on the client or
per request.

The SDK does not follow redirects from the proxy URL: a redirect is returned as an error, and TOLVYN headers go only to the proxy's host, so your keys are not sent
to another address. A base URL you pass per request (`option.WithBaseURL`) is used as given: the proxy URL rule above
applies to the client's proxy URL, not to it.

## Providers

| Provider | Package | Official SDK wrapped |
|----------|---------|---------------------|
| OpenAI | `github.com/tolvyn/tolvyn-go/openai` | `github.com/openai/openai-go` |
| Anthropic | `github.com/tolvyn/tolvyn-go/anthropic` | `github.com/anthropics/anthropic-sdk-go` |
| Google | `github.com/tolvyn/tolvyn-go/google` | `github.com/google/generative-ai-go` |
| DeepSeek | `github.com/tolvyn/tolvyn-go/deepseek` | `github.com/openai/openai-go` (OpenAI-compatible) |

## License

Apache 2.0
