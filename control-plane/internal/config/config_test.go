package config

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPBaseURL_PublicBaseURL(t *testing.T) {
	cfg := &Config{PublicBaseURL: "https://anchor-api.onrender.com"}
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	got := cfg.HTTPBaseURL(req)
	if got != "https://anchor-api.onrender.com" {
		t.Fatalf("HTTPBaseURL = %q", got)
	}
}

func TestHTTPBaseURL_XForwardedProto(t *testing.T) {
	cfg := &Config{}
	req := httptest.NewRequest(http.MethodGet, "http://localhost:8080/", nil)
	req.Host = "anchor-api.onrender.com"
	req.Header.Set("X-Forwarded-Proto", "https")
	got := cfg.HTTPBaseURL(req)
	want := "https://anchor-api.onrender.com"
	if got != want {
		t.Fatalf("HTTPBaseURL = %q, want %q", got, want)
	}
}

func TestAgentWebSocketURL_PublicBaseURL(t *testing.T) {
	cfg := &Config{
		PublicBaseURL: "https://anchor-api.onrender.com",
		BaseDomain:    "example.com", // must be ignored when PublicBaseURL set
		WSPath:        "/ws/agent",
	}
	got := cfg.AgentWebSocketURL(nil)
	want := "wss://anchor-api.onrender.com/ws/agent"
	if got != want {
		t.Fatalf("AgentWebSocketURL = %q, want %q", got, want)
	}
}

func TestAgentWebSocketURL_BaseDomainFallback(t *testing.T) {
	cfg := &Config{BaseDomain: "example.com"}
	req := httptest.NewRequest(http.MethodGet, "http://localhost/", nil)
	got := cfg.AgentWebSocketURL(req)
	want := "ws://ws.example.com/ws/agent"
	if got != want {
		t.Fatalf("AgentWebSocketURL = %q, want %q", got, want)
	}
}
