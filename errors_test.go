package opencodeauth

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestDecodeHTTPErrorClassifiesKnownValues(t *testing.T) {
	tests := []struct {
		name string
		body string
		want ErrorKind
	}{
		{name: "gateway auth", body: `{"error":{"type":"AuthError"}}`, want: ErrorKindAuthentication},
		{name: "gateway quota", body: `{"error":{"type":"CreditsError"}}`, want: ErrorKindQuota},
		{name: "gateway monthly quota", body: `{"error":{"type":"MonthlyLimitError"}}`, want: ErrorKindQuota},
		{name: "gateway user quota", body: `{"error":{"type":"UserLimitError"}}`, want: ErrorKindQuota},
		{name: "gateway go quota", body: `{"error":{"type":"GoUsageLimitError"}}`, want: ErrorKindQuota},
		{name: "gateway free quota", body: `{"error":{"type":"FreeUsageLimitError"}}`, want: ErrorKindQuota},
		{name: "gateway black quota", body: `{"error":{"type":"BlackUsageLimitError"}}`, want: ErrorKindQuota},
		{name: "gateway rate", body: `{"error":{"type":"RateLimitError"}}`, want: ErrorKindRateLimit},
		{name: "gateway model", body: `{"error":{"type":"ModelError"}}`, want: ErrorKindModel},
		{name: "gateway policy", body: `{"error":{"type":"RegionError"}}`, want: ErrorKindPolicy},
		{name: "gateway data policy", body: `{"error":{"type":"DataPolicyError"}}`, want: ErrorKindPolicy},
		{name: "native type", body: `{"error":{"type":"authentication_error"}}`, want: ErrorKindAuthentication},
		{name: "native code", body: `{"error":{"code":"invalid_api_key"}}`, want: ErrorKindAuthentication},
		{name: "native quota", body: `{"error":{"code":"insufficient_quota"}}`, want: ErrorKindQuota},
		{name: "native rate", body: `{"error":{"code":"rate_limit_error"}}`, want: ErrorKindRateLimit},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := DecodeHTTPError(errorResponse(http.StatusUnauthorized, tt.body))
			var typed *HTTPError
			if !errors.As(err, &typed) {
				t.Fatalf("DecodeHTTPError() error = %T %v, want HTTPError", err, err)
			}
			if typed.Kind != tt.want {
				t.Fatalf("Kind = %q, want %q", typed.Kind, tt.want)
			}
			if strings.Contains(err.Error(), tt.body) {
				t.Fatalf("error exposed body: %v", err)
			}
		})
	}
}

func TestDecodeHTTPErrorUnknownAndConflicting(t *testing.T) {
	for _, body := range []string{
		`{"error":{"type":"unknown-canary"}}`,
		`{"error":{"type":"AuthError","code":"ModelError"}}`,
		`{"error":{"type":3}}`,
		`{"error":{"type":null,"code":null}}`,
		`not-json`,
	} {
		err := DecodeHTTPError(errorResponse(http.StatusUnauthorized, body))
		var typed *HTTPError
		if !errors.As(err, &typed) || typed.Kind != ErrorKindUnknown {
			t.Errorf("body %q => %#v, want typed unknown", body, err)
		}
	}
	for _, status := range []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusTooManyRequests} {
		err := DecodeHTTPError(errorResponse(status, `{"error":{"type":"unknown-canary"}}`))
		var typed *HTTPError
		if !errors.As(err, &typed) || typed.StatusCode != status || typed.Kind != ErrorKindUnknown {
			t.Errorf("status %d => %#v", status, err)
		}
	}
	canary := "api-key-canary-secret"
	err := DecodeHTTPError(errorResponse(http.StatusUnauthorized, `{"error":{"type":"unknown-code","message":"`+canary+`"},"metadata":{"request_id":"`+canary+`"}}`))
	var typed *HTTPError
	if !errors.As(err, &typed) || typed.Kind != ErrorKindUnknown || typed.StatusCode != http.StatusUnauthorized {
		t.Fatalf("canary response = %#v", err)
	}
	if strings.Contains(err.Error(), canary) || strings.Contains(typed.Error(), "unknown-code") {
		t.Fatalf("response-derived data leaked: %v", err)
	}
}

