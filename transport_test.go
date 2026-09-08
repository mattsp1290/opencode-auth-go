package opencodeauth

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTransportDecoratesNativeRoutesAndPreservesRequests(t *testing.T) {
	type received struct {
		method string
		path   string
		header http.Header
		body   []byte
	}
	var mu sync.Mutex
	var calls []received
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll(request body): %v", err)
		}
		mu.Lock()
		calls = append(calls, received{method: r.Method, path: r.URL.RequestURI(), header: r.Header.Clone(), body: body})
		mu.Unlock()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()

	client, err := NewClient(Options{APIKey: "secret", BaseURL: server.URL + "/proxy/v1", UserAgent: "agent/1", SessionID: "configured"})
	if err != nil {
		t.Fatal(err)
	}
	hc := client.HTTPClient()
	tests := []struct {
		name       string
		method     string
		suffix     string
		body       string
		session    string
		wantAuth   string
		wantAPIKey string
		wantVer    string
	}{
		{name: "chat", method: http.MethodPost, suffix: "/chat/completions?stream=true", body: `{"model":"bare-model"}`, session: "chat-session", wantAuth: "Bearer secret"},
		{name: "responses", method: http.MethodPost, suffix: "/responses?x=1", body: `{"model":"qualified:model"}`, session: "response-session", wantAuth: "Bearer secret"},
		{name: "messages default version", method: http.MethodPost, suffix: "/messages", body: `{"model":"model"}`, session: "message-session", wantAPIKey: "secret", wantVer: "2023-06-01"},
		{name: "models", method: http.MethodGet, suffix: "/models?limit=2", wantVer: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			u, err := url.Parse(client.BaseURL() + tt.suffix)
			if err != nil {
				t.Fatal(err)
			}
			req := &http.Request{Method: tt.method, URL: u, Header: http.Header{
				"authorization":      {"caller-auth"},
				"X-API-KEY":          {"caller-key"},
				"uSeR-aGeNt":         {"caller-agent"},
				"X-OpenCode-Session": {"caller-session"},
				"Accept":             {"application/json"},
				"Content-Type":       {"application/json"},
				"Anthropic-Beta":     {"beta-feature"},
			}, Body: io.NopCloser(strings.NewReader(tt.body))}
			if tt.session != "" {
				req = req.WithContext(WithSessionID(req.Context(), tt.session))
			}
			beforeURL := *req.URL
			beforeHeader := req.Header.Clone()
			if _, err := hc.Do(req); err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			if *req.URL != beforeURL {
				t.Fatalf("request URL changed: got %#v, want %#v", req.URL, &beforeURL)
			}
			if !headersEqual(req.Header, beforeHeader) {
				t.Fatalf("request headers changed: got %#v, want %#v", req.Header, beforeHeader)
			}
		})
	}

	mu.Lock()
	defer mu.Unlock()
	if len(calls) != len(tests) {
		t.Fatalf("server received %d requests, want %d", len(calls), len(tests))
	}
	for i, call := range calls {
		tt := tests[i]
		if call.method != tt.method || call.path != "/proxy/v1"+tt.suffix {
			t.Errorf("call %d = %s %s, want %s %s", i, call.method, call.path, tt.method, "/proxy/v1"+tt.suffix)
		}
		if got := call.header.Get("User-Agent"); got != "agent/1" {
			t.Errorf("call %d User-Agent = %q", i, got)
		}
		if got := call.header.Get("Accept"); got != "application/json" {
			t.Errorf("call %d Accept = %q", i, got)
		}
		if got := call.header.Get("Anthropic-Beta"); got != "beta-feature" {
			t.Errorf("call %d Anthropic-Beta = %q", i, got)
		}
		if got := call.header.Get("Authorization"); got != tt.wantAuth {
			t.Errorf("call %d Authorization = %q, want %q", i, got, tt.wantAuth)
		}
		if got := call.header.Get("X-Api-Key"); got != tt.wantAPIKey {
			t.Errorf("call %d x-api-key = %q, want %q", i, got, tt.wantAPIKey)
		}
		if got := call.header.Get("X-Opencode-Session"); got != tt.session {
			t.Errorf("call %d session = %q, want %q", i, got, tt.session)
		}
		if got := call.header.Get("Anthropic-Version"); got != tt.wantVer {
			t.Errorf("call %d anthropic-version = %q, want %q", i, got, tt.wantVer)
		}
		if !bytes.Equal(call.body, []byte(tt.body)) {
			t.Errorf("call %d body = %q, want %q", i, call.body, tt.body)
		}
	}
}

