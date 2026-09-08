package opencodeauth

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestTransportStreamsOpaqueNativeResponseBytes(t *testing.T) {
	firstWritten := make(chan struct{})
	release := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("server does not support flushing")
			return
		}
		_, _ = io.WriteString(w, ": comment\n")
		_, _ = io.WriteString(w, `data: {"type":"response.output_item.added","item":{"type":"tool_call","arguments":"{\\"x\\":"}}`+"\n\n")
		flusher.Flush()
		close(firstWritten)
		<-release
		_, _ = io.WriteString(w, "data: ping\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.reasoning","thinking":"opaque <data>"}`+"\n\n")
		_, _ = io.WriteString(w, `data: {"type":"response.completed","usage":{"cost":1.25},"unknown":{"keep":true}}`+"\n\n")
	}))
	defer server.Close()

	client, err := NewClient(Options{APIKey: "key", BaseURL: server.URL, UserAgent: "agent/1", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/responses", strings.NewReader(`{"model":"model","stream":true}`))
	if err != nil {
		t.Fatal(err)
	}
	responseCh := make(chan *http.Response, 1)
	errCh := make(chan error, 1)
	go func() {
		response, err := client.HTTPClient().Do(req)
		if err != nil {
			errCh <- err
			return
		}
		responseCh <- response
	}()

	select {
	case <-firstWritten:
	case err := <-errCh:
		t.Fatalf("Do() error before first frame: %v", err)
	case <-time.After(2 * time.Second):
		t.Fatal("server did not flush first frame")
	}
	var response *http.Response
	select {
	case response = <-responseCh:
	case err := <-errCh:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("Do() did not return after response headers")
	}
	defer response.Body.Close()
	first := make(chan []byte, 1)
	firstErr := make(chan error, 1)
	go func() {
		part := make([]byte, len(": comment\n"))
		_, err := io.ReadFull(response.Body, part)
		if err != nil {
			firstErr <- err
			return
		}
		first <- part
	}()
	select {
	case part := <-first:
		if string(part) != ": comment\n" {
			t.Fatalf("first response bytes = %q", part)
		}
	case err := <-firstErr:
		t.Fatal(err)
	case <-time.After(2 * time.Second):
		t.Fatal("first response frame was not readable")
	}
	close(release)
	rest, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(rest, []byte("data: ping\n\n")) || !bytes.Contains(rest, []byte("opaque <data>")) || !bytes.Contains(rest, []byte(`"cost":1.25`)) {
		t.Fatalf("response remainder lost opaque bytes: %q", rest)
	}
}

func TestTransportCancellationAfterHeadersCancelsBlockedBodyRead(t *testing.T) {
	headers := make(chan struct{})
	serverCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("server does not support flushing")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "headers-only")
		flusher.Flush()
		close(headers)
		<-r.Context().Done()
		close(serverCanceled)
	}))
	defer server.Close()

	client, err := NewClient(Options{APIKey: "key", BaseURL: server.URL, UserAgent: "agent/1", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, client.BaseURL()+"/responses", strings.NewReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	readResult := make(chan error, 1)
	go func() {
		buf := make([]byte, 1024)
		_, readErr := io.ReadFull(response.Body, buf)
		readResult <- readErr
	}()
	select {
	case <-headers:
	case <-time.After(2 * time.Second):
		_ = response.Body.Close()
		t.Fatal("server did not send response headers")
	}
	cancel()
	select {
	case readErr := <-readResult:
		if readErr == nil {
			t.Fatal("blocked body read unexpectedly completed")
		}
	case <-time.After(2 * time.Second):
		_ = response.Body.Close()
		t.Fatal("blocked body read did not fail after cancellation")
	}
	_ = response.Body.Close()
	select {
	case <-serverCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("server request context was not canceled")
	}
}

func TestTransportClosingUnreadBodyCancelsServerStream(t *testing.T) {
	headers := make(chan struct{})
	serverCanceled := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			t.Error("server does not support flushing")
			return
		}
		w.WriteHeader(http.StatusOK)
		_, _ = io.WriteString(w, "unread stream")
		flusher.Flush()
		close(headers)
		<-r.Context().Done()
		close(serverCanceled)
	}))
	defer server.Close()

	client, err := NewClient(Options{APIKey: "key", BaseURL: server.URL, UserAgent: "agent/1", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, client.BaseURL()+"/responses", strings.NewReader("request"))
	if err != nil {
		t.Fatal(err)
	}
	response, err := client.HTTPClient().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-headers:
	case <-time.After(2 * time.Second):
		_ = response.Body.Close()
		t.Fatal("server did not send response headers")
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-serverCanceled:
	case <-time.After(2 * time.Second):
		t.Fatal("closing unread response body did not clean up server stream")
	}
}

func TestTransportPreservesExactRequestAndResponseBytesForAllProtocols(t *testing.T) {
	fixtures := map[string]struct {
		path     string
		request  []byte
		response []byte
	}{
		"chat": {
			path:     "/chat/completions",
			request:  []byte("{\"model\":\"bare:model\",\"tool_calls\":[{\"arguments\":\"{\\\"x\\\":1}\"}]}\n"),
			response: []byte(": comment\ndata: {\"choices\":[{\"delta\":{\"reasoning\":\"opaque \\u0000 bytes\"}}]}\n\ndata: [DONE]\n\n"),
		},
		"messages": {
			path:     "/messages",
			request:  []byte("{\"model\":\"claude:model\",\"content\":[{\"type\":\"text\",\"text\":\"opaque\"}]}\n"),
			response: []byte(": ping\ndata: {\"type\":\"content_block_delta\",\"delta\":{\"text\":\"thinking\\nfragment\"}}\n\ndata: {\"type\":\"message_stop\",\"usage\":{\"cost\":2.5}}\n\n"),
		},
		"responses": {
			path:     "/responses",
			request:  []byte("{\"model\":\"responses:model\",\"input\":[{\"type\":\"input_text\",\"text\":\"raw\"}]}\n"),
			response: []byte(": note\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"raw\\u2028\"}\n\ndata: {\"type\":\"response.completed\",\"unknown\":{\"keep\":true}}\n\n"),
		},
	}
	var mu sync.Mutex
	seenRequests := make(map[string][]byte)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("ReadAll(request body): %v", err)
			return
		}
		mu.Lock()
		seenRequests[r.URL.Path] = body
		mu.Unlock()
		var fixture []byte
		for _, candidate := range fixtures {
			if candidate.path == r.URL.Path {
				fixture = candidate.response
				break
			}
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	client, err := NewClient(Options{APIKey: "key", BaseURL: server.URL, UserAgent: "agent/1", SessionID: "session"})
	if err != nil {
		t.Fatal(err)
	}
	for name, fixture := range fixtures {
		t.Run(name, func(t *testing.T) {
			req, err := http.NewRequest(http.MethodPost, client.BaseURL()+fixture.path, bytes.NewReader(fixture.request))
			if err != nil {
				t.Fatal(err)
			}
			response, err := client.HTTPClient().Do(req)
			if err != nil {
				t.Fatal(err)
			}
			gotResponse, err := io.ReadAll(response.Body)
			_ = response.Body.Close()
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(gotResponse, fixture.response) {
				t.Fatalf("response bytes = %q, want %q", gotResponse, fixture.response)
			}
		})
	}
	mu.Lock()
	defer mu.Unlock()
	for _, fixture := range fixtures {
		if !bytes.Equal(seenRequests[fixture.path], fixture.request) {
			t.Errorf("request bytes for %s = %q, want %q", fixture.path, seenRequests[fixture.path], fixture.request)
		}
	}
}
