# WP3 — Public catalog and explicit HTTP error decoding

Prerequisite: WP1 contracts. This package can be implemented alongside WP2 using a fake injected RoundTripper. Integration through WP2 must pass before completion.

## Change surface

New/proposed root files `catalog.go`, `catalog_test.go`, `errors_test.go`; extend proposed `errors.go` and `client.go` from WP1. Existing parent for all new paths: `.`. New symbols:

- `Model` with `ID string`, `Object string`, `Created int64`, `OwnedBy string`.
- `(*Client).ListModels(context.Context) ([]Model, error)`.
- `DecodeHTTPError(*http.Response) error`.
- `HTTPError` with `StatusCode int`, `Kind ErrorKind`, `RetryAfter time.Duration`, `HasRetryAfter bool`.
- `ErrorKind` as a string type, with `ErrorKindUnknown="unknown"`, `ErrorKindAuthentication="authentication"`, `ErrorKindQuota="quota"`, `ErrorKindRateLimit="rate-limit"`, `ErrorKindModel="model"`, and `ErrorKindPolicy="policy"` constants.

The public types are a local contract, not aliases of unbounded backend JSON. Do not include API keys, raw response bodies, free-form upstream messages, workspace metadata, request IDs, or session IDs in the error type.

## Catalog behavior

Issue one GET to `<BaseURL>/models` for each call using the library HTTP client and no authorization/session headers. Honor request cancellation. Do not cache, serialize calls, refresh credentials, send Codex `client_version`, or filter by account.

Decode a bounded response: maximum 8 MiB plus a one-byte over-limit check. Require a JSON object with `object:"list"` and non-null array `data`; accept an empty array. Each entry requires a nonempty string `id`; validate known fields' JSON types when present, while tolerating unknown fields. Preserve IDs as supplied and entry order, including duplicate IDs. Optional known fields may be absent and use zero values. Reject malformed entries and trailing JSON as a whole rather than returning partial results. Close the response body on every path.

On non-2xx, use the explicit decoder below. Never include catalog JSON or response-derived strings in errors. JSON/body-read errors use fixed local descriptions; preserve `context.Canceled`/`DeadlineExceeded` identity when relevant. Do not surface arbitrary injected transport error strings through the high-level catalog API; use safe local wrapping or classification that preserves cancellation without exposing their text.

Created timestamps are reported metadata, not a release date or model ordering signal. The observed gateway synthesizes them at response generation. There is no catalog field for protocol, context limits, supported tools, pricing, or current usable subscription quota. Do not invent those fields in this API.

## Error decoder and ownership

The transport itself returns all HTTP responses unchanged. `DecodeHTTPError` is a deliberate consumer helper. A nil response or missing body on an unsuccessful response returns a fixed local error. For 2xx, return nil without reading or closing the body. For non-2xx (including redirects), consume at most 64 KiB plus one size-check byte and close the body exactly once; return a typed `*HTTPError` even if the body is malformed, oversized, HTML, or its read fails. Classification then defaults to unknown. Do not drain arbitrarily large bodies.

Parse only bounded known envelope fields `error.type` and `error.code`. Map exact recognized codes to local categories and discard all raw values. The initial gateway map is:

| Exact type | Kind |
| --- | --- |
| `AuthError` | Authentication |
| `CreditsError`, `MonthlyLimitError`, `UserLimitError`, `GoUsageLimitError`, `FreeUsageLimitError`, `BlackUsageLimitError` | Quota |
| `RateLimitError` | Rate limit |
| `ModelError` | Model |
| `RegionError`, `DataPolicyError` | Policy |

Also recognize exact common native types/codes `authentication_error` and `invalid_api_key` as authentication, `rate_limit_error` as rate limit, and `insufficient_quota` as quota. Missing/null fields contribute no category. If all supplied nonempty type/code fields are recognized and agree, use that category; if any supplied nonempty field is unknown, fields conflict, or neither yields a category, use unknown. Do not classify solely by 401, 403, or 429, parse human messages, or expose arbitrary codes as public strings. Always preserve StatusCode independently.

Parse `Retry-After` as nonnegative delta seconds or HTTP date relative to an internal injectable clock, regardless of category. Set HasRetryAfter only for valid, representable values; past valid dates produce zero. Malformed, negative, duplicate/conflicting, oversized, or overflowing values are absent. Error() contains only a fixed prefix, status number, and local category, with no server content. The helper does not sleep, retry, erase keys, change billing settings, or claim a key is expired.

## Acceptance and tests

Catalog tests cover actual observed shape, empty list, future unknown fields/IDs, missing/wrong/null data, malformed entry types, missing IDs, duplicates/order, truncation, extra JSON, exactly-at/over size limit, unknown optional fields, non-JSON success, cancellation, and body closure. Assert fresh independent requests and zero authorization/session headers. A catalog success must not be exposed as account verification.

Error tests cover each known mapping, multiple 401 categories, unknown 401/429, native envelope forms, conflicting fields, non-JSON errors, oversized/truncated bodies, read errors, and Retry-After seconds/date/invalid/overflow cases. Include synthetic canary credentials in body, metadata, unknown codes, and injected transport errors; no returned error text or typed fields may contain them. Verify `errors.As` for HTTPError. Confirm 2xx streaming bodies are untouched by the decoder.

Run `go test ./...`, `go test -race ./...`, and `go vet ./...`. No quota API, model allowlist, automatic rate-limit retry, or account-status method is part of this package.
