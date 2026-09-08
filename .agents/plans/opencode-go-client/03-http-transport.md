# WP2 — Authenticated native HTTP transport

Prerequisite: WP1 configuration and public signatures compile. Existing evidence is `transport.go` in the pinned sibling `codex-auth-go`: clone caller requests and return response bodies directly. Unlike that transport, this module has no refresh, endpoint rewriting, or Codex-specific headers.

## Change surface

New/proposed root files `transport.go`, `transport_test.go`, `transport_stream_test.go`; existing insertion point `.`. Extend the new `client.go` from WP1 to construct the transport. Proposed private `transport` implements `http.RoundTripper` and optionally forwards `CloseIdleConnections` to its underlying transport. Do not add exported protocol request, event, or response types.

## Request flow

1. Check request context and the WP1 origin/method/path restrictions before attaching credentials. Locally rejected requests must close their request bodies as required by the RoundTripper contract.
2. Clone the request and header map. Preserve the original body stream, `GetBody`, context, content length, valid query, and all unrelated headers. Do not read, marshal, normalize, or replace the request body.
3. Resolve session state for inference. Reject missing/invalid session before invoking the underlying transport.
4. Delete all case variants of protected headers from the clone before setting the library-owned values below. Do not rely solely on `Header.Del` to clean manually noncanonical duplicate header map keys.
5. Call the underlying RoundTripper once. Return its response and body without reading, buffering, decoding, consuming errors, or closing the body on a normal response. Add no retry loop or alternative-provider fallback.

| Exact suffix / method | Auth written | Other owned headers |
| --- | --- | --- |
| `/chat/completions`, POST | `Authorization: Bearer <redacted>`; remove `x-api-key` | Host `User-Agent`, selected `x-opencode-session` |
| `/responses`, POST | `Authorization: Bearer <redacted>`; remove `x-api-key` | Same |
| `/messages`, POST | `x-api-key: <redacted>`; remove `Authorization` | Same; preserve a single valid caller `anthropic-version`, otherwise default `2023-06-01` |
| `/models`, GET | Remove `Authorization` and `x-api-key` | Host `User-Agent`; remove `x-opencode-session` |

Protected headers are Authorization, x-api-key, User-Agent, and x-opencode-session. Treat header names case-insensitively. For `anthropic-version`, reject control characters or conflicting duplicate values without echoing them. Preserve other SDK headers such as `anthropic-beta`, `Accept`, and `Content-Type`. Do not inject Codex `originator`, `session_id`, or account headers. Caller bodies retain bare or qualified model IDs exactly as supplied; documentation teaches bare IDs rather than silently rewriting them.

## HTTP client and lifecycle

When `Options.HTTPClient` is nil, construct an HTTP client using a clone of the default HTTP transport, without a whole-stream timeout. Normal net/http dialing/TLS defaults remain available. The host supplies request contexts/deadlines; no detached background work is needed.

When supplied, shallow-copy the HTTP client configuration and wrap its Transport (default transport if nil). Preserve its timeout and relevant client settings without mutating the caller's object. The transport remains a trusted injected component that can observe credentials; it must obey the RoundTripper contract. A caller must not wrap an already authenticated client again. Document that a supplied overall Timeout also caps body streaming.

The destination/redirect guarantees cover requests the library passes into that transport and redirects performed by the returned net/http client. An injected transport, proxy, or caller-modified returned client is outside that guarantee: it can log credentials, reroute requests, or perform redirects/retries itself. The caller owns that component's confidentiality and routing policy. State this boundary in Options documentation and README; do not claim to sandbox arbitrary RoundTrippers. A benign recording-transport test must prove that only validated/decorated requests enter this trusted seam and that denied requests never do.

Always replace the copied client's redirect policy with `http.ErrUseLastResponse`: return 3xx responses without following any redirect, including same-origin redirects. This keeps SDK behavior predictable and prevents `x-api-key` leaks. Do not call a caller-provided CheckRedirect. Each `HTTPClient()` call returns a fresh client value so external configuration mutation does not mutate the stored client template; transports remain reusable and immutable. Callers must not mutate a client during concurrent use, per net/http conventions.

Cancellation, caller body close, read errors, trailers, SSE comments, ping frames, chunk ordering, unknown JSON fields, and native terminal events remain normal HTTP semantics. Library code must not detect `[DONE]` or other protocol terminal records. The host owns response Body.Close on both successful and unsuccessful calls, unless it explicitly transfers ownership to WP3's error decoder.

Byte preservation refers to request payloads passed into and response bodies returned by the underlying transport. Standard HTTP framing and that transport's transparent decompression remain its normal behavior; this wrapper adds no content transformation.

No library-level retry is added. Standard-library/injected transport behavior remains its own contract; do not promise globally exactly-once inference. Do not add idempotency headers or read a body to make a POST replayable.

## Acceptance and tests

- Table-driven fake-server tests for all four routes assert exact path/query, method, correct auth, user agent, version and session behavior, with no conflicting credential header. Compare caller headers and URL before and after the request.
- Denied host/path/method, encoded traversal, conflicting Host, and cross-origin/same-origin/HTTP redirects never send credentials to a second server. Test the actual returned `*http.Client`, not only the private RoundTripper. Returned 3xx body remains caller-owned.
- Supply headers under canonical and noncanonical casing, including conflicting credentials and duplicate session/user-agent values. The server receives one authoritative value.
- Verify streaming without timing guesses: server flushes one frame, then waits on a test synchronization channel. Client must receive that frame before the test releases the next frame and terminal response. Include native tool argument fragments, reasoning, ping/comment lines, and a trailing usage/cost record as arbitrary bytes.
- Compare full request and response bytes for all three native protocol fixtures. Unknown fields and opaque thinking/reasoning data must survive unchanged. Synthetic fixtures prove passthrough, not that the library semantically assembles tools.
- Cancel after headers and during a blocked body read. Observe server context cancellation and prompt reader failure. Close an unread body and verify stream cleanup. Use bounded waits only as deadlock guards.
- A tracking custom transport confirms body-close ownership on local rejection, no eager response reads, request body non-buffering, and CloseIdleConnections delegation.
- Share one transport across two simultaneous conversation contexts under `go test -race ./...`; keys/session values must not cross requests. Confirm client cloning does not mutate the injected client.

Verification: `go test ./...`, `go test -race ./...`, `go vet ./...`. HTTP error parsing, retries, Eino assembly, and model selection are excluded from this work package.
