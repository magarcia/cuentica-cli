package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"github.com/magarcia/cuentica-cli/internal/auth"
	"github.com/magarcia/cuentica-cli/internal/catalog"
	"github.com/magarcia/cuentica-cli/internal/testutil"
)

func TestEveryDocumentedOperationSendsExpectedRequest(t *testing.T) {
	operations := []struct{ resource, action, method, path string }{
		{"company", "get", "GET", "/company"},
		{"company-series", "list", "GET", "/company/serie"},
		{"account", "list", "GET", "/account"},
		{"account", "get", "GET", "/account/:id"},
		{"provider", "list", "GET", "/provider"},
		{"provider", "create", "POST", "/provider"},
		{"provider", "get", "GET", "/provider/:id"},
		{"provider", "update", "PUT", "/provider/:id"},
		{"provider", "delete", "DELETE", "/provider/:id"},
		{"customer", "list", "GET", "/customer"},
		{"customer", "create", "POST", "/customer"},
		{"customer", "get", "GET", "/customer/:id"},
		{"customer", "update", "PUT", "/customer/:id"},
		{"customer", "delete", "DELETE", "/customer/:id"},
		{"invoice", "list", "GET", "/invoice"},
		{"invoice", "create", "POST", "/invoice"},
		{"invoice", "get", "GET", "/invoice/:id"},
		{"invoice", "update", "PUT", "/invoice/:id"},
		{"invoice", "delete", "DELETE", "/invoice/:id"},
		{"invoice", "void", "POST", "/invoice/:id/void"},
		{"invoice", "public", "GET", "/invoice/:id/public"},
		{"invoice-charge", "update", "PUT", "/invoice/:id/charges"},
		{"invoice", "email", "POST", "/invoice/:id/email"},
		{"invoice", "pdf", "GET", "/invoice/:id/pdf"},
		{"income", "list", "GET", "/income"},
		{"income", "create", "POST", "/income"},
		{"income", "get", "GET", "/income/:id"},
		{"income", "update", "PUT", "/income/:id"},
		{"income", "delete", "DELETE", "/income/:id"},
		{"income-attachment", "get", "GET", "/income/:id/attachment"},
		{"income-attachment", "delete", "DELETE", "/income/:id/attachment"},
		{"income-attachment", "update", "PUT", "/income/:id/attachment"},
		{"income-charge", "update", "PUT", "/income/:id/charges"},
		{"expense", "list", "GET", "/expense"},
		{"expense", "create", "POST", "/expense"},
		{"expense", "get", "GET", "/expense/:id"},
		{"expense", "update", "PUT", "/expense/:id"},
		{"expense", "delete", "DELETE", "/expense/:id"},
		{"expense-attachment", "get", "GET", "/expense/:id/attachment"},
		{"expense-attachment", "delete", "DELETE", "/expense/:id/attachment"},
		{"expense-attachment", "update", "PUT", "/expense/:id/attachment"},
		{"expense-payment", "update", "PUT", "/expense/:id/payments"},
		{"document", "list", "GET", "/document"},
		{"document", "create", "POST", "/document"},
		{"document", "get", "GET", "/document/:id"},
		{"document", "update", "PUT", "/document/:id"},
		{"document", "delete", "DELETE", "/document/:id"},
		{"document-attachment", "get", "GET", "/document/:id/attachment"},
		{"tag", "list", "GET", "/tag"},
		{"transfer", "list", "GET", "/transfer"},
		{"transfer", "create", "POST", "/transfer"},
		{"transfer", "get", "GET", "/transfer/:id"},
		{"transfer", "update", "PUT", "/transfer/:id"},
		{"transfer", "delete", "DELETE", "/transfer/:id"},
	}
	byCommand := map[string]catalog.Operation{}
	for _, op := range catalog.Operations() {
		byCommand[op.Resource+" "+op.Action] = op
	}
	if len(byCommand) != len(operations) {
		t.Fatalf("catalog has %d commands; integration cases cover %d", len(byCommand), len(operations))
	}
	for _, operation := range operations {
		t.Run(operation.resource+"/"+operation.action, func(t *testing.T) {
			op, exists := byCommand[operation.resource+" "+operation.action]
			if !exists {
				t.Fatal("documented command is missing from the catalog")
			}
			args := []string{operation.resource, operation.action, "--output", "json"}
			if strings.Contains(operation.path, ":id") {
				args = append(args, "--id", "123")
			}
			if op.Body {
				args = append(args, "--data", "{}")
			}
			if op.Destructive {
				args = append(args, "--yes")
			}
			wantQuery := url.Values{}
			for _, query := range op.Query {
				value := testQueryValue(query)
				wantQuery.Add(query, value)
				args = append(args, "--"+strings.ReplaceAll(query, "_", "-"), value)
			}
			requests := 0
			app, stdout, stderr, store := newTestApp(t, func(request *http.Request) (*http.Response, error) {
				requests++
				if request.Method != operation.method || request.URL.Path != strings.ReplaceAll(operation.path, ":id", "123") {
					t.Errorf("request = %s %s; want %s %s", request.Method, request.URL.Path, operation.method, operation.path)
				}
				if got := request.URL.Query(); !reflect.DeepEqual(got, wantQuery) {
					t.Errorf("query = %v; want %v", got, wantQuery)
				}
				if got := request.Header.Get("X-AUTH-TOKEN"); got != "test-key" {
					t.Errorf("X-AUTH-TOKEN = %q", got)
				}
				if op.Body {
					body, err := io.ReadAll(request.Body)
					if err != nil || !json.Valid(body) || string(bytes.TrimSpace(body)) != "{}" {
						t.Errorf("request body = %q, error = %v", body, err)
					}
					if request.Header.Get("Content-Type") != "application/json" {
						t.Errorf("body content type = %q", request.Header.Get("Content-Type"))
					}
				}
				body := `{"id":123,"result":"ok"}`
				if op.Binary {
					body = `"cGRmIGZpeHR1cmU="`
				} else if op.Paginated || operation.action == "list" {
					body = `[{"id":123}]`
				}
				return testResponse(http.StatusOK, body), nil
			})
			if code := app.Run(context.Background(), args); code != 0 {
				t.Fatalf("exit = %d; stderr = %s", code, stderr)
			}
			if requests != 1 {
				t.Errorf("sent %d requests; want one", requests)
			}
			if !json.Valid(stdout.Bytes()) {
				t.Errorf("stdout is not JSON: %q", stdout)
			}
			if strings.Contains(stderr.String(), "test-key") || strings.Contains(stdout.String(), "test-key") {
				t.Error("API key leaked into command output")
			}
			if store.gets != 0 {
				t.Error("consulted the keyring even though the environment supplied a key")
			}
		})
	}
}

func TestInputValidationPreventsRequests(t *testing.T) {
	for _, test := range []struct {
		name string
		args []string
	}{
		{"unknown flag", []string{"company", "get", "--not-a-flag"}},
		{"unsupported flag", []string{"company", "get", "--all"}},
		{"missing id", []string{"customer", "get"}},
		{"zero id", []string{"customer", "get", "--id", "0"}},
		{"unsafe id", []string{"customer", "get", "--id", "../company"}},
		{"malformed JSON", []string{"customer", "create", "--data", "{broken"}},
		{"array body", []string{"customer", "create", "--data", "[]"}},
		{"null body", []string{"customer", "create", "--data", "null"}},
		{"missing body", []string{"customer", "create"}},
		{"invalid page", []string{"customer", "list", "--page", "0"}},
		{"oversized page", []string{"customer", "list", "--page-size", "301"}},
		{"duplicate page", []string{"customer", "list", "--page", "1", "--query", "page=2"}},
		{"all with page", []string{"customer", "list", "--all", "--page", "2"}},
		{"invalid date", []string{"invoice", "list", "--initial-date", "2026-02-30"}},
		{"invalid sort", []string{"invoice", "list", "--sort", "random"}},
		{"invalid boolean", []string{"invoice", "list", "--issued", "maybe"}},
		{"credential query", []string{"customer", "list", "--query", "X-AUTH-TOKEN=test-key"}},
		{"zero timeout", []string{"company", "get", "--timeout", "0s"}},
		{"invalid output", []string{"company", "get", "--output", "yaml"}},
		{"extra positional", []string{"customer", "get", "123"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			app, stdout, stderr, store := newTestApp(t, func(*http.Request) (*http.Response, error) {
				requests++
				return testResponse(200, `{}`), nil
			})
			args := append([]string{"--output", "json"}, test.args...)
			if code := app.Run(context.Background(), args); code != 2 {
				t.Errorf("exit = %d; want usage exit 2; stderr = %s", code, stderr)
			}
			if requests != 0 {
				t.Errorf("invalid input sent %d requests", requests)
			}
			if store.gets != 0 {
				t.Error("invalid input consulted credentials")
			}
			if stdout.Len() != 0 || stderr.Len() == 0 {
				t.Errorf("stdout = %q; stderr = %q", stdout, stderr)
			}
		})
	}
}

