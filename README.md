# opencode-auth-go

`opencode-auth-go` is a small, standard-library-only authentication and
transport boundary for applications that call OpenCode Go models. It keeps
native request and response formats in the caller, while handling destination
validation, API-key injection, and stable conversation-session headers.

The current OpenCode Go service documentation is at
[opencode.ai/docs/go](https://opencode.ai/docs/go/). The service currently
exposes three native routes below the default `https://opencode.ai/zen/go/v1`
root:

| Protocol | `Protocol` value | Endpoint suffix | Credential header |
| --- | --- | --- | --- |
| OpenAI Chat Completions | `ProtocolChatCompletions` | `/chat/completions` | `Authorization: Bearer ...` |
| Anthropic Messages | `ProtocolMessages` | `/messages` | `x-api-key: ...` |
| OpenAI Responses | `ProtocolResponses` | `/responses` | `Authorization: Bearer ...` |

The service also provides `GET /models`. Its catalog is a public description
of model IDs and basic metadata; it does not describe protocol support, tool
capabilities, context limits, pricing, or current subscription quota.

## Configuration

Construct a client with a caller identity and either an explicit key or the
`OPENCODE_GO_API_KEY` environment variable:

```go
client, err := opencodeauth.NewClient(opencodeauth.Options{
	APIKey:    os.Getenv("OPENCODE_GO_API_KEY"), // an explicit nonempty value wins
	UserAgent: "my-coding-agent/1.0",            // identify your host application
})
if err != nil {
	// Handle local configuration before creating a request.
	return err
}
```

An explicit nonempty `Options.APIKey` has precedence over the environment.
When it is empty, the environment is read once by `NewClient`; changing the
environment later does not change an existing client. The key, user agent,
base URL, and optional configured session are validated locally. The package
does not read shell profiles, history, CLI credential stores, or secret
managers, and it never writes credentials to disk.

`UserAgent` is required and must be a visible ASCII caller identifier of at
most 256 bytes. It is not a provider identity and is not replaced with a
generic Go or SDK user agent. `BaseURL` defaults to `DefaultBaseURL`; a custom
HTTPS URL is allowed, while HTTP is restricted to literal loopback or
`localhost` destinations for local testing. `BaseURL()` returns the normalized
API root. It does not describe, or adapt to, an SDK's base-URL convention.

Use one stable session ID for all turns and tool results belonging to a
conversation. The host owns that ID and should reuse it after restart when it
wants continuity. Concurrent conversations should use different IDs. A
configured `Options.SessionID` applies to every inference request, or a
per-request context can take precedence:

```go
ctx := opencodeauth.WithSessionID(context.Background(), "conversation-from-host")
endpoint, err := client.Endpoint(opencodeauth.ProtocolResponses)
if err != nil {
	return err
}
```

The package does not generate or persist session IDs, infer them from API
keys, or expose login/logout/storage APIs. Models requests do not need a
session and do not receive one.

## Direct net/http requests

The supported consumer seam is `Endpoint()` plus the authenticated
`HTTPClient()`. The client does not rewrite caller URLs: the request must use
the endpoint returned for the selected protocol, and only the documented
`GET /models` and three `POST` inference routes are accepted. The transport
preserves request bodies and returns response bodies directly, so callers own
reading and closing bodies. It does not retry, buffer, normalize native JSON,
concatenate streams, or store responses.

This complete request uses a native Responses payload, propagates context
cancellation, and closes the body. Native Chat Completions, Messages, and
Responses payloads remain the caller's responsibility:

```go
payload := strings.NewReader(`{"model":"gpt-5.6-luna","input":[{"role":"user","content":[{"type":"input_text","text":"Review this code."}]}],"max_output_tokens":512,"stream":true,"store":false}`)
ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
defer cancel()
ctx = opencodeauth.WithSessionID(ctx, "conversation-from-host")

endpoint, err := client.Endpoint(opencodeauth.ProtocolResponses)
if err != nil {
	return err
}
req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, payload)
if err != nil {
	return err
}
req.Header.Set("Content-Type", "application/json")
resp, err := client.HTTPClient().Do(req)
if err != nil {
	return err
}
if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
	return opencodeauth.DecodeHTTPError(resp) // consumes and closes this error body
}
defer resp.Body.Close()
_, err = io.Copy(os.Stdout, resp.Body) // stream bytes pass through unchanged
return err
```

For an unsuccessful response, `DecodeHTTPError` is an opt-in helper that
consumes a bounded error body and returns a local typed classification. The
transport itself does not decode errors; a successful response body stays
untouched until the caller reads it. Always close whichever response body you
receive. A supplied `Options.HTTPClient` is copied, but its injected
transport is trusted code that can observe credentials and control proxying,
logging, redirects, or retries. The returned client does not invoke caller
redirect hooks and does not rewrite URLs. A supplied overall `http.Client`
`Timeout` also limits streaming-body reads.

For a catalog request, use `client.ListModels(ctx)`. Each call makes a fresh
request without authorization or session headers, and the result is only
catalog metadata. `NewClient` still requires `OPENCODE_GO_API_KEY` because the
resulting client is also capable of authenticated inference, but the key is
not sent by `ListModels`. The package has no retry loop, account-status check,
quota API, automatic protocol routing, or model allowlist.

## Examples

The standard-library examples keep protocol selection explicit:

```text
go run ./examples/models
go run ./examples/request -protocol responses -model gpt-5.6-luna -session-id conversation-from-host
```

The request example reads `OPENCODE_GO_API_KEY`, sends a bounded synthetic
coding request, and copies stream bytes to standard output. It requires a
session ID supplied by the host or user; it does not generate or print one.
Neither example prints credentials, raw diagnostic errors, response headers,
or reasoning as diagnostics. Native result semantics remain with the caller.

## Development

The repository uses Go 1.25.5 as its minimum toolchain. The default checks
are:

```text
gofmt -l .
go build ./...
go test -race ./...
go vet ./...
```

Default tests use local deterministic HTTP fixtures. The live gate is opt-in
and requires explicitly configured protocol-specific model IDs and
`OPENCODE_GO_LIVE_TEST=1`; it is never enabled by ordinary tests.

To run that gate, set `OPENCODE_GO_API_KEY`, `OPENCODE_GO_CHAT_MODEL`,
`OPENCODE_GO_MESSAGES_MODEL`, and `OPENCODE_GO_RESPONSES_MODEL`, then run:

```text
OPENCODE_GO_LIVE_TEST=1 go test -run '^TestLiveOpenCodeGo$' -count=1 -timeout=6m ./...
```

The gate makes one catalog request and one nonstreaming plus one streaming
request for each explicit model. It does not select fallback models.
