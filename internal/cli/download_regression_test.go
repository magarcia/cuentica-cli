package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/magarcia/cuentica-cli/internal/catalog"
)

func TestBinaryResponsesPreserveBase64JSONLiterals(t *testing.T) {
	for _, encoded := range []string{"1234", "null"} {
		for _, mode := range []string{"json", "text", "save"} {
			t.Run(encoded+"/"+mode, func(t *testing.T) {
				app, stdout, stderr, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
					return testResponse(http.StatusOK, encoded), nil
				})
				args := []string{"document-attachment", "get", "--id", "123"}
				if mode == "save" {
					args = append(args, "--save", "-")
				} else {
					args = append(args, "--output", mode)
				}
				if code := app.Run(context.Background(), args); code != 0 {
					t.Fatalf("exit = %d; stderr = %s", code, stderr)
				}
				switch mode {
				case "save":
					want, err := base64.StdEncoding.DecodeString(encoded)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(stdout.Bytes(), want) {
						t.Errorf("download = %x; want %x", stdout.Bytes(), want)
					}
				case "json":
					var got string
					if err := json.Unmarshal(stdout.Bytes(), &got); err != nil || got != encoded {
						t.Errorf("JSON output = %q; want string %q; error = %v", stdout, encoded, err)
					}
				case "text":
					if stdout.String() != encoded+"\n" {
						t.Errorf("text output = %q; want %q", stdout, encoded+"\n")
					}
				}
			})
		}
	}
}

func TestMalformedBinaryEnvelopesFail(t *testing.T) {
	for _, body := range []string{`"1234`, `{"data":"1234"`} {
		for _, save := range []bool{false, true} {
			mode := "response"
			if save {
				mode = "save"
			}
			t.Run(body+"/"+mode, func(t *testing.T) {
				app, stdout, stderr, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
					response := testResponse(http.StatusOK, body)
					response.Header.Set("Content-Type", "text/plain")
					return response, nil
				})
				args := []string{"document-attachment", "get", "--id", "123", "--output", "json"}
				code := "API-002"
				if save {
					args = append(args, "--save", "-")
					code = "API-004"
				}
				if exit := app.Run(context.Background(), args); exit != 1 || stdout.Len() != 0 {
					t.Fatalf("exit = %d; stdout = %q; stderr = %s", exit, stdout, stderr)
				}
				assertExecuteErrorCode(t, stderr.Bytes(), code)
			})
		}
	}
}

func TestAllPagesLimitsCombinedResponseBytes(t *testing.T) {
	const body = `[{"id":1}]`
	for _, test := range []struct {
		name        string
		limit       int64
		requests    int
		wantFailure bool
	}{
		{"exact limit", 3 * int64(len(body)), 3, false},
		{"combined pages exceed limit", 2 * int64(len(body)), 3, true},
		{"first page exceeds limit", int64(len(body)) - 1, 1, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			app, _, _, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
				requests++
				response := testResponse(http.StatusOK, body)
				response.Header.Set("X-Pagination-PageSize", "1")
				response.Header.Set("X-Pagination-TotalElements", "3")
				return response, nil
			})
			client, err := app.client("test-key", time.Second)
			if err != nil {
				t.Fatal(err)
			}
			items, err := app.allPages(context.Background(), client, catalog.Operation{Method: http.MethodGet}, "/invoice", url.Values{"page_size": {"1"}}, &options{}, test.limit)
			if requests != test.requests {
				t.Errorf("requests = %d; want %d", requests, test.requests)
			}
			if test.wantFailure {
				var failure *Error
				if !errors.As(err, &failure) || failure.Code != "API-003" || !strings.Contains(failure.Hint, "date filters") || items != nil {
					t.Fatalf("items = %v; error = %v; want API-003 with date-filter hint and no partial result", items, err)
				}
			} else if err != nil || len(items) != 3 {
				t.Fatalf("items = %v; error = %v; want three items", items, err)
			}
		})
	}
}
