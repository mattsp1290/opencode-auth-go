package opencodeauth

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestListModelsDecodesBoundedCatalog(t *testing.T) {
	var calls int
	client, err := NewClient(Options{
		APIKey:    "synthetic-key",
		BaseURL:   "https://catalog.example",
		UserAgent: "catalog-test/1",
		SessionID: "must-not-be-sent",
		HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			calls++
			if req.Method != http.MethodGet || req.URL.String() != "https://catalog.example/models" {
				return nil, errors.New("unexpected catalog request")
			}
			for _, name := range []string{"Authorization", "X-Api-Key", "X-Opencode-Session"} {
				if value := req.Header.Get(name); value != "" {
					return nil, errors.New("catalog request carried protected header")
				}
			}
			return catalogResponse(`{"object":"list","data":[{"id":"one","object":"model","created":123,"owned_by":"owner","future":{"x":true}},{"id":"one"}]}`), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	first, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("ListModels() error = %v", err)
	}
	second, err := client.ListModels(context.Background())
	if err != nil {
		t.Fatalf("second ListModels() error = %v", err)
	}
	if calls != 2 {
		t.Fatalf("catalog calls = %d, want 2", calls)
	}
	want := []Model{{ID: "one", Object: "model", Created: 123, OwnedBy: "owner"}, {ID: "one"}}
	if len(first) != len(want) || first[0] != want[0] || first[1] != want[1] || len(second) != len(want) {
		t.Fatalf("models = %#v, want %#v", first, want)
	}
}

func TestListModelsAcceptsEmptyListAndClosesSuccessBody(t *testing.T) {
	body := &catalogTrackingBody{Reader: strings.NewReader(`{"object":"list","data":[]}`)}
	client, err := NewClient(Options{
		APIKey:    "key",
		BaseURL:   "https://catalog.example",
		UserAgent: "catalog-test/1",
		HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	models, err := client.ListModels(context.Background())
	if err != nil || models == nil || len(models) != 0 {
		t.Fatalf("empty ListModels() = %#v, %v; want nonnil empty slice", models, err)
	}
	if body.closed != 1 {
		t.Fatalf("success body close count = %d, want 1", body.closed)
	}
}

func TestDecodeModelsRejectsInvalidShapes(t *testing.T) {
	for name, body := range map[string]string{
		"missing object":    `{"data":[]}`,
		"wrong object":      `{"object":"models","data":[]}`,
		"missing data":      `{"object":"list"}`,
		"null data":         `{"object":"list","data":null}`,
		"wrong data":        `{"object":"list","data":{}}`,
		"null object":       `{"object":null,"data":[]}`,
		"wrong object type": `{"object":3,"data":[]}`,
		"missing id":        `{"object":"list","data":[{}]}`,
		"empty id":          `{"object":"list","data":[{"id":""}]}`,
		"null id":           `{"object":"list","data":[{"id":null}]}`,
		"wrong id type":     `{"object":"list","data":[{"id":3}]}`,
		"malformed entry":   `{"object":"list","data":[1]}`,
		"wrong optional":    `{"object":"list","data":[{"id":"x","owned_by":3}]}`,
		"null optional":     `{"object":"list","data":[{"id":"x","object":null}]}`,
		"wrong timestamp":   `{"object":"list","data":[{"id":"x","created":1.5}]}`,
		"null timestamp":    `{"object":"list","data":[{"id":"x","created":null}]}`,
		"trailing json":     `{"object":"list","data":[]} {}`,
		"malformed":         `{"object":"list","data":[]`,
		"non-json":          `not-json`,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := decodeModels([]byte(body)); !errors.Is(err, errCatalogDecode) {
				t.Fatalf("decodeModels() error = %v, want fixed catalog error", err)
			}
		})
	}
}

func TestListModelsBodyClosureAndHTTPErrorPaths(t *testing.T) {
	tests := []struct {
		name     string
		status   int
		body     io.Reader
		wantHTTP bool
	}{
		{name: "decode failure", status: http.StatusOK, body: strings.NewReader(`{"object":"list","data":[`)},
		{name: "read failure", status: http.StatusOK, body: catalogErrorReader{}},
		{name: "non-json success", status: http.StatusOK, body: strings.NewReader("not-json-catalog-canary")},
		{name: "non-2xx", status: http.StatusUnauthorized, body: strings.NewReader(`{"error":{"type":"AuthError","message":"body-canary"}}`), wantHTTP: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			body := &catalogTrackingBody{Reader: tt.body}
			client, err := NewClient(Options{
				APIKey:    "key",
				BaseURL:   "https://catalog.example",
				UserAgent: "catalog-test/1",
				HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: tt.status, Body: body, Header: make(http.Header)}, nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			_, err = client.ListModels(context.Background())
			if body.closed != 1 {
				t.Fatalf("body close count = %d, want 1", body.closed)
			}
			if tt.wantHTTP {
				var typed *HTTPError
				if !errors.As(err, &typed) || typed.Kind != ErrorKindAuthentication {
					t.Fatalf("error = %T %v, want typed authentication error", err, err)
				}
				if strings.Contains(err.Error(), "body-canary") {
					t.Fatalf("response body leaked through error: %v", err)
				}
			} else if !errors.Is(err, errCatalogDecode) && tt.name != "read failure" {
				t.Fatalf("error = %v, want fixed catalog decode error", err)
			} else if tt.name == "read failure" && !errors.Is(err, errCatalogRead) {
				t.Fatalf("read error = %v, want fixed catalog read error", err)
			}
			if strings.Contains(err.Error(), "catalog-canary") {
				t.Fatalf("catalog body leaked through error: %v", err)
			}
		})
	}
}

func TestListModelsBodyLimitAndResponseErrors(t *testing.T) {
	exact := exactCatalogBody(maxCatalogBody)
	for name, body := range map[string]string{
		"exact limit": exact,
		"over limit":  exact + "x",
	} {
		t.Run(name, func(t *testing.T) {
			client, err := NewClient(Options{
				APIKey:    "key",
				BaseURL:   "https://catalog.example",
				UserAgent: "catalog-test/1",
				HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return catalogResponse(body), nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			models, err := client.ListModels(context.Background())
			if name == "exact limit" {
				if err != nil || len(models) != 0 {
					t.Fatalf("exact-limit ListModels() = %#v, %v", models, err)
				}
			} else if !errors.Is(err, errCatalogDecode) {
				t.Fatalf("over-limit error = %v, want fixed catalog error", err)
			}
		})
	}
}

func TestListModelsPreservesCancellationAndHidesTransportErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	client, err := NewClient(Options{
		APIKey:    "key",
		BaseURL:   "https://catalog.example",
		UserAgent: "catalog-test/1",
		HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(req *http.Request) (*http.Response, error) {
			<-req.Context().Done()
			return nil, req.Context().Err()
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	if _, err := client.ListModels(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled ListModels() error = %v", err)
	}

	canary := "transport-secret-canary"
	client, err = NewClient(Options{
		APIKey:    "key",
		BaseURL:   "https://catalog.example",
		UserAgent: "catalog-test/1",
		HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
			return nil, errors.New(canary)
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := client.ListModels(context.Background()); err == nil || strings.Contains(err.Error(), canary) {
		t.Fatalf("transport error leaked: %v", err)
	}
}

func TestListModelsPreservesCancellationDuringBodyRead(t *testing.T) {
	for _, want := range []error{context.Canceled, context.DeadlineExceeded} {
		t.Run(want.Error(), func(t *testing.T) {
			var ctx context.Context
			var cancel context.CancelFunc
			if want == context.DeadlineExceeded {
				ctx, cancel = context.WithTimeout(context.Background(), 20*time.Millisecond)
			} else {
				ctx, cancel = context.WithCancel(context.Background())
			}
			defer cancel()
			body := &catalogContextBody{ctx: ctx, started: make(chan struct{})}
			client, err := NewClient(Options{
				APIKey:    "key",
				BaseURL:   "https://catalog.example",
				UserAgent: "catalog-test/1",
				HTTPClient: &http.Client{Transport: catalogRoundTripFunc(func(*http.Request) (*http.Response, error) {
					return &http.Response{StatusCode: http.StatusOK, Body: body, Header: make(http.Header)}, nil
				})},
			})
			if err != nil {
				t.Fatal(err)
			}
			if want == context.Canceled {
				go func() {
					<-body.started
					cancel()
				}()
			}
			_, got := client.ListModels(ctx)
			if !errors.Is(got, want) {
				t.Fatalf("body cancellation error = %v, want %v", got, want)
			}
			if body.closed != 1 {
				t.Fatalf("body close count = %d, want 1", body.closed)
			}
		})
	}
}

type catalogRoundTripFunc func(*http.Request) (*http.Response, error)

func (f catalogRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func catalogResponse(body string) *http.Response {
	return &http.Response{
		StatusCode: http.StatusOK,
		Status:     "200 OK",
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func exactCatalogBody(size int) string {
	prefix := `{"object":"list","data":[],"unknown":"`
	suffix := `"}`
	return prefix + strings.Repeat("x", size-len(prefix)-len(suffix)) + suffix
}

type catalogTrackingBody struct {
	io.Reader
	closed int
}

func (b *catalogTrackingBody) Close() error {
	b.closed++
	return nil
}

type catalogErrorReader struct{}

func (catalogErrorReader) Read([]byte) (int, error) { return 0, errors.New("catalog body canary") }

type catalogContextBody struct {
	ctx       context.Context
	started   chan struct{}
	startOnce sync.Once
	closed    int
}

func (b *catalogContextBody) Read([]byte) (int, error) {
	b.startOnce.Do(func() { close(b.started) })
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *catalogContextBody) Close() error {
	b.closed++
	return nil
}