func TestDestructiveOperationRequiresExplicitConfirmationWhenPiped(t *testing.T) {
	requests := 0
	app, stdout, stderr, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
		requests++
		return testResponse(200, `{}`), nil
	})
	code := app.Run(context.Background(), []string{"customer", "delete", "--id", "123", "--output", "json"})
	if code == 0 || requests != 0 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "--yes") {
		t.Fatalf("unconfirmed deletion: exit=%d requests=%d stdout=%q stderr=%q", code, requests, stdout, stderr)
	}
}

func TestDryRunDoesNotReadCredentialsOrSendRequests(t *testing.T) {
	requests := 0
	app, stdout, stderr, store := newTestApp(t, func(*http.Request) (*http.Response, error) {
		requests++
		return testResponse(200, `{}`), nil
	})
	app.Getenv = func(string) string { return "" }
	code := app.Run(context.Background(), []string{"invoice", "delete", "--id", "123", "--dry-run", "--output", "json"})
	if code != 0 || requests != 0 || store.gets != 0 {
		t.Fatalf("dry run: exit=%d requests=%d keyring reads=%d stderr=%s", code, requests, store.gets, stderr)
	}
	if !json.Valid(stdout.Bytes()) || !strings.Contains(stdout.String(), "/invoice/123") || !strings.Contains(stdout.String(), "DELETE") {
		t.Errorf("dry-run output = %q", stdout)
	}
}

func TestMissingCredentialsFailsBeforeSendingRequest(t *testing.T) {
	requests := 0
	app, stdout, stderr, store := newTestApp(t, func(*http.Request) (*http.Response, error) {
		requests++
		return testResponse(200, `{}`), nil
	})
	app.Getenv = func(string) string { return "" }
	code := app.Run(context.Background(), []string{"company", "get", "--output", "json"})
	if code != 3 || requests != 0 || stdout.Len() != 0 || store.gets != 1 {
		t.Fatalf("missing auth: exit=%d requests=%d keyring reads=%d stdout=%q stderr=%q", code, requests, store.gets, stdout, stderr)
	}
	if !strings.Contains(stderr.String(), "CUENTICA_API_KEY") || !json.Valid(stderr.Bytes()) {
		t.Errorf("auth error does not explain recovery: %q", stderr)
	}
}

func TestUnknownCommandSuggestsCorrection(t *testing.T) {
	app, stdout, stderr, store := newTestApp(t, nil)
	code := app.Run(context.Background(), []string{"custmer", "list", "--output", "json"})
	if code != 2 || stdout.Len() != 0 || store.gets != 0 || !strings.Contains(stderr.String(), "cuentica customer list") {
		t.Fatalf("unknown command: exit=%d stdout=%q stderr=%q", code, stdout, stderr)
	}
}

