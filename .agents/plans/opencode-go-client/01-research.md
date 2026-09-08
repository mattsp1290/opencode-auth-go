# Research and evidence

Research date: 2026-09-08. Facts below were inspected during planning. Proposed API decisions live in the work files and are not upstream contracts.

## Portable checkout resolution

Resolve the current repository with `git rev-parse --show-toplevel`. For external source inspection, set `CODEX_AUTH_GO_DIR` to a checkout of `github.com/mattsp1290/codex-auth-go` and `EINO_PROVIDERS_DIR` to a checkout of `github.com/mattsp1290/eino-providers`. Optional `OPENCODE_SOURCE_DIR` denotes a checkout of `github.com/anomalyco/opencode`. These are documented local-resolution variables, not application configuration. Do not assume sibling directories exist on another machine.

## Repository evidence

| Repository and revision | Evidence | Design consequence |
| --- | --- | --- |
| Local `487c158371ef0675df1fba95955574859ceb8788` | `README.md` describes Go subscription use; `LICENSE` and `.gitignore` are the only other tracked files | All Go APIs and automation in this plan are new |
| `codex-auth-go` `6ca92f5e6dd8a71ff7fe710d967438b55bb7a7aa` | `client.go`: `Client`, `Options`, `NewClient`; `public.go`: `HTTPClient`; `transport.go`: cloned requests and streaming body passthrough; `catalog.go`: bounded catalog decoding | Copy responsibility boundaries, testability, and explicit client ergonomics |
| Same sibling | OAuth/login/device/refresh/store modules; `models.go`: `IsCodexAllowed`; README account catalog and redirect behavior | These mechanisms are Codex-specific and are not local requirements |
| Same sibling | `go.mod` and `.github/workflows/ci.yml` use Go 1.26.8; untracked `.agents/plans/codex-endpoint-and-credpath-options/` | Do not copy its toolchain requirement or mutate its planning files |
| `eino-providers` `3e0069d028bc946deaa96ea2dd6bff76b4118c38` | `go.mod`: Go 1.25.5, Eino v0.8.13, `eino-ext/components/model/openai` v0.1.13, `codex-auth-go` v0.1.0 | Keep initial local Go floor at 1.25.5 and the library independent of Eino |
| Same Eino checkout | `openaicodex/chatmodel.go`: `ChatModelConfig`, `NewChatModelWithHTTPClient`, `WithTools`, `Generate`, `Stream`; `responses.go` translates Eino messages/SSE | Auth library returns HTTP client; Eino owns model/schema/stream integration |
| Same Eino checkout | `options.go`, `provider.go`, `errors.go`; `.agents/requests/local-symphony-tool-calling-chat-models.md`; `.agents/requests/incremental-toolcall-args-streaming/request.md` | Prefer backend-specific ToolCallingChatModel constructor and exact tool fragment/continuation tests |

The Eino worktree has a modified `go.sum`, untracked `.agents/plans/incremental-toolcall-args-streaming/` and `.agents/requests/`, and is three commits behind its tracking branch. These are observations, not permission to reconcile it here. Both siblings have Beads guidance in `AGENTS.md`; no tasks or code were changed there. The canonical external request directory for Eino did not exist; the new request follows representative sibling request formats under `$HOME/.agents/projects/`.

## Authoritative upstream sources

Current upstream snapshot: `anomalyco/opencode@dff8fbc149fb7492e4f07b713ac31ea70d9a541c`, authored 2026-09-08. Source paths below are repository-relative and linked to that immutable revision.

