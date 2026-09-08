// Command request sends one explicit native OpenCode Go request and copies
// the response stream to standard output.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	opencodeauth "github.com/mattsp1290/opencode-auth-go"
)

const (
	exampleUserAgent = "opencode-auth-go-request-example/1.0"
	tokenCap         = 512
)

func main() {
	protocolFlag := flag.String("protocol", "", "chat-completions, messages, or responses")
	modelFlag := flag.String("model", "", "bare model ID from the OpenCode Go catalog")
	sessionFlag := flag.String("session-id", "", "stable conversation ID supplied by the host")
	baseURLFlag := flag.String("base-url", "", "optional API root, useful for a local test server")
	flag.Parse()

	protocol, ok := parseProtocol(*protocolFlag)
	if !ok || *modelFlag == "" || *sessionFlag == "" {
		fmt.Fprintln(os.Stderr, "protocol, model, and session-id are required")
		os.Exit(2)
	}

	payload, err := requestPayload(protocol, *modelFlag)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to encode request")
		os.Exit(1)
	}
	client, err := opencodeauth.NewClient(opencodeauth.Options{
		BaseURL:   *baseURLFlag,
		UserAgent: exampleUserAgent,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to configure OpenCode Go client")
		os.Exit(1)
	}
	endpoint, err := client.Endpoint(protocol)
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to select protocol endpoint")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	ctx = opencodeauth.WithSessionID(ctx, *sessionFlag)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		fmt.Fprintln(os.Stderr, "unable to construct request")
		os.Exit(1)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	resp, err := client.HTTPClient().Do(req)
	if err != nil {
		fmt.Fprintln(os.Stderr, "OpenCode Go request failed")
		os.Exit(1)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		fmt.Fprintln(os.Stderr, "OpenCode Go returned an unsuccessful response")
		os.Exit(1)
	}
	if _, err := io.Copy(os.Stdout, resp.Body); err != nil {
		fmt.Fprintln(os.Stderr, "unable to read OpenCode Go response")
		os.Exit(1)
	}
}

func parseProtocol(value string) (opencodeauth.Protocol, bool) {
	switch strings.TrimSpace(value) {
	case string(opencodeauth.ProtocolChatCompletions):
		return opencodeauth.ProtocolChatCompletions, true
	case string(opencodeauth.ProtocolMessages):
		return opencodeauth.ProtocolMessages, true
	case string(opencodeauth.ProtocolResponses):
		return opencodeauth.ProtocolResponses, true
	default:
		return "", false
	}
}

func requestPayload(protocol opencodeauth.Protocol, model string) ([]byte, error) {
	const prompt = "Write a small, correct function that checks whether an integer is even."
	var payload any
	switch protocol {
	case opencodeauth.ProtocolChatCompletions:
		payload = map[string]any{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": prompt}},
			"max_tokens": tokenCap,
			"stream":     true,
		}
	case opencodeauth.ProtocolMessages:
		payload = map[string]any{
			"model":      model,
			"messages":   []map[string]string{{"role": "user", "content": prompt}},
			"max_tokens": tokenCap,
			"stream":     true,
		}
	case opencodeauth.ProtocolResponses:
		payload = map[string]any{
			"model": model,
			"store": false,
			"input": []map[string]any{{
				"role":    "user",
				"content": []map[string]string{{"type": "input_text", "text": prompt}},
			}},
			"max_output_tokens": tokenCap,
			"stream":            true,
		}
	default:
		return nil, fmt.Errorf("unsupported protocol")
	}
	return json.Marshal(payload)
}