func TestAllPagesPreservesFiltersAndAggregatesResults(t *testing.T) {
	var pages []int
	app, stdout, stderr, _ := newTestApp(t, func(request *http.Request) (*http.Response, error) {
		page, err := strconv.Atoi(request.URL.Query().Get("page"))
		if err != nil {
			t.Errorf("invalid page query: %q", request.URL.RawQuery)
		}
		pages = append(pages, page)
		if got := request.URL.Query().Get("q"); got != "a & b" {
			t.Errorf("pagination lost filter: q = %q", got)
		}
		if got := request.URL.Query().Get("page_size"); got != "1" {
			t.Errorf("pagination changed page_size = %q", got)
		}
		response := testResponse(200, `[{"id":`+strconv.Itoa(page)+`}]`)
		response.Header.Set("X-Pagination-CurrentPage", strconv.Itoa(page))
		response.Header.Set("X-Pagination-PageSize", "1")
		response.Header.Set("X-Pagination-TotalElements", "2")
		return response, nil
	})
	code := app.Run(context.Background(), []string{"customer", "list", "--all", "--page-size", "1", "--q", "a & b", "--output", "json"})
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %s", code, stderr)
	}
	if !reflect.DeepEqual(pages, []int{1, 2}) {
		t.Errorf("requested pages = %v; want [1 2]", pages)
	}
	var rows []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &rows); err != nil || len(rows) != 2 || rows[0].ID != 1 || rows[1].ID != 2 {
		t.Errorf("aggregated output = %q; decode error = %v", stdout, err)
	}
}

func TestPaginationFallsBackToShortPagesWithoutMetadata(t *testing.T) {
	requests := 0
	app, stdout, stderr, _ := newTestApp(t, func(request *http.Request) (*http.Response, error) {
		requests++
		if request.URL.Query().Get("page") != strconv.Itoa(requests) {
			t.Errorf("page = %q; want %d", request.URL.Query().Get("page"), requests)
		}
		if requests == 1 {
			return testResponse(200, `[{"id":1},{"id":2}]`), nil
		}
		return testResponse(200, `[{"id":3}]`), nil
	})
	code := app.Run(context.Background(), []string{"customer", "list", "--all", "--page-size", "2", "--output", "json"})
	var rows []struct {
		ID int `json:"id"`
	}
	decodeErr := json.Unmarshal(stdout.Bytes(), &rows)
	if code != 0 || requests != 2 || decodeErr != nil || len(rows) != 3 {
		t.Errorf("pagination: exit=%d requests=%d output=%q stderr=%q decode=%v", code, requests, stdout, stderr, decodeErr)
	}
}

func TestPaginationFailureDoesNotPrintPartialData(t *testing.T) {
	for _, test := range []struct{ name, body, currentPage string }{
		{"object response", `{"id":2}`, "2"},
		{"unexpected page", `[{"id":2}]`, "1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			requests := 0
			app, stdout, stderr, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
				requests++
				response := testResponse(200, `[{"id":1}]`)
				response.Header.Set("X-Pagination-TotalElements", "2")
				response.Header.Set("X-Pagination-PageSize", "1")
				if requests == 2 {
					response.Body = io.NopCloser(strings.NewReader(test.body))
					response.Header.Set("X-Pagination-CurrentPage", test.currentPage)
				}
				return response, nil
			})
			code := app.Run(context.Background(), []string{"customer", "list", "--all", "--page-size", "1", "--output", "json"})
			if code != 1 || requests != 2 || stdout.Len() != 0 || !strings.Contains(stderr.String(), "API-003") {
				t.Errorf("failed pagination: exit=%d requests=%d stdout=%q stderr=%q", code, requests, stdout, stderr)
			}
		})
	}
}

