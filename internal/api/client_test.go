package api

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode"

	"github.com/magarcia/cuentica-cli/internal/testutil"
)

func TestDoSendsRequestAndPreservesResponseBytes(t *testing.T) {
	wantBody := []byte(`{"name":"A & B"}`)
	wantResponse := []byte{0, 1, 2, 255, '\n'}
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/customer/42" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		if values := r.URL.Query()["tag"]; len(values) != 2 || values[0] != "one two" || values[1] != "a&b" {
			t.Errorf("query = %q", r.URL.RawQuery)
		}
		for name, want := range map[string]string{
			"X-AUTH-TOKEN": "test-secret",
			"Accept":       "application/json",
			"Content-Type": "application/json",
			"User-Agent":   "cuentica-cli",
		} {
			if got := r.Header.Get(name); got != want {
				t.Errorf("%s = %q; want %q", name, got, want)
			}
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || !bytes.Equal(body, wantBody) {
			t.Errorf("body = %q, error = %v", body, err)
		}
		w.Header().Set("X-Result", "preserved")
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write(wantResponse)
	}))
	defer server.Close()
	client := server.client(t)
	response, err := client.Do(context.Background(), http.MethodPost, "/customer/42", url.Values{"tag": {"one two", "a&b"}}, wantBody)
	if err != nil {
		t.Fatal(err)
	}
	if response.Status != http.StatusCreated || response.Header.Get("X-Result") != "preserved" || !bytes.Equal(response.Body, wantResponse) {
		t.Fatalf("unexpected response: %#v", response)
	}
}

func TestDoOmitsContentTypeWithoutBody(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Content-Type"); got != "" {
			t.Errorf("Content-Type = %q for request without body", got)
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer server.Close()
	_, err := server.client(t).Do(context.Background(), http.MethodGet, "/company", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
}

func TestDoReturnsSanitizedHTTPError(t *testing.T) {
	for _, status := range []int{http.StatusForbidden, http.StatusTooManyRequests} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Retry-After", "60")
				w.WriteHeader(status)
				_, _ = io.WriteString(w, "test-secret\x1b[31m\n"+strings.Repeat("界", 5000))
			}))
			defer server.Close()
			response, err := server.client(t).Do(context.Background(), http.MethodGet, "/company", nil, nil)
			var apiError *Error
			if !errors.As(err, &apiError) {
				t.Fatalf("error = %v; want *Error", err)
			}
			if response == nil || apiError.Status != status || apiError.RetryAfter != "60" {
				t.Fatalf("unexpected error response: %#v, %#v", response, apiError)
			}
			if strings.Contains(apiError.Body, "test-secret") || !strings.Contains(apiError.Body, "[REDACTED]") || strings.ContainsFunc(apiError.Body, unicode.IsControl) || len([]rune(apiError.Body)) != maxErrorCharacters {
				t.Fatalf("error body was not bounded and sanitized: %.80q", apiError.Body)
			}
			if strings.Contains(apiError.Error(), apiError.Body) || strings.Contains(apiError.Error(), "test-secret") {
				t.Fatalf("error text exposes body: %s", apiError.Error())
			}
			if string(response.Body) != apiError.Body {
				t.Fatal("Response.Body differs from the sanitized API error")
			}
		})
	}
}

