package opencodeauth

import (
	"context"
	"errors"
	"net/http"
	"net/url"
	"testing"
)

func TestSessionSelectionAndContextPrecedence(t *testing.T) {
	client, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1", SessionID: "configured"})
	if err != nil {
		t.Fatal(err)
	}
	if got, err := client.sessionForContext(context.Background()); err != nil || got != "configured" {
		t.Fatalf("configured session = %q, %v", got, err)
	}
	if got, err := client.sessionForContext(WithSessionID(context.Background(), "per-request")); err != nil || got != "per-request" {
		t.Fatalf("context session = %q, %v", got, err)
	}
	if _, err := client.sessionForContext(WithSessionID(context.Background(), "")); !errors.Is(err, ErrMissingSessionID) {
		t.Fatalf("empty context session error = %v", err)
	}
	if _, err := client.sessionForContext(WithSessionID(context.Background(), "bad session")); !errors.Is(err, ErrInvalidSessionID) {
		t.Fatalf("invalid context session error = %v", err)
	}

	withoutConfigured, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := withoutConfigured.sessionForContext(context.Background()); !errors.Is(err, ErrMissingSessionID) {
		t.Fatalf("missing configured session error = %v", err)
	}
	if got, err := withoutConfigured.sessionForContext(WithSessionID(context.Background(), "conversation-2")); err != nil || got != "conversation-2" {
		t.Fatalf("context-only session = %q, %v", got, err)
	}
}

func TestSessionContextsAreIsolatedAndCatalogDoesNotNeedSession(t *testing.T) {
	client, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1"})
	if err != nil {
		t.Fatal(err)
	}
	first := WithSessionID(context.Background(), "first")
	second := WithSessionID(context.Background(), "second")
	gotFirst, err := client.sessionForContext(first)
	if err != nil || gotFirst != "first" {
		t.Fatalf("first session = %q, %v", gotFirst, err)
	}
	gotSecond, err := client.sessionForContext(second)
	if err != nil || gotSecond != "second" {
		t.Fatalf("second session = %q, %v", gotSecond, err)
	}
	req := &http.Request{Method: http.MethodGet, URL: mustURL(t, client.base.endpoint("/models"))}
	if route, err := client.validateRequest(req); err != nil || route != routeModels {
		t.Fatalf("models validation = %v, %v", route, err)
	}
}

func mustURL(t *testing.T, raw string) *url.URL {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("url.Parse(%q) error = %v", raw, err)
	}
	return u
}