func TestBodySourcesAndFieldOverrides(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "customer.json")
	if err := os.WriteFile(filename, []byte(`{"name":"Ada","sequence":9007199254740993}`), 0600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, input string
		args        []string
		want        string
	}{
		{"literal", "", []string{"--data", `{"name":"Ada","sequence":9007199254740993}`}, `{"name":"Ada","sequence":9007199254740993}`},
		{"file", "", []string{"--data", "@" + filename}, `{"name":"Ada","sequence":9007199254740993}`},
		{"stdin", `{"name":"Ada","sequence":9007199254740993}`, []string{"--data", "-"}, `{"name":"Ada","sequence":9007199254740993}`},
		{"fields", "", []string{"--field", "name=Ada", "--field", "active=true", "--field", `tags=["one","two"]`}, `{"name":"Ada","active":true,"tags":["one","two"]}`},
		{"override", "", []string{"--data", `{"name":"Ada","count":1}`, "--field", "count=2"}, `{"name":"Ada","count":2}`},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, _, stderr, _ := newTestApp(t, func(request *http.Request) (*http.Response, error) {
				body, err := io.ReadAll(request.Body)
				var got, want map[string]json.RawMessage
				gotErr := json.Unmarshal(body, &got)
				wantErr := json.Unmarshal([]byte(test.want), &want)
				if err != nil || gotErr != nil || wantErr != nil || !reflect.DeepEqual(got, want) {
					t.Errorf("body = %s; want %s; errors = %v, %v, %v", body, test.want, err, gotErr, wantErr)
				}
				return testResponse(200, `{"id":123}`), nil
			})
			app.In = strings.NewReader(test.input)
			args := append([]string{"customer", "create", "--output", "json"}, test.args...)
			if code := app.Run(context.Background(), args); code != 0 {
				t.Fatalf("exit = %d; stderr = %s", code, stderr)
			}
		})
	}
}

func TestUploadsEncodeFilesInDocumentAndAttachmentBodies(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "receipt.pdf")
	if err := os.WriteFile(filename, []byte("receipt fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, resource := range []string{"document", "expense-attachment"} {
		t.Run(resource, func(t *testing.T) {
			app, _, stderr, _ := newTestApp(t, func(request *http.Request) (*http.Response, error) {
				var body map[string]json.RawMessage
				if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
					t.Error(err)
				}
				attachment := map[string]string{}
				if resource == "document" {
					if err := json.Unmarshal(body["attachment"], &attachment); err != nil {
						t.Error(err)
					}
				} else {
					for _, key := range []string{"filename", "data"} {
						var value string
						if err := json.Unmarshal(body[key], &value); err != nil {
							t.Error(err)
						}
						attachment[key] = value
					}
				}
				want := map[string]string{"filename": "receipt.pdf", "data": "cmVjZWlwdCBmaXh0dXJl"}
				if !reflect.DeepEqual(attachment, want) {
					t.Errorf("attachment = %v; want %v", attachment, want)
				}
				return testResponse(200, `{"id":123}`), nil
			})
			args := []string{resource, "create", "--upload", filename, "--output", "json"}
			if resource == "expense-attachment" {
				args = []string{resource, "update", "--id", "123", "--upload", filename, "--yes", "--output", "json"}
			}
			if code := app.Run(context.Background(), args); code != 0 {
				t.Fatalf("exit = %d; stderr = %s", code, stderr)
			}
		})
	}
}

func TestBinaryDownloadsWriteDecodedBytes(t *testing.T) {
	for _, test := range []struct{ name, response, want string }{
		{"JSON string", `"cGRmIGZpeHR1cmU="`, "pdf fixture"},
		{"plain Base64", `cGRmIGZpeHR1cmU=`, "pdf fixture"},
		{"data object", `{"data":"cGRmIGZpeHR1cmU="}`, "pdf fixture"},
		{"attachment object", `{"attachment":{"data":"cGRmIGZpeHR1cmU="}}`, "pdf fixture"},
		{"raw PDF", "%PDF-fixture", "%PDF-fixture"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, stderr, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
				return testResponse(200, test.response), nil
			})
			code := app.Run(context.Background(), []string{"invoice", "pdf", "--id", "123", "--save", "-"})
			if code != 0 || stdout.String() != test.want {
				t.Errorf("exit = %d; stdout = %q; want %q; stderr = %s", code, stdout, test.want, stderr)
			}
		})
	}
}

