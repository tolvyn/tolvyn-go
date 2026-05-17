# tolvyn-go

Go SDK for [TOLVYN](https://tolvyn.io) — the financial control plane for AI infrastructure.

Drop-in replacement for OpenAI, Anthropic, and Google Go SDKs. Routes through TOLVYN for cost tracking, budget enforcement, and audit trails. Fails open automatically if TOLVYN is unreachable.

[![Go Reference](https://pkg.go.dev/badge/github.com/tolvyn/tolvyn-go.svg)](https://pkg.go.dev/github.com/tolvyn/tolvyn-go)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)

## Install

```bash
go get github.com/tolvyn/tolvyn-go/openai
go get github.com/tolvyn/tolvyn-go/anthropic
go get github.com/tolvyn/tolvyn-go/google
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
    ProviderAPIKey: "sk-...",     // for fail-open fallback
    Team:           "engineering",
    Service:        "chatbot-api",
    FailOpen:       true,
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
| `TOLVYN_PROXY_URL` | No | Override proxy URL (default: https://proxy.tolvyn.io/v1/proxy/{provider}/) |

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

## Fail-open

If TOLVYN is unreachable, requests automatically retry directly against the provider.
Set `ProviderAPIKey` and `FailOpen: true` to enable:

```go
tolvyn.ClientOptions{
    TolvynAPIKey:   "tlv_live_...",
    ProviderAPIKey: "sk-...",   // required for fail-open
    FailOpen:       true,       // must be set explicitly — zero value is false
}
```

**Important:** Unlike the Python and Node.js SDKs, `FailOpen` defaults to `false` in Go
(the zero value of `bool`). You must set `FailOpen: true` explicitly, or use
`NewClientFromEnv()` which sets it for you.

Fail-open triggers on: connection refused, timeout, DNS failure, HTTP 503.
Fail-open does NOT trigger on: 4xx errors (auth failures, rate limits, bad requests).

## Providers

| Provider | Package | Official SDK wrapped |
|----------|---------|---------------------|
| OpenAI | `github.com/tolvyn/tolvyn-go/openai` | `github.com/openai/openai-go` |
| Anthropic | `github.com/tolvyn/tolvyn-go/anthropic` | `github.com/anthropics/anthropic-sdk-go` |
| Google | `github.com/tolvyn/tolvyn-go/google` | `github.com/google/generative-ai-go` |

## License

Apache 2.0