func TestDecodeHTTPErrorRetryAfter(t *testing.T) {
	originalNow := httpErrorNow
	httpErrorNow = func() time.Time { return time.Date(2026, 9, 8, 12, 0, 0, 0, time.UTC) }
	defer func() { httpErrorNow = originalNow }()

	tests := []struct {
		name   string
		header []string
		want   time.Duration
		has    bool
	}{
		{name: "seconds", header: []string{"7"}, want: 7 * time.Second, has: true},
		{name: "past date", header: []string{"Tue, 08 Sep 2026 11:59:00 GMT"}, want: 0, has: true},
		{name: "future date", header: []string{"Tue, 08 Sep 2026 12:00:03 GMT"}, want: 3 * time.Second, has: true},
		{name: "negative", header: []string{"-1"}},
		{name: "overflow", header: []string{"999999999999999999999999999999"}},
		{name: "oversized", header: []string{strings.Repeat("0", 129)}},
		{name: "unrepresentable seconds", header: []string{"9223372037"}},
		{name: "duplicate", header: []string{"1", "2"}},
		{name: "duplicate casing", header: []string{"1"}},
		{name: "malformed", header: []string{"tomorrow"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			resp := errorResponse(http.StatusTooManyRequests, `{}`)
			resp.Header["Retry-After"] = tt.header
			if tt.name == "duplicate casing" {
				resp.Header["rEtRy-AfTeR"] = []string{"1"}
			}
			err := DecodeHTTPError(resp)
			var typed *HTTPError
			if !errors.As(err, &typed) {
				t.Fatal(err)
			}
			if typed.HasRetryAfter != tt.has || typed.RetryAfter != tt.want {
				t.Fatalf("retry = %v/%v, want %v/%v", typed.RetryAfter, typed.HasRetryAfter, tt.want, tt.has)
			}
		})
	}
}

func TestDecodeHTTPErrorBodyBoundsAndOwnership(t *testing.T) {
	for name, body := range map[string]io.Reader{
		"oversized":  strings.NewReader(strings.Repeat("x", maxHTTPErrorBody+1)),
		"read error": errorReader{},
		"truncated":  &partialErrorReader{remaining: maxHTTPErrorBody / 2},
	} {
		t.Run(name, func(t *testing.T) {
			tracked := &trackedErrorBody{Reader: body}
			resp := &http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: tracked}
			err := DecodeHTTPError(resp)
			var typed *HTTPError
			if !errors.As(err, &typed) || typed.Kind != ErrorKindUnknown {
				t.Fatalf("error = %T %v, want typed unknown", err, err)
			}
			if tracked.closed != 1 {
				t.Fatalf("body close count = %d, want 1", tracked.closed)
			}
		})
	}

	for name, reader := range map[string]io.Reader{
		"exactly 64 KiB": strings.NewReader(strings.Repeat("x", maxHTTPErrorBody)),
		"over 64 KiB":    strings.NewReader(strings.Repeat("x", maxHTTPErrorBody+1)),
	} {
		t.Run(name, func(t *testing.T) {
			body := &countingErrorBody{Reader: reader}
			err := DecodeHTTPError(&http.Response{StatusCode: http.StatusBadGateway, Header: make(http.Header), Body: body})
			var typed *HTTPError
			if !errors.As(err, &typed) || typed.Kind != ErrorKindUnknown {
				t.Fatalf("error = %T %v, want typed unknown", err, err)
			}
			wantRead := maxHTTPErrorBody
			if name == "over 64 KiB" {
				wantRead++
			}
			if body.readBytes != wantRead {
				t.Fatalf("read bytes = %d, want %d", body.readBytes, wantRead)
			}
		})
	}

	stream := &trackedErrorBody{Reader: strings.NewReader("stream")}
	if err := DecodeHTTPError(&http.Response{StatusCode: http.StatusOK, Body: stream}); err != nil {
		t.Fatalf("2xx DecodeHTTPError() = %v", err)
	}
	if stream.closed != 0 || stream.reads != 0 {
		t.Fatalf("2xx body touched: reads=%d closes=%d", stream.reads, stream.closed)
	}
	if _, err := io.ReadAll(stream); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeHTTPErrorInvalidResponse(t *testing.T) {
	if err := DecodeHTTPError(nil); !errors.Is(err, errInvalidHTTPResponse) {
		t.Fatalf("nil response error = %v", err)
	}
	if err := DecodeHTTPError(&http.Response{StatusCode: http.StatusBadGateway}); !errors.Is(err, errInvalidHTTPResponse) {
		t.Fatalf("missing body error = %v", err)
	}
	if err := DecodeHTTPError(&http.Response{StatusCode: http.StatusNoContent}); err != nil {
		t.Fatalf("2xx missing body error = %v, want nil", err)
	}
}

func errorResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body))}
}

type errorReader struct{}

func (errorReader) Read([]byte) (int, error) { return 0, errors.New("read canary") }

type partialErrorReader struct {
	remaining int
}

func (r *partialErrorReader) Read(p []byte) (int, error) {
	if r.remaining == 0 {
		return 0, errors.New("truncated body canary")
	}
	if len(p) > r.remaining {
		p = p[:r.remaining]
	}
	for i := range p {
		p[i] = 'x'
	}
	r.remaining -= len(p)
	return len(p), nil
}

type trackedErrorBody struct {
	io.Reader
	closed int
	reads  int
}

type countingErrorBody struct {
	io.Reader
	readBytes int
}

func (b *countingErrorBody) Read(p []byte) (int, error) {
	n, err := b.Reader.Read(p)
	b.readBytes += n
	return n, err
}

func (b *countingErrorBody) Close() error { return nil }

func (b *trackedErrorBody) Read(p []byte) (int, error) {
	b.reads++
	return b.Reader.Read(p)
}

func (b *trackedErrorBody) Close() error {
	b.closed++
	return nil
}