func TestDoRedactsJSONEscapedSecrets(t *testing.T) {
	for _, test := range []struct{ name, token, body string }{
		{"unicode", "test-secret", `{"error":"\u0074\u0065st-\u0073ecret"}`},
		{"uppercase hex", "a/b", `{"error":"\u0061\u002F\u0062"}`},
		{"escaped slash", "a/b", `{"error":"a\/b"}`},
		{"escaped quote", `a"b`, `{"error":"a\"b"}`},
		{"escaped backslash", `a\b`, `{"error":"a\\b"}`},
		{"escaped non-BMP", "a😀b", `{"error":"a\uD83D\uDE00b"}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusForbidden)
				_, _ = io.WriteString(w, test.body)
			}))
			defer server.Close()
			client, err := New(server.URL, test.token, time.Second)
			if err != nil {
				t.Fatal(err)
			}
			client.http.Transport = server.transport
			_, err = client.Do(context.Background(), http.MethodGet, "/company", nil, nil)
			var apiError *Error
			if !errors.As(err, &apiError) || !strings.Contains(apiError.Body, "[REDACTED]") {
				t.Fatalf("escaped secret was not redacted: %v, %#v", err, apiError)
			}
		})
	}
}

func TestNewRejectsUnsafeBaseURLs(t *testing.T) {
	for _, base := range []string{
		"", "/relative", "http://api.cuentica.com", "http://localhost:8080", "https://user:pass@api.cuentica.com",
		"https://api.cuentica.com/api", "https://api.cuentica.com?secret=1", "https://api.cuentica.com?", "https://api.cuentica.com#fragment",
		"https://api.cuentica.com#", "https://api.cuentica.com:", "https://api.cuentica.com:0", "https://api.cuentica.com:65536", "https://api.cuentica.com:port",
	} {
		t.Run(base, func(t *testing.T) {
			if _, err := New(base, "test-secret", time.Second); err == nil {
				t.Fatalf("accepted unsafe base URL %q", base)
			}
		})
	}
	for _, base := range []string{DefaultBaseURL, DefaultBaseURL + "/", "http://127.0.0.1:8080", "http://[::1]:8080"} {
		if _, err := New(base, "test-secret", time.Second); err != nil {
			t.Errorf("rejected safe base URL %q: %v", base, err)
		}
	}
	for _, timeout := range []time.Duration{0, -time.Second} {
		if _, err := New(DefaultBaseURL, "test-secret", timeout); err == nil {
			t.Errorf("accepted timeout %v", timeout)
		}
	}
}

func TestDoRejectsUnsafePathsBeforeMakingRequests(t *testing.T) {
	client := newTestClient(t, "http://127.0.0.1:1")
	for _, path := range []string{
		"", "customer", "https://elsewhere.test/company", "//elsewhere.test/company", "/../company", "/customer/./company",
		"/%2e%2e/company", "/%252e%252e/company", "/customer%2f..%2fcompany", "/%2felsewhere.test", "/%252felsewhere.test",
		"/customer\\company", "/customer%5ccompany", "/customer?x=1", "/customer?", "/customer#fragment", "/customer#", "/customer%00", "/customer%09",
	} {
		t.Run(path, func(t *testing.T) {
			_, err := client.Do(context.Background(), http.MethodGet, path, nil, nil)
			if err == nil || strings.Contains(err.Error(), "connection refused") {
				t.Fatalf("unsafe path was not rejected before transport: %v", err)
			}
		})
	}
}

func TestDoDoesNotFollowRedirects(t *testing.T) {
	var requests atomic.Int32
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		http.Redirect(w, r, "/redirected", http.StatusFound)
	}))
	defer server.Close()
	response, err := server.client(t).Do(context.Background(), http.MethodGet, "/company", nil, nil)
	var apiError *Error
	if requests.Load() != 1 || response == nil || response.Status != http.StatusFound || !errors.As(err, &apiError) {
		t.Fatalf("redirect was not returned directly: requests=%d, response=%#v, error=%v", requests.Load(), response, err)
	}
}

func TestDoHonorsTimeoutAndContextCancellation(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()
	client, err := New(server.URL, "test-secret", 20*time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	client.http.Transport = server.transport
	if _, err := client.Do(context.Background(), http.MethodGet, "/company", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("timeout error = %v; want context.DeadlineExceeded", err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Do(ctx, http.MethodGet, "/company", nil, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancellation error = %v; want context.Canceled", err)
	}
}

func TestDoBoundsResponseSize(t *testing.T) {
	server := newTestServer(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "123456789")
	}))
	defer server.Close()
	client := server.client(t)
	client.responseLimit = 8
	if _, err := client.Do(context.Background(), http.MethodGet, "/company", nil, nil); err == nil || !strings.Contains(err.Error(), "8-byte limit") {
		t.Fatalf("oversized response error = %v", err)
	}
}

func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	client, err := New(baseURL, "test-secret", time.Second)
	if err != nil {
		t.Fatal(err)
	}
	return client
}

type testServer struct {
	URL       string
	transport testutil.RoundTripCloser
}

func newTestServer(t *testing.T, handler http.Handler) *testServer {
	t.Helper()
	return &testServer{URL: "http://127.0.0.1", transport: testutil.Transport(handler)}
}

func (s *testServer) client(t *testing.T) *Client {
	t.Helper()
	client := newTestClient(t, s.URL)
	client.http.Transport = s.transport
	return client
}

func (s *testServer) Close() {
	_ = s.transport.Close()
}
