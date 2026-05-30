package tolvyn

import (
	"net/http"
	"testing"
)

// GO-08 / GO-09: the direct fallback must use the provider's own auth header
// with the PROVIDER key, and must never leak the TOLVYN key that the SDK placed
// in X-Api-Key / X-Goog-Api-Key.

func TestApplyFallbackAuth_OpenAI(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer tlv_live_secret")
	h.Set("Content-Type", "application/json")
	applyFallbackAuth(h, "openai", "sk-openai-fallback")
	if got := h.Get("Authorization"); got != "Bearer sk-openai-fallback" {
		t.Fatalf("Authorization = %q, want Bearer sk-openai-fallback", got)
	}
	if h.Get("X-Api-Key") != "" || h.Get("X-Goog-Api-Key") != "" {
		t.Fatalf("unexpected key headers present: %v", h)
	}
	if h.Get("Content-Type") != "application/json" {
		t.Fatalf("non-auth header not preserved")
	}
}

func TestApplyFallbackAuth_AnthropicNoLeak(t *testing.T) {
	h := http.Header{}
	h.Set("X-Api-Key", "tlv_live_secret") // TOLVYN key, set by the Anthropic SDK
	h.Set("Anthropic-Version", "2023-06-01")
	applyFallbackAuth(h, "anthropic", "sk-ant-fallback")
	if got := h.Get("X-Api-Key"); got != "sk-ant-fallback" {
		t.Fatalf("X-Api-Key = %q, want provider key (no TOLVYN leak)", got)
	}
	if h.Get("Authorization") != "" {
		t.Fatalf("Authorization (Bearer) must be absent for Anthropic")
	}
	if h.Get("Anthropic-Version") != "2023-06-01" {
		t.Fatalf("required anthropic-version header was dropped")
	}
}

func TestApplyFallbackAuth_GoogleNoLeak(t *testing.T) {
	h := http.Header{}
	h.Set("X-Goog-Api-Key", "tlv_live_secret")
	applyFallbackAuth(h, "google", "goog-fallback")
	if got := h.Get("X-Goog-Api-Key"); got != "goog-fallback" {
		t.Fatalf("X-Goog-Api-Key = %q, want provider key", got)
	}
	if h.Get("Authorization") != "" || h.Get("X-Api-Key") != "" {
		t.Fatalf("unexpected auth headers present: %v", h)
	}
}

func TestApplyFallbackAuth_StripsAllInbound(t *testing.T) {
	h := http.Header{}
	h.Set("Authorization", "Bearer tlv")
	h.Set("X-Api-Key", "tlv")
	h.Set("X-Goog-Api-Key", "tlv")
	applyFallbackAuth(h, "anthropic", "sk-ant-fallback")
	if h.Get("X-Api-Key") != "sk-ant-fallback" {
		t.Fatalf("X-Api-Key = %q", h.Get("X-Api-Key"))
	}
	if h.Get("Authorization") != "" || h.Get("X-Goog-Api-Key") != "" {
		t.Fatalf("other auth headers not stripped: %v", h)
	}
}

func TestApplyFallbackAuth_UnknownProviderDefaultsBearer(t *testing.T) {
	h := http.Header{}
	applyFallbackAuth(h, "mystery", "key123")
	if h.Get("Authorization") != "Bearer key123" {
		t.Fatalf("unknown provider should default to Bearer, got %v", h)
	}
}
