# Changelog

## v0.1.5 — 2026-10-26

### Fail-open: changed behaviour

- **Fail-open now fires only when TOLVYN is unreachable**: the dial failed
  (refused, unreachable, timed out while connecting), DNS failed, or the TLS
  handshake failed (`tolvyn.ProxyNotReached`) — or TOLVYN answered 503 with
  `X-Tolvyn-Unavailable: 1` (`tolvyn.IsTolvynUnavailable`).
- **It no longer fires on a provider's 503.** A plain 503 through the proxy is the
  provider's own and is returned to you; retrying it direct only moved the call out
  of TOLVYN's metering.
- **It no longer fires on timeouts, resets or EOF after the request was sent.**
  TOLVYN may already have forwarded the request, and the direct retry would be a
  second, unmetered provider call.
- Enforcement responses (kill switch 451, budget/quota 429) never fail open.
- `IsProxyError` and `ShouldNotFailOpen` remain exported but are deprecated; the
  transport no longer uses them.

### Fail-open: fixes

- Fail-open worked only when the connection was refused: on every other trigger
  the retry reused a body the first attempt had consumed and failed with
  `ContentLength=N with Body length 0`. The body is now replayed. (BE-508)
- The direct request no longer carries any `X-Tolvyn-*` header. Client-level
  headers were already removed; **per-request headers were not** (`X-Tolvyn-User`
  and `X-Tolvyn-End-Customer` can be personal data). Reported by Maram Sai Harsha
  Vardhan Reddy. (BE-507)
- The direct request now sends the provider's Host, not the proxy's, and keeps the
  query string. (BE-511)
- The discarded 503 response is drained and closed before the retry. (BE-514)

### Added

- `deepseek` package (`github.com/tolvyn/tolvyn-go/deepseek`, OpenAI-compatible,
  `DEEPSEEK_API_KEY` for fail-open). DeepSeek fail-open previously went to
  `/v1/v1/proxy/deepseek/...`. (BE-512)
