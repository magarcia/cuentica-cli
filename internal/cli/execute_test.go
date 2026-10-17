package cli

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestDownloadCreatesPrivateFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "invoice.pdf")
	app, stdout, stderr, store := newTestApp(t, func(*http.Request) (*http.Response, error) {
		return testResponse(http.StatusOK, `{"data":"JVBERi0AZml4dHVyZQo="}`), nil
	})
	code := app.Run(context.Background(), []string{"invoice", "pdf", "--id", "123", "--save", filename, "--output", "json"})
	if code != 0 {
		t.Fatalf("exit = %d; stderr = %s", code, stderr)
	}
	contents, err := os.ReadFile(filename)
	if err != nil || string(contents) != "%PDF-\x00fixture\n" {
		t.Fatalf("saved contents = %q; error = %v", contents, err)
	}
	info, err := os.Stat(filename)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0600 {
		t.Errorf("saved permissions = %04o; want 0600", info.Mode().Perm())
	}
	if stdout.Len() != 0 || !strings.Contains(stderr.String(), filename) {
		t.Errorf("stdout = %q; stderr = %q", stdout, stderr)
	}
	if store.gets != 0 {
		t.Error("environment authentication consulted the keyring")
	}
}

func TestDownloadRefusesBinaryTerminalOutputBeforeAuthentication(t *testing.T) {
	app, stdout, stderr, store := newTestApp(t, nil)
	app.OutputTTY = true
	code := app.Run(context.Background(), []string{"invoice", "pdf", "--id", "123", "--save", "-", "--output", "json"})
	if code != 2 || stdout.Len() != 0 || store.gets != 0 {
		t.Fatalf("terminal download: exit=%d stdout=%q keyring reads=%d stderr=%s", code, stdout, store.gets, stderr)
	}
	assertExecuteErrorCode(t, stderr.Bytes(), "CLI-001")
}

func TestMalformedDownloadDoesNotCreateDestination(t *testing.T) {
	for _, response := range []string{
		`not Base64!`,
		`{"data":123}`,
		`{"attachment":{"filename":"invoice.pdf"}}`,
		`{"attachment":{"data":null}}`,
		`["cGRmIGZpeHR1cmU="]`,
	} {
		t.Run(response, func(t *testing.T) {
			filename := filepath.Join(t.TempDir(), "invoice.pdf")
			app, stdout, stderr, _ := newTestApp(t, func(*http.Request) (*http.Response, error) {
				return testResponse(http.StatusOK, response), nil
			})
			code := app.Run(context.Background(), []string{"invoice", "pdf", "--id", "123", "--save", filename, "--output", "json"})
			if code != 1 || stdout.Len() != 0 {
				t.Fatalf("malformed download: exit=%d stdout=%q stderr=%s", code, stdout, stderr)
			}
			assertExecuteErrorCode(t, stderr.Bytes(), "API-004")
			if _, err := os.Lstat(filename); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("failed download created a file: stat error = %v", err)
			}
		})
	}
}

func TestInvalidFileInputFailsBeforeAuthentication(t *testing.T) {
	directory := t.TempDir()
	missing := filepath.Join(directory, "missing.json")
	for _, test := range []struct {
		name string
		args []string
		exit int
		code string
	}{
		{"missing JSON file", []string{"customer", "create", "--data", "@" + missing}, 1, "IO-001"},
		{"directory JSON input", []string{"customer", "create", "--data", "@" + directory}, 1, "IO-001"},
		{"missing upload file", []string{"document", "create", "--upload", missing}, 1, "IO-001"},
		{"directory upload", []string{"document", "create", "--upload", directory}, 1, "IO-001"},
		{"unsupported upload", []string{"customer", "create", "--upload", missing}, 2, "CLI-001"},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, stderr, store := newTestApp(t, nil)
			args := append([]string{"--output", "json"}, test.args...)
			code := app.Run(context.Background(), args)
			if code != test.exit || stdout.Len() != 0 || store.gets != 0 {
				t.Fatalf("invalid file: exit=%d stdout=%q keyring reads=%d stderr=%s", code, stdout, store.gets, stderr)
			}
			assertExecuteErrorCode(t, stderr.Bytes(), test.code)
		})
	}
}

func TestJSONStdinRefusesTerminalBeforeAuthentication(t *testing.T) {
	app, stdout, stderr, store := newTestApp(t, nil)
	app.InputTTY = true
	app.In = strings.NewReader(`{"name":"Ada"}`)
	code := app.Run(context.Background(), []string{"customer", "create", "--data", "-", "--output", "json"})
	if code != 2 || stdout.Len() != 0 || store.gets != 0 {
		t.Fatalf("terminal JSON input: exit=%d stdout=%q keyring reads=%d stderr=%s", code, stdout, store.gets, stderr)
	}
	assertExecuteErrorCode(t, stderr.Bytes(), "CLI-001")
}

func assertExecuteErrorCode(t *testing.T, payload []byte, code string) {
	t.Helper()
	var result struct {
		Error Error `json:"error"`
	}
	if err := json.Unmarshal(payload, &result); err != nil {
		t.Fatalf("stderr is not a JSON error: %q; error = %v", payload, err)
	}
	if result.Error.Code != code || result.Error.Message == "" || result.Error.Hint == "" {
		t.Errorf("error = %+v; want code %s and recovery instructions", result.Error, code)
	}
}
