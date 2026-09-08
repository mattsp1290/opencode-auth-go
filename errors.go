package opencodeauth

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Stable local errors returned when client configuration or a request cannot
// be accepted. Callers can inspect these with errors.Is.
var (
	ErrMissingAPIKey        = errors.New("opencodeauth: missing API key")
	ErrInvalidConfiguration = errors.New("opencodeauth: invalid configuration")
	ErrMissingSessionID     = errors.New("opencodeauth: missing session ID")
	ErrInvalidSessionID     = errors.New("opencodeauth: invalid session ID")
	ErrDisallowedRequest    = errors.New("opencodeauth: disallowed request")
)

// ErrorKind is the local category assigned to an unsuccessful API response.
// It deliberately does not expose an upstream error code or message.
type ErrorKind string

const (
	ErrorKindUnknown        ErrorKind = "unknown"
	ErrorKindAuthentication ErrorKind = "authentication"
	ErrorKindQuota          ErrorKind = "quota"
	ErrorKindRateLimit      ErrorKind = "rate-limit"
	ErrorKindModel          ErrorKind = "model"
	ErrorKindPolicy         ErrorKind = "policy"
)

// HTTPError describes an unsuccessful HTTP response using only stable local
// fields. RetryAfter is meaningful only when HasRetryAfter is true.
type HTTPError struct {
	StatusCode    int
	Kind          ErrorKind
	RetryAfter    time.Duration
	HasRetryAfter bool
}

func (e *HTTPError) Error() string {
	if e == nil {
		return "opencodeauth: HTTP error"
	}
	return fmt.Sprintf("opencodeauth: HTTP %d (%s)", e.StatusCode, localErrorKind(e.Kind))
}

func localErrorKind(kind ErrorKind) ErrorKind {
	switch kind {
	case ErrorKindAuthentication, ErrorKindQuota, ErrorKindRateLimit, ErrorKindModel, ErrorKindPolicy:
		return kind
	default:
		return ErrorKindUnknown
	}
}

var (
	errInvalidHTTPResponse = errors.New("opencodeauth: invalid HTTP response")
	errCatalogRequest      = errors.New("opencodeauth: model catalog request failed")
	errCatalogRead         = errors.New("opencodeauth: could not read model catalog")
	errCatalogDecode       = errors.New("opencodeauth: invalid model catalog response")
)

const maxHTTPErrorBody = 64 << 10

// httpErrorNow is kept as a variable so package tests can use a deterministic
// clock for HTTP-date Retry-After values without exposing clock policy publicly.
var httpErrorNow = time.Now

// DecodeHTTPError consumes and closes an unsuccessful response body, returning
// a bounded, typed description of the response. Successful responses are
// untouched and return nil.
func DecodeHTTPError(resp *http.Response) error {
	if resp == nil {
		return errInvalidHTTPResponse
	}
	if resp.StatusCode >= http.StatusOK && resp.StatusCode < http.StatusMultipleChoices {
		return nil
	}
	if resp.Body == nil {
		return errInvalidHTTPResponse
	}
	defer resp.Body.Close()

	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxHTTPErrorBody+1))
	if readErr != nil || len(body) > maxHTTPErrorBody {
		retry, hasRetry := parseRetryAfter(resp.Header, httpErrorNow())
		return &HTTPError{StatusCode: resp.StatusCode, Kind: ErrorKindUnknown, RetryAfter: retry, HasRetryAfter: hasRetry}
	}

	kind := classifyHTTPError(body)
	retryAfter, hasRetryAfter := parseRetryAfter(resp.Header, httpErrorNow())
	return &HTTPError{
		StatusCode:    resp.StatusCode,
		Kind:          kind,
		RetryAfter:    retryAfter,
		HasRetryAfter: hasRetryAfter,
	}
}

func classifyHTTPError(body []byte) ErrorKind {
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil || envelope == nil {
		return ErrorKindUnknown
	}
	raw, ok := envelope["error"]
	if !ok || string(raw) == "null" {
		return ErrorKindUnknown
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil || fields == nil {
		return ErrorKindUnknown
	}

	var categories []ErrorKind
	for _, name := range []string{"type", "code"} {
		rawValue, present := fields[name]
		if !present || string(rawValue) == "null" {
			continue
		}
		var value string
		if err := json.Unmarshal(rawValue, &value); err != nil {
			return ErrorKindUnknown
		}
		if value == "" {
			continue
		}
		kind, recognized := classifyHTTPErrorValue(value)
		if !recognized {
			return ErrorKindUnknown
		}
		categories = append(categories, kind)
	}
	if len(categories) == 0 {
		return ErrorKindUnknown
	}
	for _, category := range categories[1:] {
		if category != categories[0] {
			return ErrorKindUnknown
		}
	}
	return categories[0]
}

func classifyHTTPErrorValue(value string) (ErrorKind, bool) {
	switch value {
	case "AuthError", "authentication_error", "invalid_api_key":
		return ErrorKindAuthentication, true
	case "CreditsError", "MonthlyLimitError", "UserLimitError", "GoUsageLimitError", "FreeUsageLimitError", "BlackUsageLimitError", "insufficient_quota":
		return ErrorKindQuota, true
	case "RateLimitError", "rate_limit_error":
		return ErrorKindRateLimit, true
	case "ModelError":
		return ErrorKindModel, true
	case "RegionError", "DataPolicyError":
		return ErrorKindPolicy, true
	default:
		return ErrorKindUnknown, false
	}
}

func parseRetryAfter(header http.Header, now time.Time) (time.Duration, bool) {
	var values []string
	for name, entries := range header {
		if !strings.EqualFold(name, "Retry-After") {
			continue
		}
		values = append(values, entries...)
	}
	if len(values) != 1 {
		return 0, false
	}
	value := strings.TrimSpace(values[0])
	if value == "" || len(value) > 128 {
		return 0, false
	}
	if isDecimal(value) {
		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > uint64(maxRetryAfter/time.Second) {
			return 0, false
		}
		return time.Duration(seconds) * time.Second, true
	}
	when, err := http.ParseTime(value)
	if err != nil {
		return 0, false
	}
	if !when.After(now) {
		return 0, true
	}
	if when.After(now.Add(maxRetryAfter)) {
		return 0, false
	}
	delta := when.Sub(now)
	if delta < 0 || delta > maxRetryAfter {
		return 0, false
	}
	return delta, true
}

const maxRetryAfter = time.Duration(1<<63 - 1)

func isDecimal(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] < '0' || value[i] > '9' {
			return false
		}
	}
	return len(value) > 0
}