- [Go documentation](https://opencode.ai/docs/go/): current endpoint assignments and coding-agent identification expectations. Use a host-specific user agent and a stable `x-opencode-session` per conversation. The catalog changes. Console-controlled balance fallback is a service setting, not a client-side fallback feature.
- [Chat route](https://github.com/anomalyco/opencode/blob/dff8fbc149fb7492e4f07b713ac31ea70d9a541c/packages/console/app/src/routes/zen/go/v1/chat/completions.ts): parses bearer authorization and native Chat Completions request fields.
- [Messages route](https://github.com/anomalyco/opencode/blob/dff8fbc149fb7492e4f07b713ac31ea70d9a541c/packages/console/app/src/routes/zen/go/v1/messages.ts): parses `x-api-key` and Anthropic-format request fields.
- [Responses route](https://github.com/anomalyco/opencode/blob/dff8fbc149fb7492e4f07b713ac31ea70d9a541c/packages/console/app/src/routes/zen/go/v1/responses.ts): parses bearer authorization and Responses-format request fields.
- [Catalog route](https://github.com/anomalyco/opencode/blob/dff8fbc149fb7492e4f07b713ac31ea70d9a541c/packages/console/app/src/routes/zen/go/v1/models.ts) and [response builder](https://github.com/anomalyco/opencode/blob/dff8fbc149fb7492e4f07b713ac31ea70d9a541c/packages/console/app/src/routes/zen/util/modelsHandler.ts): public model-list route and OpenAI-style minimal entry shape.
- [Gateway handler](https://github.com/anomalyco/opencode/blob/dff8fbc149fb7492e4f07b713ac31ea70d9a541c/packages/console/app/src/routes/zen/util/handler.ts): model/format validation, auth/billing, errors, optional retry-after, session-based routing, and response passthrough.
- [Request body transformer](https://github.com/anomalyco/opencode/blob/dff8fbc149fb7492e4f07b713ac31ea70d9a541c/packages/console/app/src/routes/zen/util/requestBody.ts): model substitution and streaming usage options in the gateway. This is gateway behavior, not a transformation the local library should duplicate.

The older local reference at `ae53163cad0048b2351e258699e815f4f2110807` lacks `packages/console/app/src/routes/zen/go/v1/responses.ts`. It establishes historical patterns only. A route or conversion helper in an old checkout does not establish present live model compatibility.

## Live research procedure and results

Temporary Python prototypes sent bounded synthetic coding requests through curl, using the authorized `OPENCODE_GO_API_KEY` in memory. Credentials were never included in command arguments, probe reports, plan text, or subagent prompts. The process used its own research user agent and an unprinted conversation identifier. Inference calls had a 45-second request timeout and 512-token output ceilings. Tool results were fixed synthetic Go source, not files read from the user's projects. No real tools were executed.

The report retained HTTP status, field names, event types, counts, token usage, and completion reasons. It omitted response/conversation/tool identifiers, text/reasoning content, and credentials. Temporary prototype files are not required implementation inputs and are not library artifacts.

| Probe | Model / authentication | Observed result |
| --- | --- | --- |
| Public and authenticated GET models | No key / bearer key | Both HTTP 200, 35 entries; fields only `id`, `object`, `created`, `owned_by` |
| Chat nonstream and stream | `mimo-v2.5`, bearer | Both HTTP 200; normal finish `stop`; SSE included reasoning fields, usage, and `[DONE]` |
| Messages nonstream and stream | `minimax-m2.7`, `x-api-key`, version `2023-06-01` | Both HTTP 200; thinking/text blocks; SSE block/message events; terminal `message_stop` |
| Responses nonstream and stream | `gpt-5.6-luna`, bearer, `store:false` | Both HTTP 200; completed response; event-typed SSE ending in `response.completed` |
| Chat tool call then result | Same Chat model; replay whole assistant message | Both HTTP 200; one tool call, `tool_calls` finish, then normal text completion |
| Messages tool call then result | Same Messages model; replay assistant blocks including thinking | Both HTTP 200; one `tool_use`, then `end_turn` |
| Responses tool call then result | Same Responses model; replay output items and correlated `function_call_output` | Both HTTP 200; one `function_call`, then completed reasoning/message output |

The SSE probe observed 54 data records for Chat, 15 for Messages, and 12 for Responses. These counts are observations, not expected test constants. Chat records included a nonstandard trailing record; Responses included a `ping`. Do not build a transport that stops reading at a guessed event or buffers to decode JSON.

The first public request using Python's default HTTP user agent received 403; a subsequent request using the research user agent returned 200. The client implementation also changed between these attempts, so this does not isolate the reason for 403. Follow the documented identification contract regardless.

Limitations: probes establish current success for one model per protocol, not all 35 models, SDK compatibility, cancellation behavior, rate-limit behavior, incremental tool-argument streaming, or future entitlement. Tool-loop probes were nonstreaming and replayed native response state. Streaming tool fragmentation must be proved by deterministic tests and later Eino integration tests. HTTP 200 from the public catalog does not authenticate the supplied key.

## Error evidence and implications

The pinned handler maps `AuthError`, `CreditsError`, `MonthlyLimitError`, `UserLimitError`, and `ModelError` to HTTP 401. It maps rate and Go usage-limit errors to 429, optionally with `Retry-After`. Region/data-policy errors use 403. Provider errors can use native JSON envelopes and statuses. Consequently, status 401 alone must not mean “invalid credentials”, and 429 alone does not distinguish a short rate limit from subscription exhaustion.

Native reasoning and tool state differs by protocol. The live Chat assistant included `reasoning` and `reasoning_details`; Messages included thinking blocks; Responses returned native output items. A raw HTTP library can preserve all of them without claiming to understand them. The future Eino adapter needs protocol-specific continuity and fragment tests.

The models endpoint provides no endpoint, context-window, capability, or entitlement fields. Keep new model IDs visible in the catalog. Let the host select an explicit protocol using current official information. Do not infer protocol from prefixes or perform speculative inference requests automatically.
