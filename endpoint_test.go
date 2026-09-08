package opencodeauth

import (
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestBaseURLNormalizationAndEndpoints(t *testing.T) {
	tests := []struct {
		name       string
		base       string
		wantBase   string
		wantModels string
	}{
		{name: "default", wantBase: DefaultBaseURL, wantModels: DefaultBaseURL + "/models"},
		{name: "trailing slash", base: "https://EXAMPLE.com/zen/go/v1///", wantBase: "https://example.com/zen/go/v1", wantModels: "https://example.com/zen/go/v1/models"},
		{name: "custom prefix", base: "http://127.0.0.1:8080/proxy/go/v1/", wantBase: "http://127.0.0.1:8080/proxy/go/v1", wantModels: "http://127.0.0.1:8080/proxy/go/v1/models"},
		{name: "ipv6 loopback", base: "http://[0:0:0:0:0:0:0:1]/", wantBase: "http://[::1]", wantModels: "http://[::1]/models"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1", BaseURL: tt.base})
			if err != nil {
				t.Fatalf("NewClient() error = %v", err)
			}
			if got := client.BaseURL(); got != tt.wantBase {
				t.Fatalf("BaseURL() = %q, want %q", got, tt.wantBase)
			}
			if got, err := client.Endpoint(ProtocolChatCompletions); err != nil || got != client.BaseURL()+"/chat/completions" {
				t.Fatalf("chat Endpoint() = %q, %v", got, err)
			}
			if got, err := client.Endpoint(ProtocolMessages); err != nil || got != client.BaseURL()+"/messages" {
				t.Fatalf("messages Endpoint() = %q, %v", got, err)
			}
			if got, err := client.Endpoint(ProtocolResponses); err != nil || got != client.BaseURL()+"/responses" {
				t.Fatalf("responses Endpoint() = %q, %v", got, err)
			}
			if got, err := client.Endpoint(Protocol("unknown")); !errors.Is(err, ErrInvalidConfiguration) || got != "" {
				t.Fatalf("unknown Endpoint() = %q, %v", got, err)
			}
			if got := client.base.endpoint("/models"); got != tt.wantModels {
				t.Fatalf("models URL = %q, want %q", got, tt.wantModels)
			}
		})
	}
}

func TestRejectInvalidBaseURLs(t *testing.T) {
	for _, raw := range []string{
		"http://example.com", // HTTP destinations must be explicitly local.
		"https://example.com?query=1",
		"https://example.com/#fragment",
		"https://user:pass@example.com",
		"https://example.com/proxy//v1",
		"https://example.com/proxy/%2e%2e/v1",
		"https://example.com/proxy/%2Fv1",
		"https://example.com.",
		"https://[fe80::1%25en0]/",
		"https://example.com:0",
		"https://example.com:65536",
		"https://example.com:bad",
		"https://[::1",
	} {
		t.Run(raw, func(t *testing.T) {
			if _, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1", BaseURL: raw}); !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("NewClient(%q) error = %v, want ErrInvalidConfiguration", raw, err)
			}
		})
	}
}

func TestValidateRequestOriginHostAndRoutes(t *testing.T) {
	client, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1", BaseURL: "https://Example.com:443/proxy/go/v1/"})
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name    string
		method  string
		rawURL  string
		host    string
		want    requestRoute
		wantErr bool
	}{
		{name: "models", method: http.MethodGet, rawURL: "https://EXAMPLE.com/proxy/go/v1/models?x=1", host: "example.com:443", want: routeModels},
		{name: "chat", method: http.MethodPost, rawURL: "https://example.com:443/proxy/go/v1/chat/completions", want: routeChat},
		{name: "messages", method: http.MethodPost, rawURL: "https://example.com/proxy/go/v1/messages", want: routeMessages},
		{name: "responses", method: http.MethodPost, rawURL: "https://example.com/proxy/go/v1/responses", want: routeResponses},
		{name: "wrong host", method: http.MethodPost, rawURL: "https://other.example.com/proxy/go/v1/messages", wantErr: true},
		{name: "wrong request host", method: http.MethodPost, rawURL: "https://example.com/proxy/go/v1/messages", host: "other.example.com", wantErr: true},
		{name: "trailing dot", method: http.MethodPost, rawURL: "https://example.com./proxy/go/v1/messages", wantErr: true},
		{name: "wrong method", method: http.MethodGet, rawURL: "https://example.com/proxy/go/v1/messages", wantErr: true},
		{name: "sibling path", method: http.MethodPost, rawURL: "https://example.com/proxy/go/v1/messages/extra", wantErr: true},
		{name: "encoded separator", method: http.MethodPost, rawURL: "https://example.com/proxy/go/v1/%2fmessages", wantErr: true},
		{name: "userinfo", method: http.MethodPost, rawURL: "https://user@example.com/proxy/go/v1/messages", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(tt.rawURL)
			if err != nil {
				t.Fatalf("url.Parse() error = %v", err)
			}
			req := &http.Request{Method: tt.method, URL: u, Host: tt.host}
			got, err := client.validateRequest(req)
			if tt.wantErr {
				if !errors.Is(err, ErrDisallowedRequest) {
					t.Fatalf("validateRequest() error = %v, want ErrDisallowedRequest", err)
				}
				return
			}
			if err != nil || got != tt.want {
				t.Fatalf("validateRequest() = %v, %v, want %v", got, err, tt.want)
			}
		})
	}
}

func TestValidateRequestRejectsOpaqueAndIPv6Zone(t *testing.T) {
	client, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1", BaseURL: "https://example.com"})
	if err != nil {
		t.Fatal(err)
	}
	for _, req := range []*http.Request{
		{Method: http.MethodGet, URL: &url.URL{Scheme: "https", Opaque: "example.com/models"}},
		{Method: http.MethodGet, URL: &url.URL{Scheme: "https", Host: "[fe80::1%25en0]", Path: "/models"}},
	} {
		if _, err := client.validateRequest(req); !errors.Is(err, ErrDisallowedRequest) {
			t.Fatalf("validateRequest() error = %v, want ErrDisallowedRequest", err)
		}
	}
}
