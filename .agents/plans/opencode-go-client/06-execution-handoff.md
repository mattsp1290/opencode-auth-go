# Execution handoff

Status: Ready for local library implementation. Implementation has not started. User confirmed no active users, no compatibility requirement, and no feature flags. Reconfirm repository state and read any newly added guidance at execution time; preserve unrelated changes.

## Ordered work packages

| Order | Package and result | Paths/symbols | Dependencies and gate |
| --- | --- | --- | --- |
| 1 | [WP1: client and sessions](02-client-and-session.md) — public contract compiles | New `go.mod`, `doc.go`, `client.go`, `endpoint.go`, `session.go`, `errors.go` and tests; proposed Client/Options/Protocol/session helper | No external dependency; lock shared signatures first |
| 2a | [WP2: HTTP transport](03-http-transport.md) — three native routes with correct headers and streaming | New `transport.go`, `transport_test.go`, `transport_stream_test.go`; extend WP1 client construction | WP1; exact-byte, redirect, lifecycle, and race tests |
| 2b | [WP3: catalog and errors](04-catalog-and-errors.md) — bounded public catalog and explicit error decoder | New `catalog.go`, `catalog_test.go`, `errors_test.go`; extend WP1 errors/client | WP1; may run alongside WP2 using fake HTTP transport; integrate both afterward |
| 3 | [WP4: verification and handoff](05-verification-and-consumer.md) — examples, CI, live evidence, downstream contract | Existing `README.md`; new examples, `integration_test.go`, `.github/workflows/ci.yml` | WP2 + WP3 integrated; full gates below |

All new paths are anchored to existing root `.` and detailed in their work files. Suggested separate reviewable implementation commits follow WP1, WP2, WP3, and WP4. These are work packages, not currently created branches, PRs, or tracker tasks.

## Parallelization

Use Sol for transport/security integration and Luna for bounded configuration/catalog/test work where useful, as requested. WP2 and WP3 can run concurrently after WP1. Assign one owner to shared `client.go` edits to avoid conflicting implementation. Documentation and fixture preparation may proceed once signatures are settled, but final integration tests wait for both code paths. Do not delegate final integration judgment or run simultaneous unbounded live probes.

## Verification gates

1. WP1 tests prove construction, environment precedence, URL normalization, session selection, and zero I/O side effects. `gofmt -l .` is empty, `go test ./...` and `go vet ./...` pass.
2. WP2 tests prove route-specific authentication, no redirects/credential escape, request immutability, byte-preserving streaming, response-close ownership, cancellation, and concurrent conversation isolation. `go test -race ./...` passes.
3. WP3 tests prove bounded strict-enough catalog decoding, public unauthenticated behavior, safe error classification, Retry-After parsing, no leaked canaries, and explicit body ownership. Run through the real client as well as the isolated decoder.
4. Integration runs `go build ./...`, `go test ./...`, `go test -race ./...`, `go vet ./...`, formatting checks, and CI's platform/toolchain checks. An isolated consumer compiles on the Go minimum without importing Eino or modifying sibling worktrees.
5. Run the opt-in live gate documented in WP4 against one explicitly selected model for each protocol. All three must pass through the implemented Go library before claiming release-readiness. Planning probe success does not substitute for this implementation gate.

Do not repeat broad verification after all gates pass unless a code change, failure, or unresolved risk justifies it. Fix defects in the owning work package and rerun affected checks.

## Definition of done

- Public `opencodeauth` client, endpoint helpers, HTTP transport, catalog, error decoder, examples, and CI exist and follow the work-file contracts.
- The module has no production Eino/SDK dependency, OAuth/storage code, protocol decoder, auto-routing catalog, or added retry loop.
- Default tests require no secrets or network access; live tests are explicit and bounded.
- Host identity and conversation context reach every inference route, with raw tool/reasoning bytes unchanged.
- Deterministic, race, platform/toolchain, isolated-consumer, and live gates pass or release-readiness remains explicitly incomplete with the exact failing external prerequisite.
- README explains transport ownership, stable session lifecycle, public catalog limits, supported coding-agent usage, and future Eino boundary.
- No key, session identifier, live response fixture, or unrelated sibling edit enters a commit.

## Downstream request and deferred work

Canonical request: `$HOME/.agents/projects/eino-providers/requests/2026-09-08-opencode-go-provider.md`. Owner: Matt / Eino provider maintainer. Status: open, unaccepted, non-blocking for local implementation. The separate Eino integration requires an accepted response, a verified public `opencode-auth-go` pin compatible with its Go module, and provider-specific acceptance tests. A temporary local replace is never the final adoption contract.

The next Eino session must inspect the current remote/local state, preserve or isolate the existing dirty worktree, and resolve SDK/protocol choices before coding. Preserve existing backend APIs and behavior; this library's no-compatibility instruction does not extend to that repository.

### Publication and Eino adoption gate

Owner/publisher: Matt, acting as this library's maintainer. After local WP1–WP4 and their gates pass, publish an authorized immutable commit or version tag and record its full commit SHA. Publication is a distinct future delivery action; this planning session creates no tag or remote change. Until that artifact exists and the checks below pass, report “local implementation complete; Eino adoption pending publication/pin verification”, not that Eino can already consume the library.

Set `OPENCODE_AUTH_GO_VERSION` to that real tag or full public commit. Reuse the isolated temporary consumer from WP4, remove its temporary replace/workspace wiring, and run these commands from that consumer:

```sh
GOWORK=off go get github.com/mattsp1290/opencode-auth-go@"$OPENCODE_AUTH_GO_VERSION"
GOWORK=off go list -m -json github.com/mattsp1290/opencode-auth-go
GOWORK=off go mod verify
GOWORK=off go build ./...
GOWORK=off go test ./...
```

Require a resolved Version and absent Replace in the module report, verify the tag/pseudo-version corresponds to the recorded commit, and run on Go 1.25.5 unless the Eino maintainer explicitly accepts another minimum. Attach this evidence to the external request's response before Eino adoption. The Eino maintainer then pins that version and runs the provider request's separate acceptance suite. A successful local replace-based test or request acceptance alone does not pass this gate.

Deferred: Eino adapter implementation, any automatic model routing, persisted host conversation state, credential-store UI, additional endpoints, release publication, and downstream deployment. No shared new repository is needed; ownership already fits this library and `eino-providers`.

First implementation action: create the WP1 Go module and public configuration/session API with its hermetic tests.