func TestTransportPreservesOneAnthropicVersionAndRejectsConflicts(t *testing.T) {
	var got http.Header
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	client, err := NewClient(Options{APIKey: "key", BaseURL: server.URL, UserAgent: "agent/1", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/messages", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header["aNtHrOpIc-VeRsIoN"] = []string{"2024-01-01"}
	req.Header["ANTHROPIC-VERSION"] = []string{"2024-01-01"}
	if _, err := client.HTTPClient().Do(req); err != nil {
		t.Fatal(err)
	}
	if values := got.Values("Anthropic-Version"); len(values) != 1 || values[0] != "2024-01-01" {
		t.Fatalf("anthropic-version = %#v", values)
	}

	for name, values := range map[string][]string{
		"conflicting": {"2024-01-01", "2024-02-01"},
		"control":     {"2024-01-01\nleak"},
	} {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/messages", strings.NewReader("body"))
			if err != nil {
				t.Fatal(err)
			}
			req.Header["Anthropic-Version"] = values
			if _, err := client.HTTPClient().Do(req); !errors.Is(err, ErrDisallowedRequest) {
				t.Fatalf("Do() error = %v, want ErrDisallowedRequest", err)
			}
		})
	}
}

func TestTransportRejectsRequestsWithoutSendingCredentialsAndClosesBody(t *testing.T) {
	called := 0
	client, err := NewClient(Options{APIKey: "key", BaseURL: "https://example.com", UserAgent: "agent/1", SessionID: "session", HTTPClient: &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		called++
		return nil, errors.New("unexpected transport call")
	})}})
	if err != nil {
		t.Fatal(err)
	}
	body := &closeTrackingBody{}
	req, err := http.NewRequest(http.MethodPost, "https://other.example.com/messages", body)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.HTTPClient().Do(req); !errors.Is(err, ErrDisallowedRequest) {
		t.Fatalf("Do() error = %v, want ErrDisallowedRequest", err)
	}
	if body.closed != 1 {
		t.Fatalf("body close count = %d, want 1", body.closed)
	}
	if called != 0 {
		t.Fatalf("underlying transport called %d times", called)
	}
}

func TestTransportCanonicalizesClonedHostWithoutMutatingCaller(t *testing.T) {
	var seenHost, seenURLHost string
	underlying := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seenHost = req.Host
		seenURLHost = req.URL.Host
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	client, err := NewClient(Options{APIKey: "key", BaseURL: "https://example.com", UserAgent: "agent/1", SessionID: "session", HTTPClient: &http.Client{Transport: underlying}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, "https://EXAMPLE.COM/responses", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	req.Host = "example.com:443"
	callerHost := req.Host
	response, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if seenHost != seenURLHost || seenHost != "EXAMPLE.COM" {
		t.Fatalf("underlying Host = %q, URL.Host = %q, want both %q", seenHost, seenURLHost, "EXAMPLE.COM")
	}
	if req.Host != callerHost {
		t.Fatalf("caller Request.Host changed to %q, want %q", req.Host, callerHost)
	}
}

func TestTransportPassesAnthropicVersionThroughNonMessagesRoutes(t *testing.T) {
	var seen http.Header
	underlying := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		seen = req.Header.Clone()
		return &http.Response{StatusCode: http.StatusNoContent, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(""))}, nil
	})
	client, err := NewClient(Options{APIKey: "key", BaseURL: "https://example.com", UserAgent: "agent/1", SessionID: "session", HTTPClient: &http.Client{Transport: underlying}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/responses", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	req.Header["aNtHrOpIc-VeRsIoN"] = []string{"caller-version", "caller-version"}
	response, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = response.Body.Close()
	if values := seen["aNtHrOpIc-VeRsIoN"]; len(values) != 2 || values[0] != "caller-version" || values[1] != "caller-version" {
		t.Fatalf("non-Messages anthropic-version = %#v, want caller values unchanged", values)
	}
}

func TestHTTPClientNeverFollowsRedirects(t *testing.T) {
	secondCalls := 0
	second := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		secondCalls++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer second.Close()
	first := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", second.URL+"/messages")
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = w.Write([]byte("redirect body"))
	}))
	defer first.Close()
	client, err := NewClient(Options{APIKey: "key", BaseURL: first.URL, UserAgent: "agent/1", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/messages", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusTemporaryRedirect)
	}
	if secondCalls != 0 {
		t.Fatalf("redirect target called %d times", secondCalls)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "redirect body" {
		t.Fatalf("redirect body = %q, %v", body, err)
	}
}

func TestHTTPClientBlocksSameOriginRedirectAndIgnoresCallerPolicy(t *testing.T) {
	var calls int
	var callerRedirectCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.URL.Query().Get("redirected") == "1" {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		w.Header().Set("Location", r.URL.Path+"?redirected=1")
		w.WriteHeader(http.StatusTemporaryRedirect)
		_, _ = io.WriteString(w, "same-origin redirect body")
	}))
	defer server.Close()
	callerPolicy := func(*http.Request, []*http.Request) error {
		callerRedirectCalls++
		return nil
	}
	client, err := NewClient(Options{APIKey: "key", BaseURL: server.URL, UserAgent: "agent/1", SessionID: "session", HTTPClient: &http.Client{CheckRedirect: callerPolicy}})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/messages", strings.NewReader("body"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusTemporaryRedirect {
		t.Fatalf("status = %d, want %d", response.StatusCode, http.StatusTemporaryRedirect)
	}
	if calls != 1 {
		t.Fatalf("same-origin redirect target was requested; handler calls = %d", calls)
	}
	if callerRedirectCalls != 0 {
		t.Fatalf("caller CheckRedirect invoked %d times", callerRedirectCalls)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil || string(body) != "same-origin redirect body" {
		t.Fatalf("redirect response body = %q, %v", body, err)
	}
}

