package opencodeauth

import (
	"net/http"
	"strings"
)

// transport authenticates only the native OpenCode routes accepted by Client.
// It deliberately leaves request and response bodies to the underlying
// RoundTripper and to the caller, respectively.
type transport struct {
	client *Client
	next   http.RoundTripper
}

func (t *transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req == nil {
		return nil, ErrDisallowedRequest
	}
	if err := req.Context().Err(); err != nil {
		return nil, rejectRequest(req, err)
	}
	route, err := t.client.validateRequest(req)
	if err != nil {
		return nil, rejectRequest(req, err)
	}

	var session string
	if route != routeModels {
		session, err = t.client.sessionForContext(req.Context())
		if err != nil {
			return nil, rejectRequest(req, err)
		}
	}

	decorated := req.Clone(req.Context())
	decorated.Host = decorated.URL.Host
	if decorated.Header == nil {
		decorated.Header = make(http.Header)
	}
	removeOwnedHeader(decorated.Header, "Authorization")
	removeOwnedHeader(decorated.Header, "x-api-key")
	removeOwnedHeader(decorated.Header, "User-Agent")
	removeOwnedHeader(decorated.Header, "x-opencode-session")

	setOwnedHeader := func(name, value string) {
		decorated.Header.Set(name, value)
	}
	switch route {
	case routeModels:
		// Catalog requests intentionally carry no credentials or session.
		setOwnedHeader("User-Agent", t.client.userAgent)
	case routeChat, routeResponses:
		setOwnedHeader("Authorization", "Bearer "+t.client.apiKey)
		setOwnedHeader("User-Agent", t.client.userAgent)
		setOwnedHeader("x-opencode-session", session)
	case routeMessages:
		setOwnedHeader("x-api-key", t.client.apiKey)
		setOwnedHeader("User-Agent", t.client.userAgent)
		setOwnedHeader("x-opencode-session", session)
		version, err := anthropicVersion(decorated.Header)
		if err != nil {
			return nil, rejectRequest(req, err)
		}
		removeOwnedHeader(decorated.Header, "anthropic-version")
		setOwnedHeader("anthropic-version", version)
	}

	return t.next.RoundTrip(decorated)
}

func (t *transport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

func rejectRequest(req *http.Request, err error) error {
	if req != nil && req.Body != nil {
		_ = req.Body.Close()
	}
	return err
}

func removeOwnedHeader(header http.Header, name string) {
	for key := range header {
		if strings.EqualFold(key, name) {
			delete(header, key)
		}
	}
}

func anthropicVersion(header http.Header) (string, error) {
	var values []string
	for key, entries := range header {
		if !strings.EqualFold(key, "anthropic-version") {
			continue
		}
		for _, value := range entries {
			if containsHeaderControl(value) {
				return "", ErrDisallowedRequest
			}
			values = append(values, value)
		}
	}
	if len(values) == 0 {
		return "2023-06-01", nil
	}
	first := values[0]
	for _, value := range values[1:] {
		if value != first {
			return "", ErrDisallowedRequest
		}
	}
	if first == "" {
		return "2023-06-01", nil
	}
	return first, nil
}

func containsHeaderControl(value string) bool {
	for i := 0; i < len(value); i++ {
		if isHeaderControl(value[i]) {
			return true
		}
	}
	return false
}

var _ http.RoundTripper = (*transport)(nil)
