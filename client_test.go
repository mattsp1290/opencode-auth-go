package opencodeauth

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewClientConfigurationAndEnvironmentPrecedence(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "environment-key")

	client, err := NewClient(Options{UserAgent: "test-agent/1.0"})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	if client.apiKey != "environment-key" {
		t.Fatalf("environment key was not captured at construction")
	}

	client, err = NewClient(Options{APIKey: "explicit-key", UserAgent: "test-agent/1.0"})
	if err != nil {
		t.Fatalf("NewClient(explicit) error = %v", err)
	}
	if client.apiKey != "explicit-key" {
		t.Fatalf("explicit key did not take precedence")
	}
	t.Setenv("OPENCODE_GO_API_KEY", "changed-after-construction")
	if client.apiKey != "explicit-key" {
		t.Fatalf("client configuration changed after construction")
	}

	for name, options := range map[string]Options{
		"missing":        {UserAgent: "test-agent/1.0"},
		"leading space":  {APIKey: " key", UserAgent: "test-agent/1.0"},
		"trailing space": {APIKey: "key ", UserAgent: "test-agent/1.0"},
		"newline":        {APIKey: "key\nvalue", UserAgent: "test-agent/1.0"},
		"missing agent":  {APIKey: "key"},
		"unicode agent":  {APIKey: "key", UserAgent: "agent/é"},
		"long agent":     {APIKey: "key", UserAgent: string(make([]byte, 257))},
	} {
		t.Run(name, func(t *testing.T) {
			if name == "missing" {
				t.Setenv("OPENCODE_GO_API_KEY", "")
			}
			_, err := NewClient(options)
			if name == "missing" {
				if !errors.Is(err, ErrMissingAPIKey) {
					t.Fatalf("error = %v, want ErrMissingAPIKey", err)
				}
				return
			}
			if !errors.Is(err, ErrInvalidConfiguration) {
				t.Fatalf("error = %v, want ErrInvalidConfiguration", err)
			}
		})
	}
}

func TestNewClientCopiesHTTPClient(t *testing.T) {
	suppliedTransport := &recordingTransport{}
	source := &http.Client{Transport: suppliedTransport, Timeout: 17 * time.Second}
	client, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1", HTTPClient: source})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	first := client.HTTPClient()
	second := client.HTTPClient()
	if first == nil || second == nil || first == second {
		t.Fatalf("HTTPClient() did not return fresh client values")
	}
	firstTransport, ok := first.Transport.(*transport)
	if !ok || firstTransport.next != suppliedTransport {
		t.Fatalf("HTTPClient() transport = %#v, want authenticated wrapper around supplied transport", first.Transport)
	}
	secondTransport, ok := second.Transport.(*transport)
	if !ok || secondTransport.next != suppliedTransport || firstTransport != secondTransport {
		t.Fatalf("HTTPClient() did not reuse an immutable authenticated transport")
	}
	if first.Timeout != source.Timeout || second.Timeout != source.Timeout {
		t.Fatalf("HTTPClient() did not preserve the supplied timeout")
	}
	first.Timeout = time.Second
	if second.Timeout != source.Timeout || source.Timeout != 17*time.Second || source.Transport != suppliedTransport {
		t.Fatalf("mutating returned client changed the template")
	}

	defaultClient, err := NewClient(Options{APIKey: "key", UserAgent: "agent/1"})
	if err != nil {
		t.Fatalf("NewClient(default) error = %v", err)
	}
	if defaultClient.HTTPClient().Transport == nil {
		t.Fatal("default HTTP client has nil transport")
	}
}

func TestCredentialBearingValuesFormatSafely(t *testing.T) {
	const (
		apiKeyCanary  = "api-key-format-canary"
		sessionCanary = "session-format-canary"
	)
	options := Options{
		APIKey:    apiKeyCanary,
		UserAgent: "agent/1",
		SessionID: sessionCanary,
	}
	client, err := NewClient(options)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	values := map[string]any{
		"options value":   options,
		"options pointer": &options,
		"client value":    *client,
		"client pointer":  client,
	}
	for name, value := range values {
		for _, format := range []string{"%v", "%+v", "%#v"} {
			t.Run(name+" "+format, func(t *testing.T) {
				got := fmt.Sprintf(format, value)
				if strings.Contains(got, apiKeyCanary) || strings.Contains(got, sessionCanary) {
					t.Fatalf("formatted value exposed a credential: %q", got)
				}
				if !strings.Contains(got, "<redacted>") {
					t.Fatalf("formatted value = %q, want explicit redaction", got)
				}
			})
		}
	}
}

func TestNewClientConfinesCredentialLookupAndHasNoIO(t *testing.T) {
	t.Setenv("OPENCODE_GO_API_KEY", "")
	temporaryHome := t.TempDir()
	t.Setenv("HOME", temporaryHome)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(temporaryHome, "config"))
	profile := filepath.Join(temporaryHome, ".profile")
	profileContents := []byte("export OPENCODE_GO_API_KEY=profile-canary\n")
	if err := os.WriteFile(profile, profileContents, 0o600); err != nil {
		t.Fatal(err)
	}

	transportCalls := 0
	source := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		transportCalls++
		return nil, errors.New("unexpected network request")
	})}
	if _, err := NewClient(Options{UserAgent: "agent/1", HTTPClient: source}); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("NewClient() error = %v, want ErrMissingAPIKey", err)
	}
	if transportCalls != 0 {
		t.Fatalf("NewClient() made %d network requests", transportCalls)
	}
	gotProfile, err := os.ReadFile(profile)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotProfile) != string(profileContents) {
		t.Fatal("NewClient() modified the credential canary file")
	}
	entries, err := os.ReadDir(temporaryHome)
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) != 1 || entries[0].Name() != ".profile" {
		t.Fatalf("NewClient() wrote unexpected files under HOME: %#v", entries)
	}
}

type recordingTransport struct{}

func (*recordingTransport) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }
