# WP1 — Client configuration, endpoints, and sessions

Prerequisites: read 00 and 01; preserve unrelated worktree changes. All implementation files and symbols below are **new/proposed**. Their existing insertion point is the repository root `.`. No proposed public signature is an existing compatibility promise.

## Change surface

New root files: `go.mod`, `doc.go`, `client.go`, `endpoint.go`, `session.go`, `errors.go`, and matching `client_test.go`, `endpoint_test.go`, `session_test.go`. WP3 later extends `errors.go` and adds its tests. Module: `github.com/mattsp1290/opencode-auth-go`; package: `opencodeauth`; Go directive: `1.25.5`; no production third-party dependency.

Proposed public contract:

| Symbol | Contract |
| --- | --- |
| `DefaultBaseURL` | Constant `https://opencode.ai/zen/go/v1` |
| `Options` | Fields `APIKey string`, `BaseURL string`, `UserAgent string`, `SessionID string`, `HTTPClient *http.Client` |
| `NewClient(Options) (*Client, error)` | Resolve and validate configuration once; no filesystem writes or network calls |
| `(*Client).HTTPClient() *http.Client` | Return a fresh HTTP-client value around immutable configured transport; see WP2 |
| `(*Client).BaseURL() string` | Return normalized public API root; this is not a promise about any SDK's path-appending convention |
| `Protocol` | String-based type with `ProtocolChatCompletions="chat-completions"`, `ProtocolMessages="messages"`, `ProtocolResponses="responses"` constants |
| `(*Client).Endpoint(Protocol) (string, error)` | Return base plus `/chat/completions`, `/messages`, or `/responses`; reject unknown protocol |
| `WithSessionID(context.Context, string) context.Context` | Attach an opaque conversation ID with a private context key |
| `ErrMissingAPIKey`, `ErrInvalidConfiguration`, `ErrMissingSessionID`, `ErrInvalidSessionID`, `ErrDisallowedRequest` | Stable local sentinels inspectable with `errors.Is`; no untrusted values in messages |

`Client` fields stay private. Do not expose a credential getter. Use keyed `Options` literals in documentation. Credentials are memory-held configuration; no explicit Login, Logout, Save, or Status APIs are needed. Constructor success means local configuration is valid, not that a subscription was authenticated.

## Configuration rules

- Nonempty explicit `APIKey` wins. Otherwise snapshot `os.Getenv("OPENCODE_GO_API_KEY")` once in `NewClient`. Missing/empty yields `ErrMissingAPIKey`. A supplied whitespace-only key is invalid and must not silently fall back. Reject leading/trailing whitespace and header-control characters without returning the value.
- The library must never source `~/.profile`, search shell history, invoke a secret manager, or read OpenCode CLI auth storage. Hosts own environment setup and key replacement by constructing a new client.
- `UserAgent` is required and must be nonblank visible ASCII, with a maximum of 256 bytes and no control characters. README examples use a real host identifier such as `my-coding-agent/1.0`. It describes the caller, not a spoofed known client. Do not substitute `Go-http-client` or another SDK's identity.
- Optional `SessionID` is for a client dedicated to one conversation. Validate configured IDs at construction: nonempty visible ASCII without whitespace, at most 256 bytes. Do not derive it from the API key or log it.
- A session attached by `WithSessionID` takes precedence over configured `Options.SessionID`. An explicitly attached empty or invalid value is an error, not a fallback. With no context value, use the configured session. If neither exists, reject inference before sending a request. The helper itself cannot return an error; validate at send time.
- A host reuses the same ID across turns and tool results of one conversation, including after restart when continuity is wanted. Different concurrent conversations use distinct IDs. The library neither generates an ID per request nor stores a global conversation identity.
- Models requests require no session and omit session and auth headers. An absent/invalid per-request session is irrelevant on the models route.

## Endpoint rules

An empty `BaseURL` selects `DefaultBaseURL`. Parse an absolute HTTP(S) URL, normalize trailing slashes, and reject userinfo, query, fragment, opaque URLs, encoded path separators/dot segments, and ambiguous/non-clean internal paths. HTTPS overrides are explicit trusted destinations. Permit HTTP only for literal loopback IP addresses or `localhost`; do not resolve a remote name and accept it because a single lookup returns loopback.

Configured custom path prefixes are supported, e.g. a loopback test root ending `/proxy/go/v1`. Route helpers append exactly one suffix; they never insert another `/v1`. A host-only root is valid. Match request origins using lowercase scheme/hostname and effective port (443 or 80 when omitted). Reject a request with userinfo, fragment, noncanonical encoded/dot path, or conflicting `Request.Host` override.

Use one private canonical-authority helper for configured base, request URL, and any `Request.Host`: lowercase DNS names, normalize literal IP spelling with `net/netip`, and compare numeric effective ports. Accept only explicit ports 1–65535. Reject trailing-dot DNS names, IPv6 zone identifiers, malformed authorities, and opaque request URLs. An empty `Request.Host` uses the URL authority. A nonempty Host must be an authority only (no scheme, path, query, fragment, or userinfo) and canonicalize to the URL's hostname/effective port using its scheme. Equivalent DNS casing, explicit default ports, and equivalent bracketed IPv6 spellings are accepted. After validation set the cloned request's Host to its URL.Host so serialization uses one checked authority; do not mutate the caller's Host.

The allowed request routes are exactly GET `<base>/models` and POST the three inference endpoints. Do not proxy arbitrary URLs or rewrite caller URLs. Reject wrong methods, neighboring Zen URLs, lookalike hosts, and sibling paths before credentials reach the underlying transport. Preserve ordinary request query parameters on valid routes; never copy them into library-generated error text. Other future API routes require an intentional later extension.

## Acceptance and verification

Use table tests for explicit/environment precedence, environment snapshot stability, missing and malformed keys, required host identity, invalid base URLs, trailing slash normalization, IPv4/IPv6 loopback, custom prefixes, equivalent default ports, and protocol-to-endpoint mapping. Assert rejected configuration makes zero HTTP requests.

Test Host empty, matching, DNS-case-equivalent, default-port-equivalent, and equivalent bracketed IPv6 forms. Test mismatching hostname/port, trailing-dot names, IPv6 zones, userinfo/path contamination, and manually constructed opaque URLs. Assert denied cases never invoke the underlying transport.

Test configured session reuse, context override, explicit empty override, isolated concurrent contexts, and catalog exemption. Confirm construction neither reads credential files nor writes under HOME/XDG. Normal formatting/logging examples must never print options or environment contents.

Run `gofmt -l .`, `go test ./...`, and `go vet ./...` after WP1. Use synthetic values in tests. Keep later work compilable by defining shared types before parallel implementation starts. No flags or migration work applies to this new API.