func TestTransportConcurrentContextsKeepKeysAndSessionsTogether(t *testing.T) {
	arrived := make(chan struct{}, 2)
	release := make(chan struct{})
	var releaseOnce sync.Once
	releaseAll := func() {
		releaseOnce.Do(func() { close(release) })
	}
	t.Cleanup(releaseAll)
	underlying := &concurrentRecordingTransport{arrived: arrived, release: release}
	client, err := NewClient(Options{APIKey: "shared-key", BaseURL: "https://example.com", UserAgent: "agent/1", HTTPClient: &http.Client{Transport: underlying}})
	if err != nil {
		t.Fatal(err)
	}

	type result struct{ err error }
	results := make(chan result, 2)
	go func() {
		req, err := http.NewRequestWithContext(WithSessionID(context.Background(), "first-session"), http.MethodPost, client.BaseURL()+"/responses", strings.NewReader("first"))
		if err != nil {
			results <- result{err: err}
			return
		}
		response, err := client.HTTPClient().Do(req)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		results <- result{err: err}
	}()
	go func() {
		req, err := http.NewRequestWithContext(WithSessionID(context.Background(), "second-session"), http.MethodPost, client.BaseURL()+"/responses", strings.NewReader("second"))
		if err != nil {
			results <- result{err: err}
			return
		}
		response, err := client.HTTPClient().Do(req)
		if response != nil && response.Body != nil {
			_ = response.Body.Close()
		}
		results <- result{err: err}
	}()
	deadline := time.NewTimer(2 * time.Second)
	defer deadline.Stop()
	for range 2 {
		select {
		case <-arrived:
		case <-deadline.C:
			t.Fatal("timed out waiting for concurrent requests to reach the transport")
		}
	}
	releaseAll()
	for range 2 {
		select {
		case got := <-results:
			if got.err != nil {
				t.Fatal(got.err)
			}
		case <-deadline.C:
			t.Fatal("timed out waiting for concurrent request completion")
		}
	}

	underlying.mu.Lock()
	defer underlying.mu.Unlock()
	if len(underlying.requests) != 2 {
		t.Fatalf("recorded requests = %d, want 2", len(underlying.requests))
	}
	seen := make(map[string]bool)
	for _, req := range underlying.requests {
		key := req.Header.Get("Authorization")
		session := req.Header.Get("x-opencode-session")
		seen[key+"/"+session] = true
	}
	if !seen["Bearer shared-key/first-session"] || !seen["Bearer shared-key/second-session"] {
		t.Fatalf("credential/session pairs crossed: %#v", seen)
	}
}

