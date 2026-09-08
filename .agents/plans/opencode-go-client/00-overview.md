# OpenCode Go client implementation plan

Status: Ready

Planning deliverable only. No library implementation, dependency change, release, or Eino provider implementation has occurred. Temporary Python probes were used for research with explicit user authorization.

## Application context

```json
{
  "application_context": {
    "has_active_users": false,
    "backward_compatibility_required": false,
    "feature_flags": "not-applicable",
    "confirmation_digest": "d7b1ad9b1c048cf939cc8ca9624ee3951fce6445d1da727d259c51e95bd3078a",
    "confirmed_at": "2026-09-08T16:32:14Z"
  }
}
```

User confirmation: “brand new. no users. ignore backwards compaibility and feature flags”. There are no existing local APIs, stored credentials, or workflows to migrate. This permission applies to this library; it does not authorize breaking changes in the existing Eino repository.

## Outcome and success criteria

Create a small Go library for using an OpenCode Go subscription from a coding agent. Follow `codex-auth-go`'s explicit client, authenticated `*http.Client`, endpoint test seam, and model-catalog ergonomics. Adapt the mechanism to API keys and three native protocols.

Implementation is complete when a consumer can construct the proposed `opencodeauth.Client` from `OPENCODE_GO_API_KEY`, identify its application and conversation, obtain an ordinary `*http.Client`, and issue native Chat Completions, Messages, and Responses requests. Local deterministic tests must prove correct route-specific authentication, immediate streaming, unchanged tool/reasoning bytes, cancellation, credential confinement, and public model discovery. Each protocol must also pass an opt-in live smoke test through the implemented Go transport before release-readiness is claimed.

The later `eino-providers/opencodego` adapter must be possible without exporting credentials or importing Eino into this module. Its implementation remains a separate follow-up, as requested by the user. The specified local consumer contract is ordinary `net/http`; this plan does not claim drop-in compatibility with a particular native SDK.

## Change type and evidence

This is a greenfield library with new public API, HTTP transport, model discovery, optional HTTP error decoding, examples, and CI. Existing local content at `487c158371ef0675df1fba95955574859ceb8788` is only `README.md`, `LICENSE`, and `.gitignore`. The initial worktree was clean. No local `AGENTS.md`, contributor guide, Go module, or test/build configuration exists.

The sibling library already supplies an authenticated HTTP client without implementing its consumer's SSE parser. OpenCode Go differs in four material ways: static API-key credentials, different auth headers per endpoint, conversation-scoped identification, and a public model list without protocol metadata. Research and source pins are in [01-research.md](01-research.md).

## Decisions

1. Keep the boundary at authentication and HTTP transport. The host/SDK owns generation request schemas, native streaming decoders, tool execution, reasoning replay, model choice, and conversation persistence. Do not introduce a generic generation SDK or event representation here.
2. Use package name `opencodeauth` and module `github.com/mattsp1290/opencode-auth-go`. Use Go `1.25.5` initially, matching the inspected Eino consumer's directive. Use only the standard library in production.
3. Read the API key from explicit options or `OPENCODE_GO_API_KEY` at construction. Require the host's own `UserAgent`. Require a stable session ID for inference, supplied per context or for a single-conversation client. Do not load shell profiles or write credential files from the library.
4. Preserve the selected native route and every request/response body byte. Do not rewrite all requests to one endpoint, guess a protocol from model names, or retry through another endpoint or Zen balance URL.
5. Treat the catalog as discovery, not proof of authenticated entitlement or inference availability. Do not copy Codex OAuth, refresh, login/logout, model allowlisting, or account-specific catalog semantics.

Rejected alternatives: cloning Codex authentication adds unsupported lifecycle behavior; treating all models as Chat Completions discards native semantics; implementing Eino/SSE abstractions here duplicates the consumer's responsibility; automatically reading OpenCode CLI auth files adds unrequested storage ownership.

## Target flow and ownership

