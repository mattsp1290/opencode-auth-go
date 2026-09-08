package opencodeauth

import (
	"context"
	"net/http"
	"os"
	"strings"
)

// DefaultBaseURL is the OpenCode Go API root used when Options.BaseURL is
// empty.
const DefaultBaseURL = "https://opencode.ai/zen/go/v1"

// Options configures a Client.
//
// APIKey takes precedence over OPENCODE_GO_API_KEY when it is nonempty. The
// caller must supply UserAgent to identify its coding agent. SessionID is
// optional here and can instead be attached to each request context with
// WithSessionID.
//
// A supplied HTTPClient is copied at construction. Its Transport is a trusted
// component: it can observe credentials and is responsible for its own proxy,
// logging, and routing policy. The returned HTTPClient has its own client
// value, but callers must follow net/http's normal rule against mutating a
// client while it is in use. The copied client's redirect policy always
// returns http.ErrUseLastResponse; a supplied overall Timeout is preserved
// and therefore also caps response-body streaming.
type Options struct {
	APIKey     string
	BaseURL    string
	UserAgent  string
	SessionID  string
	HTTPClient *http.Client
}

// Client holds immutable local configuration for OpenCode Go requests.
// Credentials and session identifiers are intentionally private.
type Client struct {
	apiKey       string
	baseURL      string
	base         canonicalURL
	userAgent    string
	sessionID    string
	httpTemplate http.Client
}

// NewClient validates options and performs no network or filesystem I/O.
func NewClient(options Options) (*Client, error) {
	apiKey := options.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("OPENCODE_GO_API_KEY")
	}
	if apiKey == "" {
		return nil, ErrMissingAPIKey
	}
	if !validAPIKey(apiKey) {
		return nil, ErrInvalidConfiguration
	}
	if !validUserAgent(options.UserAgent) {
		return nil, ErrInvalidConfiguration
	}
	base, err := parseBaseURL(options.BaseURL)
	if err != nil {
		return nil, err
	}
	if options.SessionID != "" && !validSessionID(options.SessionID) {
		return nil, ErrInvalidSessionID
	}

	template, err := cloneHTTPClient(options.HTTPClient)
	if err != nil {
		return nil, err
	}
	client := &Client{
		apiKey:       apiKey,
		baseURL:      base.String(),
		base:         base,
		userAgent:    options.UserAgent,
		sessionID:    options.SessionID,
		httpTemplate: template,
	}
	client.httpTemplate.Transport = &transport{client: client, next: template.Transport}
	client.httpTemplate.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return client, nil
}

// HTTPClient returns a new ordinary net/http client configured from the
// immutable client template. The authenticated request transport is layered by
// the package transport implementation; callers own response bodies.
func (c *Client) HTTPClient() *http.Client {
	if c == nil {
		return nil
	}
	copy := c.httpTemplate
	return &copy
}

// BaseURL returns the normalized API root used by the client.
func (c *Client) BaseURL() string {
	if c == nil {
		return ""
	}
	return c.baseURL
}

// Protocol identifies one of OpenCode Go's native inference request formats.
type Protocol string

const (
	ProtocolChatCompletions Protocol = "chat-completions"
	ProtocolMessages        Protocol = "messages"
	ProtocolResponses       Protocol = "responses"
)

// Endpoint returns the URL for a native inference protocol.
func (c *Client) Endpoint(protocol Protocol) (string, error) {
	if c == nil {
		return "", ErrInvalidConfiguration
	}
	suffix, ok := protocolSuffix(protocol)
	if !ok {
		return "", ErrInvalidConfiguration
	}
	return c.base.endpoint(suffix), nil
}

func protocolSuffix(protocol Protocol) (string, bool) {
	switch protocol {
	case ProtocolChatCompletions:
		return "/chat/completions", true
	case ProtocolMessages:
		return "/messages", true
	case ProtocolResponses:
		return "/responses", true
	default:
		return "", false
	}
}

func cloneHTTPClient(source *http.Client) (http.Client, error) {
	if source == nil {
		return http.Client{Transport: cloneDefaultTransport()}, nil
	}
	copy := *source
	if copy.Transport == nil {
		copy.Transport = cloneDefaultTransport()
	}
	if _, ok := copy.Transport.(*transport); ok {
		return http.Client{}, ErrInvalidConfiguration
	}
	return copy, nil
}

func cloneDefaultTransport() http.RoundTripper {
	if defaultTransport, ok := http.DefaultTransport.(*http.Transport); ok {
		return defaultTransport.Clone()
	}
	return http.DefaultTransport
}

func validAPIKey(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	for i := 0; i < len(value); i++ {
		if isHeaderControl(value[i]) {
			return false
		}
	}
	return true
}

func validUserAgent(value string) bool {
	if value == "" || strings.TrimSpace(value) == "" || len(value) > 256 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] < 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func validSessionID(value string) bool {
	if value == "" || len(value) > 256 {
		return false
	}
	for i := 0; i < len(value); i++ {
		if value[i] <= 0x20 || value[i] > 0x7e {
			return false
		}
	}
	return true
}

func isHeaderControl(value byte) bool {
	return value < 0x20 || value == 0x7f
}

func (c *Client) sessionForContext(ctx context.Context) (string, error) {
	if ctx != nil {
		if value, ok := sessionIDFromContext(ctx); ok {
			if value == "" {
				return "", ErrMissingSessionID
			}
			if !validSessionID(value) {
				return "", ErrInvalidSessionID
			}
			return value, nil
		}
	}
	if c.sessionID == "" {
		return "", ErrMissingSessionID
	}
	return c.sessionID, nil
}