func TestTransportDelegatesCloseIdleAndDoesNotReadBodies(t *testing.T) {
	underlying := &trackingRoundTripper{response: &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("response")),
	}}
	client, err := NewClient(Options{APIKey: "key", BaseURL: "https://example.com", UserAgent: "agent/1", SessionID: "session", HTTPClient: &http.Client{Transport: underlying}})
	if err != nil {
		t.Fatal(err)
	}
	hc := client.HTTPClient()
	hc.CloseIdleConnections()
	if underlying.closed != 1 {
		t.Fatalf("CloseIdleConnections calls = %d, want 1", underlying.closed)
	}

	requestBody := &readTrackingBody{}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/responses", requestBody)
	if err != nil {
		t.Fatal(err)
	}
	response, err := hc.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	if requestBody.reads != 0 {
		t.Fatalf("request body reads = %d, want 0", requestBody.reads)
	}
	if underlying.requests != 1 {
		t.Fatalf("underlying requests = %d, want 1", underlying.requests)
	}
	if response.Body == nil {
		t.Fatal("response body is nil")
	}
	if _, err := io.ReadAll(response.Body); err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestTransportPreservesRequestReplayMetadata(t *testing.T) {
	payload := []byte("opaque request bytes")
	contextKey := struct{}{}
	underlying := roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.ContentLength != int64(len(payload)) {
			t.Errorf("ContentLength = %d, want %d", req.ContentLength, len(payload))
		}
		if req.GetBody == nil {
			t.Error("GetBody was cleared")
		} else {
			replay, err := req.GetBody()
			if err != nil {
				t.Errorf("GetBody() error = %v", err)
			} else {
				defer replay.Close()
				got, readErr := io.ReadAll(replay)
				if readErr != nil || !bytes.Equal(got, payload) {
					t.Errorf("GetBody() = %q, %v", got, readErr)
				}
			}
		}
		if req.Context().Value(contextKey) != "preserved" {
			t.Error("request context value was not preserved")
		}
		if req.URL.RawQuery != "mode=exact%20bytes" {
			t.Errorf("RawQuery = %q", req.URL.RawQuery)
		}
		return &http.Response{
			StatusCode: http.StatusNoContent,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	})
	client, err := NewClient(Options{
		APIKey:     "key",
		BaseURL:    "https://example.com",
		UserAgent:  "agent/1",
		SessionID:  "session",
		HTTPClient: &http.Client{Transport: underlying},
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.WithValue(context.Background(), contextKey, "preserved")
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.BaseURL()+"/responses?mode=exact%20bytes", bytes.NewReader(payload))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

type trackingRoundTripper struct {
	response *http.Response
	requests int
	closed   int
}

func (t *trackingRoundTripper) RoundTrip(*http.Request) (*http.Response, error) {
	t.requests++
	return t.response, nil
}

func (t *trackingRoundTripper) CloseIdleConnections() { t.closed++ }

type concurrentRecordedRequest struct {
	Header http.Header
}

type concurrentRecordingTransport struct {
	mu       sync.Mutex
	arrived  chan<- struct{}
	release  <-chan struct{}
	requests []concurrentRecordedRequest
}

func (t *concurrentRecordingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	t.mu.Lock()
	t.requests = append(t.requests, concurrentRecordedRequest{Header: req.Header.Clone()})
	t.mu.Unlock()
	t.arrived <- struct{}{}
	<-t.release
	return &http.Response{
		StatusCode: http.StatusNoContent,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

type closeTrackingBody struct {
	closed int
}

func (*closeTrackingBody) Read([]byte) (int, error) { return 0, io.EOF }

func (b *closeTrackingBody) Close() error {
	b.closed++
	return nil
}

type readTrackingBody struct {
	reads int
}

func (b *readTrackingBody) Read([]byte) (int, error) {
	b.reads++
	return 0, io.EOF
}

func (*readTrackingBody) Close() error { return nil }

func headersEqual(left, right http.Header) bool {
	if len(left) != len(right) {
		return false
	}
	for key, values := range left {
		if !bytes.Equal([]byte(strings.Join(values, "\x00")), []byte(strings.Join(right[key], "\x00"))) {
			return false
		}
	}
	return true
}
