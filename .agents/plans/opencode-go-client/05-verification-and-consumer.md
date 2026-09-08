# WP4 — Examples, verification, and consumer handoff

Prerequisites: WP1 API is fixed; WP2 and WP3 must integrate before final verification. No Eino source change is authorized by this plan.

## Change surface

Edit existing `README.md`. Add new/proposed `examples/models/main.go`, `examples/request/main.go`, `integration_test.go`, and `.github/workflows/ci.yml`. The `examples/` and `.github/` directories are new under existing repository root `.`. The examples' command entrypoints are new. No prototype secrets, live session IDs, responses, generated reasoning, or shell-profile contents enter the repository.

The documentation must describe the API-key/environment precedence, user-agent requirement, stable session ownership, endpoint mapping, direct net/http use of `Endpoint()` and `HTTPClient()`, the meaning of `BaseURL()`, raw response-body ownership, opt-in error decoding, lack of retries/storage, public catalog limitations, and current service documentation link. Show a complete request with context cancellation and Body.Close. Explain that `HTTPClient` does not rewrite caller URLs. Do not publish plug-and-play SDK snippets without testing that exact SDK/version and its base-URL convention; the local contract and examples use net/http.

Use standard-library examples to keep production and example dependencies small. `examples/models` prints catalog IDs. `examples/request` accepts explicit protocol and model, reads the key from the environment, constructs a synthetic coding request with a token cap, and demonstrates stream body passthrough. It must require an explicit session ID supplied by the host/user rather than logging a generated identifier. Do not promise semantic result normalization or stream concatenation. Print user content only as the example's intentional output, never diagnostic credentials or raw errors.

## Deterministic verification

Use `httptest.Server` and `httptest.NewTLSServer` for the real public transport seam. Do not require a live account for default tests. Build fixture payloads for all three native protocols using synthetic identifiers and reasoning data. Use synchronization to prove partial data delivery; test exact bytes rather than only eventual text. Exercise the full `Options` → `NewClient` → `HTTPClient` → request path.

CI must run formatting checks, `go build ./...`, `go test -race ./...`, and `go vet ./...`. Check both the declared minimum Go toolchain and the current stable toolchain available at implementation. Run Darwin and Windows compile/vet checks for this stdlib-only package. Select existing stable action versions at implementation and pin action commits per the resulting repository policy; do not copy sibling Go 1.26.8 as the module minimum. No test may read a developer profile or silently use live credentials.

Go-module consumer smoke: in an isolated temporary consumer, use a temporary go.work or replace only for local verification, import the new module, construct an options value with a custom HTTP client, and issue each native route against a fake server. Compile with Go 1.25.5. Do not modify the dirty Eino worktree to run this check. Before real Eino adoption, repeat against the published immutable pin without a replace directive.

## Opt-in live gate

Proposed live test entrypoint: `TestLiveOpenCodeGo`, skipped unless `OPENCODE_GO_LIVE_TEST=1`. Required environment: `OPENCODE_GO_API_KEY`, `OPENCODE_GO_CHAT_MODEL`, `OPENCODE_GO_MESSAGES_MODEL`, and `OPENCODE_GO_RESPONSES_MODEL`. These three model variables explicitly bind models to native protocols. Empty required live configuration fails with a fixed local error when the gate is enabled. Defaults must not silently charge an unexpected model.

Run `OPENCODE_GO_LIVE_TEST=1 go test -run '^TestLiveOpenCodeGo$' -count=1 -timeout=6m ./...` with those variables already set. Run the three protocol cases sequentially in the test with 45-second per-request contexts (including the catalog) and 512-token ceilings; do not use t.Parallel against shared subscription limits. The overall timeout accommodates seven bounded requests and cleanup. The test owns an unprinted random research conversation ID for its bounded synthetic coding exchange and uses its own test user agent.

Verify: catalog shape; one completed nonstream coding response and one SSE response per protocol through the actual Go client. Read to the protocol terminal event and close, using tiny test-only protocol readers; do not export those readers as production API. Assert some native content and the correct terminal completion, not exact generated wording or event counts. Do not pass merely because HTTP status is 200. If reasoning consumes the ceiling and no completed result is produced, record an inconclusive live gate and deliberately adjust the bounded test/model rather than silently passing.

The completed planning prototypes already tested one nonstreaming tool round trip per protocol. Future local transport tests preserve synthetic tool bytes; semantic incremental assembly and durable reasoning continuation belong to the Eino request. Live tests must emit only status, protocol, model, field/event categories, and pass/fail. They must never dump headers, bodies, tool IDs, sessions, reasoning, or upstream error messages.

If a protocol is unavailable because of quota, service changes, policy, region, or network restrictions, deterministic development can finish, but release-readiness must name that outstanding gate. The implementing developer/user resolves it with a working authorized key/model or a documented scope change. Do not automatically try all catalog models, retry charged POSTs, spoof another client, or switch to the Zen pay-as-you-go path.

## Cross-repository boundary

The external request at `$HOME/.agents/projects/eino-providers/requests/2026-09-08-opencode-go-provider.md` proposes an `opencodego` backend-specific `NewChatModel` returning Eino `model.ToolCallingChatModel`. The adapter owns native serialization, stream decoding, model/protocol selection, Eino error wrapping, tool correlation, usage/finish metadata, and reasoning continuity. The auth library owns API-key injection, destination checks, session header propagation, and catalog/error helpers.

The host owns application identity, credentials, conversation/session persistence, and tool execution. Eino must carry each generation call's context through to net/http so `WithSessionID` remains usable for concurrent conversations.

The request requires explicit protocol selection initially. Any later curated auto-routing must be separately evidenced; catalog IDs alone do not justify it. Raw wire model IDs are bare. The adapter must not inherit Codex's forced stream setting, no-token-cap assumption, endpoint rewrite, or hardcoded reasoning defaults. Reuse an existing Eino protocol adapter only after tests prove it preserves OpenCode native tool and reasoning fields.

SDK adoption is a downstream gate: identify each chosen SDK/module version and verify its real request construction with the library's returned HTTP client against a fake server. Record the exact SDK base-URL option for each route, including any `/v1` appending behavior. A fake that bypasses the SDK does not prove SDK compatibility. The Eino adapter may instead use direct net/http with Endpoint(), the seam that local WP2/WP4 tests will verify.

This request is a planning artifact only. Do not create issues, edit Eino code, publish modules, or claim owner acceptance in this task. Record a usable library tag/full public commit only after implementation and authorized publication have occurred.