| Component | Responsibility | Output |
| --- | --- | --- |
| Host coding agent | API key, own user agent, stable conversation ID, selected model/protocol | Proposed `Options` and request context |
| Proposed `opencodeauth.Client` | Validate configuration, constrain destinations, decorate route-specific headers | Ordinary `*http.Client`, base URL, protocol endpoints |
| Host HTTP consumer or future Eino adapter | Encode native request and decode native stream; preserve tool/reasoning state | Host-specific model result |
| OpenCode Go | Inference, routing, subscription accounting | Native JSON/SSE |

The proposed `ListModels` call uses the public catalog independently of inference session state. The proposed `DecodeHTTPError` is opt-in and consumes only unsuccessful responses. Ordinary HTTP calls retain ordinary response ownership.

## Scope, constraints, and non-goals

In scope: three inference routes, the public models route, explicit endpoint/HTTP-client injection, session context helper, immutable transport configuration, safe errors, credential-free tests/examples, and a future-consumer handoff.

Out of scope: OAuth, browser/device login, key rotation/revocation APIs, CLI credential storage, quota polling, account balance changes, automatic retries, model capability database, protocol conversion, SSE parsing, tool execution, Eino changes, deployment, or publishing a release during this planning task. Do not market Go access as unrestricted general-purpose inference. The host remains responsible for using its subscription in the service's supported coding-agent context.

No compatibility flags or migration machinery are needed locally. Rollback during implementation is reverting the relevant new package commit. No runtime data rollback exists because the library persists nothing. Adoption by Eino will use a published tag or immutable public commit, and can roll back by removing the new adapter/pin without affecting other backends.

## Assumptions, risks, and gates

- Non-blocking assumption: the user wants the same transport-library role as `codex-auth-go`, not a new Go generation SDK. This matches the reference library and keeps the later Eino work separate.
- Model lists and endpoint assignments change. The library makes no static routing claim. Live tests select explicit model IDs and protocols from current official evidence.
- Source snapshots and live service behavior differ. The older local OpenCode reference lacks the current Go Responses route. Prefer the pinned current upstream source and live results documented in 01.
- Live quota/policy/region failures do not prove a code defect. They block release verification for the affected protocol until an authorized working model/key is available; owner: implementing developer/user. Never weaken authentication or use another provider automatically to bypass that gate.
- The Eino checkout contains unrelated user edits and is behind its tracking branch. Its owner must reconcile state before implementation there. It is read-only input here.

There are no unresolved blocking planning decisions. Exact future Eino SDK selection and supported model roster are non-blocking downstream decisions owned by its maintainer. They must be resolved against the explicit-protocol request before downstream coding starts.

## External request

Canonical request: `$HOME/.agents/projects/eino-providers/requests/2026-09-08-opencode-go-provider.md`.

Owner/decision owner: maintainer of `github.com/mattsp1290/eino-providers` (Matt). Prospective consumer of this library: the requested Eino OpenCode Go adapter. Existing Eino coding-agent usage demonstrates the adapter pattern, not existing OpenCode Go adoption. The request is open, not accepted or implemented.

Effect: non-blocking for all local work packages; required for the later separate Eino integration. Local completion ends with a tested transport and documented handoff. Downstream adoption requires the maintainer's response, verified compatible public library pin, and its own provider tests. Resolve its same-filename response under `$HOME/.agents/projects/eino-providers/responses/`. Request creation alone is not an acceptance or dependency pin.

Local implementation completion does not establish Eino adoption-readiness. The separate publication/adoption gate in [06-execution-handoff.md](06-execution-handoff.md) names Matt as publication owner and gives clean-consumer pin verification commands. That future gate is outstanding until a real public artifact is verified.

## Document map

- [01-research.md](01-research.md): observed repository contracts, authoritative source pins, live probe evidence, and uncertainty.
- [02-client-and-session.md](02-client-and-session.md): WP1 public API, environment configuration, sessions, and URL policy.
- [03-http-transport.md](03-http-transport.md): WP2 credential confinement, native HTTP behavior, concurrency, and stream lifecycle.
- [04-catalog-and-errors.md](04-catalog-and-errors.md): WP3 public catalog and explicit safe HTTP error decoder.
- [05-verification-and-consumer.md](05-verification-and-consumer.md): WP4 examples, deterministic/live verification, CI, and downstream contract.
- [06-execution-handoff.md](06-execution-handoff.md): order, parallel work boundaries, release gates, and definition of done.
