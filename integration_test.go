package opencodeauth

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

type transportFixture struct {
	body    []byte
	first   []byte
	rest    []byte
	release chan struct{}
}

type fixtureRequest struct {
	method  string
	path    string
	body    []byte
	auth    string
	apiKey  string
	session string
	agent   string
}

type fixtureHandler struct {
	t         *testing.T
	mu        sync.Mutex
	requests  []fixtureRequest
	releases  chan chan struct{}
	modelsHit chan struct{}
}

func (h *fixtureHandler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/models" {
		if got := r.Header.Get("Authorization"); got != "" {
			h.t.Errorf("catalog Authorization header = %q", got)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			h.t.Errorf("catalog x-api-key header = %q", got)
		}
		if got := r.Header.Get("x-opencode-session"); got != "" {
			h.t.Errorf("catalog session header = %q", got)
		}
		select {
		case h.modelsHit <- struct{}{}:
		default:
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"fixture-chat","object":"model","created":1700000000,"owned_by":"fixture"},{"id":"fixture-messages"}]}`)
		return
	}
	if r.Method != http.MethodPost {
		http.Error(w, "method rejected", http.StatusMethodNotAllowed)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "body rejected", http.StatusBadRequest)
		return
	}
	h.mu.Lock()
	h.requests = append(h.requests, fixtureRequest{
		method:  r.Method,
		path:    r.URL.Path,
		body:    body,
		auth:    r.Header.Get("Authorization"),
		apiKey:  r.Header.Get("x-api-key"),
		session: r.Header.Get("x-opencode-session"),
		agent:   r.Header.Get("User-Agent"),
	})
	h.mu.Unlock()

	fixture, ok := map[string][]byte{
		"/chat/completions": []byte("data: {\"id\":\"chat-fixture\",\"choices\":[{\"delta\":{\"reasoning_content\":\"synthetic reasoning\"}}]}\n\ndata: [DONE]\n\n"),
		"/messages":         []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"messages-fixture\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"synthetic content\",\"reasoning\":\"synthetic reasoning\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"),
		"/responses":        []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"responses-fixture\",\"delta\":\"synthetic content\",\"reasoning\":\"synthetic reasoning\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"responses-fixture\",\"status\":\"completed\"}}\n\n"),
	}[r.URL.Path]
	if !ok {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.WriteHeader(http.StatusOK)
	first := fixture[:len(fixture)/2]
	rest := fixture[len(fixture)/2:]
	_, _ = w.Write(first)
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}
	release := make(chan struct{})
	h.releases <- release
	<-release
	_, _ = w.Write(rest)
}

func newFixtureHandler(t *testing.T) *fixtureHandler {
	t.Helper()
	return &fixtureHandler{
		t:         t,
		releases:  make(chan chan struct{}, 1),
		modelsHit: make(chan struct{}, 1),
	}
}

func TestPublicTransportSeam(t *testing.T) {
	handler := newFixtureHandler(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	client, err := NewClient(Options{
		APIKey:    "synthetic-key",
		BaseURL:   server.URL,
		UserAgent: "integration-test/1.0",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	models, err := client.ListModels(ctx)
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("catalog length = %d, want 2", len(models))
	}
	if got := []string{models[0].ID, models[1].ID}; !equalStrings(got, []string{"fixture-chat", "fixture-messages"}) {
		t.Fatalf("catalog IDs = %v", got)
	}

	tests := []struct {
		protocol Protocol
		path     string
		body     []byte
	}{
		{ProtocolChatCompletions, "/chat/completions", []byte(`{"model":"fixture-chat","messages":[{"role":"user","content":"synthetic coding request"}],"max_tokens":512,"stream":true}`)},
		{ProtocolMessages, "/messages", []byte(`{"model":"fixture-messages","messages":[{"role":"user","content":"synthetic coding request"}],"max_tokens":512,"stream":true}`)},
		{ProtocolResponses, "/responses", []byte(`{"model":"fixture-responses","input":[{"role":"user","content":[{"type":"input_text","text":"synthetic coding request"}]}],"max_output_tokens":512,"stream":true}`)},
	}
	for _, tt := range tests {
		t.Run(string(tt.protocol), func(t *testing.T) {
			endpoint, err := client.Endpoint(tt.protocol)
			if err != nil {
				t.Fatalf("Endpoint() error = %v", err)
			}
			if !strings.HasSuffix(endpoint, tt.path) {
				t.Fatalf("endpoint = %q, want suffix %q", endpoint, tt.path)
			}
			requestContext := WithSessionID(ctx, "session-fixture")
			req, err := http.NewRequestWithContext(requestContext, http.MethodPost, endpoint, bytes.NewReader(tt.body))
			if err != nil {
				t.Fatalf("NewRequest() error = %v", err)
			}
			req.Header.Set("Content-Type", "application/json")
			resp, err := client.HTTPClient().Do(req)
			if err != nil {
				t.Fatalf("Do() error = %v", err)
			}
			defer resp.Body.Close()
			if resp.StatusCode != http.StatusOK {
				t.Fatalf("status = %d, want %d", resp.StatusCode, http.StatusOK)
			}
			release := <-handler.releases
			releaseClosed := false
			defer func() {
				if !releaseClosed {
					close(release)
				}
			}()
			fixture := map[string][]byte{
				"/chat/completions": []byte("data: {\"id\":\"chat-fixture\",\"choices\":[{\"delta\":{\"reasoning_content\":\"synthetic reasoning\"}}]}\n\ndata: [DONE]\n\n"),
				"/messages":         []byte("event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"messages-fixture\"}}\n\nevent: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"synthetic content\",\"reasoning\":\"synthetic reasoning\"}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"),
				"/responses":        []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"responses-fixture\",\"delta\":\"synthetic content\",\"reasoning\":\"synthetic reasoning\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"responses-fixture\",\"status\":\"completed\"}}\n\n"),
			}[tt.path]
			first := fixture[:len(fixture)/2]
			gotFirst := make([]byte, len(first))
			if _, err := io.ReadFull(resp.Body, gotFirst); err != nil {
				t.Fatalf("partial response read error = %v", err)
			}
			if !bytes.Equal(gotFirst, first) {
				t.Fatalf("partial response = %q, want %q", gotFirst, first)
			}
			close(release)
			releaseClosed = true
			rest, err := io.ReadAll(resp.Body)
			if err != nil {
				t.Fatalf("response read error = %v", err)
			}
			if !bytes.Equal(append(gotFirst, rest...), fixture) {
				t.Fatalf("response bytes changed in transit")
			}
		})
	}

	handler.mu.Lock()
	requests := append([]fixtureRequest(nil), handler.requests...)
	handler.mu.Unlock()
	if len(requests) != len(tests) {
		t.Fatalf("inference requests = %d, want %d", len(requests), len(tests))
	}
	for i, tt := range tests {
		got := requests[i]
		if got.method != http.MethodPost || got.path != tt.path || !bytes.Equal(got.body, tt.body) {
			t.Fatalf("request %d was not preserved exactly", i)
		}
		if got.agent != "integration-test/1.0" || got.session != "session-fixture" {
			t.Fatalf("request %d identity headers = agent %q, session %q", i, got.agent, got.session)
		}
		if tt.protocol == ProtocolMessages {
			if got.apiKey != "synthetic-key" || got.auth != "" {
				t.Fatalf("messages auth headers = api-key %q, authorization %q", got.apiKey, got.auth)
			}
		} else if got.auth != "Bearer synthetic-key" || got.apiKey != "" {
			t.Fatalf("bearer auth headers = authorization %q, api-key %q", got.auth, got.apiKey)
		}
	}
}

func TestPublicTransportTLSAndCatalogHeaders(t *testing.T) {
	handler := newFixtureHandler(t)
	server := httptest.NewTLSServer(handler)
	defer server.Close()
	client, err := NewClient(Options{
		APIKey:     "synthetic-tls-key",
		BaseURL:    server.URL,
		UserAgent:  "integration-tls-test/1.0",
		HTTPClient: server.Client(),
		SessionID:  "configured-session",
	})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	models, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() over TLS error = %v", err)
	}
	if len(models) != 2 {
		t.Fatalf("TLS catalog length = %d, want 2", len(models))
	}

	endpoint, err := client.Endpoint(ProtocolResponses)
	if err != nil {
		t.Fatalf("Endpoint() over TLS error = %v", err)
	}
	body := []byte(`{"model":"fixture-responses","input":[{"role":"user","content":[{"type":"input_text","text":"synthetic coding request"}]}],"max_output_tokens":512,"stream":true}`)
	req, err := http.NewRequestWithContext(WithSessionID(context.Background(), "tls-session"), http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		t.Fatalf("NewRequest() over TLS error = %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatalf("Do() over TLS error = %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("TLS inference status = %d, want %d", resp.StatusCode, http.StatusOK)
	}
	release := <-handler.releases
	first := []byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"response_id\":\"responses-fixture\",\"delta\":\"synthetic content\",\"reasoning\":\"synthetic reasoning\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"responses-fixture\",\"status\":\"completed\"}}\n\n")
	partial := make([]byte, len(first)/2)
	if _, err := io.ReadFull(resp.Body, partial); err != nil {
		close(release)
		t.Fatalf("TLS partial response error = %v", err)
	}
	close(release)
	remaining, err := io.ReadAll(resp.Body)
	if err != nil || !bytes.Equal(append(partial, remaining...), first) {
		t.Fatalf("TLS response bytes changed in transit")
	}
	handler.mu.Lock()
	last := handler.requests[len(handler.requests)-1]
	handler.mu.Unlock()
	if last.auth != "Bearer synthetic-tls-key" || last.session != "tls-session" {
		t.Fatalf("TLS inference identity headers = authorization %q, session %q", last.auth, last.session)
	}
}

func equalStrings(got, want []string) bool {
	if len(got) != len(want) {
		return false
	}
	for i := range got {
		if got[i] != want[i] {
			return false
		}
	}
	return true
}

func TestLiveOpenCodeGo(t *testing.T) {
	if os.Getenv("OPENCODE_GO_LIVE_TEST") != "1" {
		t.Skip("live OpenCode Go gate is opt-in")
	}
	key := os.Getenv("OPENCODE_GO_API_KEY")
	models := map[Protocol]string{
		ProtocolChatCompletions: os.Getenv("OPENCODE_GO_CHAT_MODEL"),
		ProtocolMessages:        os.Getenv("OPENCODE_GO_MESSAGES_MODEL"),
		ProtocolResponses:       os.Getenv("OPENCODE_GO_RESPONSES_MODEL"),
	}
	if key == "" || models[ProtocolChatCompletions] == "" || models[ProtocolMessages] == "" || models[ProtocolResponses] == "" {
		t.Fatal("live OpenCode Go configuration is incomplete")
	}
	sessionID, err := randomLiveSessionID()
	if err != nil {
		t.Fatal("unable to create live test session")
	}
	client, err := NewClient(Options{APIKey: key, UserAgent: "opencode-auth-go-live-test/1.0"})
	if err != nil {
		t.Fatal("live client configuration failed")
	}
	catalogContext, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	catalog, err := client.ListModels(catalogContext)
	cancel()
	if err != nil || len(catalog) == 0 {
		t.Fatal("live catalog request failed")
	}
	for _, model := range catalog {
		if model.ID == "" {
			t.Fatal("live catalog shape is invalid")
		}
	}

	for _, protocol := range []Protocol{ProtocolChatCompletions, ProtocolMessages, ProtocolResponses} {
		model := models[protocol]
		if err := runLiveRequest(t, client, protocol, model, sessionID, false); err != nil {
			t.Fatalf("live %s nonstream failed", protocol)
		}
		if err := runLiveRequest(t, client, protocol, model, sessionID, true); err != nil {
			t.Fatalf("live %s stream failed", protocol)
		}
	}
}

func randomLiveSessionID() (string, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return "opencode-go-live-" + hex.EncodeToString(raw[:]), nil
}

func runLiveRequest(t *testing.T, client *Client, protocol Protocol, model, sessionID string, stream bool) error {
	t.Helper()
	payload := livePayload(protocol, model, stream)
	endpoint, err := client.Endpoint(protocol)
	if err != nil {
		return errors.New("endpoint")
	}
	ctx, cancel := context.WithTimeout(WithSessionID(context.Background(), sessionID), 45*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return errors.New("request")
	}
	req.Header.Set("Content-Type", "application/json")
	if stream {
		req.Header.Set("Accept", "text/event-stream")
	}
	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		return errors.New("transport")
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return errors.New("status")
	}
	if stream {
		return readLiveStream(protocol, resp.Body)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil || len(body) == 0 {
		return errors.New("body")
	}
	return validateLiveNonstream(protocol, body)
}

func validateLiveNonstream(protocol Protocol, body []byte) error {
	switch protocol {
	case ProtocolChatCompletions:
		var response struct {
			Choices []struct {
				Message struct {
					Content string `json:"content"`
				} `json:"message"`
				FinishReason *string `json:"finish_reason"`
			} `json:"choices"`
		}
		if json.Unmarshal(body, &response) != nil || len(response.Choices) == 0 {
			return errors.New("chat response")
		}
		for _, choice := range response.Choices {
			if strings.TrimSpace(choice.Message.Content) != "" && choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != "" {
				return nil
			}
		}
		return errors.New("chat completion")
	case ProtocolMessages:
		var response struct {
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			StopReason *string `json:"stop_reason"`
		}
		if json.Unmarshal(body, &response) != nil || response.StopReason == nil || strings.TrimSpace(*response.StopReason) == "" {
			return errors.New("messages response")
		}
		for _, block := range response.Content {
			if block.Type == "text" && strings.TrimSpace(block.Text) != "" {
				return nil
			}
		}
		return errors.New("messages completion")
	case ProtocolResponses:
		var response struct {
			Status string `json:"status"`
			Output []struct {
				Content []struct {
					Type string `json:"type"`
					Text string `json:"text"`
				} `json:"content"`
			} `json:"output"`
		}
		if json.Unmarshal(body, &response) != nil || response.Status != "completed" {
			return errors.New("responses response")
		}
		for _, item := range response.Output {
			for _, block := range item.Content {
				if block.Type == "output_text" && strings.TrimSpace(block.Text) != "" {
					return nil
				}
			}
		}
		return errors.New("responses completion")
	default:
		return errors.New("protocol")
	}
}

func TestLiveProtocolValidationRequiresContentAndCompletion(t *testing.T) {
	valid := map[Protocol][]byte{
		ProtocolChatCompletions: []byte(`{"choices":[{"message":{"content":"synthetic answer"},"finish_reason":"stop"}]}`),
		ProtocolMessages:        []byte(`{"content":[{"type":"text","text":"synthetic answer"}],"stop_reason":"end_turn"}`),
		ProtocolResponses:       []byte(`{"status":"completed","output":[{"content":[{"type":"output_text","text":"synthetic answer"}]}]}`),
	}
	for protocol, body := range valid {
		if err := validateLiveNonstream(protocol, body); err != nil {
			t.Errorf("valid %s response rejected", protocol)
		}
	}
	invalid := map[Protocol][]byte{
		ProtocolChatCompletions: []byte(`{"choices":[{"message":{"content":"synthetic answer"},"finish_reason":null}]}`),
		ProtocolMessages:        []byte(`{"content":[{"type":"text","text":""}],"stop_reason":"end_turn"}`),
		ProtocolResponses:       []byte(`{"status":"completed","output":[]}`),
	}
	for protocol, body := range invalid {
		if err := validateLiveNonstream(protocol, body); err == nil {
			t.Errorf("incomplete %s response accepted", protocol)
		}
	}
}

func livePayload(protocol Protocol, model string, stream bool) []byte {
	base := map[string]any{"model": model, "stream": stream}
	switch protocol {
	case ProtocolChatCompletions:
		base["messages"] = []map[string]string{{"role": "user", "content": "Reply with one short sentence describing a safe code review."}}
		base["max_tokens"] = 512
	case ProtocolMessages:
		base["messages"] = []map[string]string{{"role": "user", "content": "Reply with one short sentence describing a safe code review."}}
		base["max_tokens"] = 512
	case ProtocolResponses:
		base["input"] = []map[string]any{{"role": "user", "content": []map[string]string{{"type": "input_text", "text": "Reply with one short sentence describing a safe code review."}}}}
		base["max_output_tokens"] = 512
		base["store"] = false
	}
	payload, _ := json.Marshal(base)
	return payload
}

func readLiveStream(protocol Protocol, body io.Reader) error {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 1024), 1<<20)
	content := false
	terminal := false
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" {
			continue
		}
		switch protocol {
		case ProtocolChatCompletions:
			if data == "[DONE]" {
				terminal = true
				continue
			}
			var event struct {
				Choices []struct {
					Delta struct {
						Content string `json:"content"`
					} `json:"delta"`
					FinishReason *string `json:"finish_reason"`
				} `json:"choices"`
			}
			if json.Unmarshal([]byte(data), &event) != nil {
				return errors.New("chat stream event")
			}
			for _, choice := range event.Choices {
				content = content || strings.TrimSpace(choice.Delta.Content) != ""
				terminal = terminal || choice.FinishReason != nil && strings.TrimSpace(*choice.FinishReason) != ""
			}
		case ProtocolMessages:
			var event struct {
				Type  string `json:"type"`
				Delta struct {
					Text string `json:"text"`
				} `json:"delta"`
			}
			if json.Unmarshal([]byte(data), &event) != nil {
				return errors.New("messages stream event")
			}
			content = content || strings.TrimSpace(event.Delta.Text) != ""
			terminal = terminal || event.Type == "message_stop"
		case ProtocolResponses:
			var event struct {
				Type     string `json:"type"`
				Delta    string `json:"delta"`
				Response struct {
					Status string `json:"status"`
				} `json:"response"`
			}
			if json.Unmarshal([]byte(data), &event) != nil {
				return errors.New("responses stream event")
			}
			content = content || event.Type == "response.output_text.delta" && strings.TrimSpace(event.Delta) != ""
			terminal = terminal || event.Type == "response.completed" && event.Response.Status == "completed"
		}
	}
	if err := scanner.Err(); err != nil {
		return errors.New("stream")
	}
	if !content || !terminal {
		return errors.New("stream completion")
	}
	return nil
}