func TestDownloadRefusesExistingFileBeforeHTTP(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "invoice.pdf")
	if err := os.WriteFile(filename, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	app, stdout, stderr, _ := newTestApp(t, nil)
	code := app.Run(context.Background(), []string{"invoice", "pdf", "--id", "123", "--save", filename})
	contents, err := os.ReadFile(filename)
	if code == 0 || stdout.Len() != 0 || err != nil || string(contents) != "existing" {
		t.Fatalf("existing download: exit=%d contents=%q error=%v stderr=%s", code, contents, err, stderr)
	}
}

func TestAPIErrorsUseStructuredExitCodesAndRedactCredentials(t *testing.T) {
	for _, test := range []struct {
		status, exit int
		code         string
	}{
		{401, 3, "AUTH-002"}, {403, 3, "AUTH-002"}, {404, 4, "API-404"}, {429, 1, "API-429"}, {500, 1, "API-001"},
	} {
		t.Run(strconv.Itoa(test.status), func(t *testing.T) {
			app, stdout, stderr, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
				response := testResponse(test.status, `{"error":"rejected token test-key"}`)
				response.Header.Set("Retry-After", "30")
				return response, nil
			})
			code := app.Run(context.Background(), []string{"company", "get", "--output", "json"})
			var result struct {
				Error Error `json:"error"`
			}
			err := json.Unmarshal(stderr.Bytes(), &result)
			if code != test.exit || stdout.Len() != 0 || err != nil || result.Error.Code != test.code || result.Error.Status != test.status {
				t.Errorf("API failure: exit=%d stdout=%q stderr=%q decode=%v", code, stdout, stderr, err)
			}
			if strings.Contains(stderr.String(), "test-key") || !strings.Contains(stderr.String(), "[REDACTED]") {
				t.Errorf("credential was not redacted: %s", stderr)
			}
		})
	}
}

func newTestApp(t *testing.T, handler func(*http.Request) (*http.Response, error)) (*App, *bytes.Buffer, *bytes.Buffer, *authTestStore) {
	t.Helper()
	stdout, stderr := new(bytes.Buffer), new(bytes.Buffer)
	store := &authTestStore{getErr: auth.ErrNotFound}
	transport := testutil.Transport(http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		if handler == nil {
			t.Error("unexpected HTTP request")
			http.Error(w, "unexpected request", http.StatusInternalServerError)
			return
		}
		response, err := handler(request)
		if err != nil {
			t.Errorf("HTTP fixture: %v", err)
			http.Error(w, "fixture failed", http.StatusInternalServerError)
			return
		}
		defer response.Body.Close()
		for name, values := range response.Header {
			for _, value := range values {
				w.Header().Add(name, value)
			}
		}
		w.WriteHeader(response.StatusCode)
		_, _ = io.Copy(w, response.Body)
	}))
	t.Cleanup(func() { _ = transport.Close() })
	app := &App{
		In: strings.NewReader(""), Out: stdout, Err: stderr,
		Getenv: func(name string) string {
			if name == "CUENTICA_API_KEY" {
				return "test-key"
			}
			return ""
		},
		Store: store, BaseURL: "http://127.0.0.1:12345", Version: "test",
		Transport: transport,
	}
	return app, stdout, stderr, store
}

func testResponse(status int, body string) *http.Response {
	return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}}, Body: io.NopCloser(strings.NewReader(body))}
}

func testQueryValue(name string) string {
	switch name {
	case "page":
		return "2"
	case "page_size":
		return "10"
	case "customer", "provider", "origin_account", "destination_account":
		return "456"
	case "min_total_limit":
		return "10.5"
	case "max_total_limit":
		return "500.5"
	case "initial_date":
		return "2026-01-01"
	case "end_date":
		return "2026-12-31"
	case "issued", "draft", "assigned":
		return "false"
	case "sort":
		return "desc"
	case "extension":
		return "pdf"
	default:
		return "a & b"
	}
}
