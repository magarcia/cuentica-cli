package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"syscall"
	"testing"
)

type commandFailingWriter struct {
	err   error
	calls int
}

func (w *commandFailingWriter) Write(data []byte) (int, error) {
	w.calls++
	if w.err == nil {
		return len(data) / 2, nil
	}
	return 0, w.err
}

func TestCommandsHandleStdoutFailures(t *testing.T) {
	commands := []struct {
		name string
		args []string
	}{
		{"summary", nil},
		{"root help", []string{"--help"}},
		{"resource help", []string{"customer", "--help"}},
		{"operation help", []string{"invoice", "create", "--help"}},
		{"auth help", []string{"auth", "--help"}},
		{"version", []string{"--version"}},
		{"api help", []string{"api", "--help"}},
		{"api list", []string{"api", "list"}},
		{"completion help", []string{"completion", "--help"}},
		{"completion", []string{"completion", "bash"}},
		{"environment", []string{"help", "environment"}},
		{"exit codes", []string{"help", "exit-codes"}},
		{"auth login", []string{"auth", "login", "--key-stdin"}},
		{"auth status", []string{"auth", "status"}},
		{"auth logout", []string{"auth", "logout"}},
		{"API response", []string{"company", "get"}},
		{"binary download", []string{"invoice", "pdf", "--id", "123", "--save", "-"}},
		{"dry run", []string{"customer", "delete", "--id", "123", "--dry-run"}},
	}
	for _, command := range commands {
		for _, failure := range []struct {
			name string
			err  error
			exit int
		}{
			{"write error", errors.New("private output failure"), 1},
			{"short write", nil, 1},
			{"closed pipe", fmt.Errorf("stdout: %w", syscall.EPIPE), 0},
		} {
			t.Run(command.name+"/"+failure.name, func(t *testing.T) {
				app, _, stderr, _ := newTestApp(t, func(request *http.Request) (*http.Response, error) {
					if strings.HasSuffix(request.URL.Path, "/pdf") {
						return testResponse(http.StatusOK, `"cGRm"`), nil
					}
					return testResponse(http.StatusOK, `{}`), nil
				})
				writer := &commandFailingWriter{err: failure.err}
				app.Out = writer
				app.In = strings.NewReader("new-key")
				args := append([]string{"--output", "json", "--quiet"}, command.args...)
				if code := app.Run(context.Background(), args); code != failure.exit {
					t.Fatalf("exit = %d; want %d; stderr = %s", code, failure.exit, stderr)
				}
				if writer.calls != 1 {
					t.Errorf("stdout writes = %d; want one failed write", writer.calls)
				}
				if app.Out != writer {
					t.Error("Run changed the caller's output writer")
				}
				if failure.exit == 0 {
					if stderr.Len() != 0 {
						t.Errorf("closed pipe emitted stderr: %q", stderr)
					}
					return
				}
				assertExecuteErrorCode(t, stderr.Bytes(), "IO-001")
				if strings.Contains(stderr.String(), "private output failure") || strings.Contains(stderr.String(), "NET-001") {
					t.Errorf("stdout failure was not translated at its boundary: %s", stderr)
				}
			})
		}
	}
}

type brokenPipeTransport struct{}

func (brokenPipeTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, fmt.Errorf("socket write: %w", syscall.EPIPE)
}

func TestNetworkBrokenPipeIsAnError(t *testing.T) {
	app, stdout, stderr, _ := newTestApp(t, nil)
	app.Transport = brokenPipeTransport{}
	code := app.Run(context.Background(), []string{"company", "get", "--output", "json"})
	if code != 10 || stdout.Len() != 0 {
		t.Fatalf("network pipe failure: exit=%d stdout=%q stderr=%s", code, stdout, stderr)
	}
	assertExecuteErrorCode(t, stderr.Bytes(), "NET-001")
}

func TestParseErrorsUseTheParsedOutputFormat(t *testing.T) {
	for _, test := range []struct {
		name string
		tty  bool
		args []string
		json bool
	}{
		{"pipe default", false, nil, true},
		{"terminal default", true, nil, false},
		{"attached shorthand", true, []string{"-ojson"}, true},
		{"repeated format", true, []string{"--output", "text", "--output", "json"}, true},
		{"repeated equals format", false, []string{"--output=json", "--output=text"}, false},
		{"flag value resembling output", false, []string{"--data", "--output=text"}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			app, stdout, stderr, store := newTestApp(t, nil)
			app.OutputTTY = test.tty
			args := append(append([]string{"company", "get"}, test.args...), "--bad-flag")
			if code := app.Run(context.Background(), args); code != 2 {
				t.Fatalf("exit = %d; want 2; stderr = %s", code, stderr)
			}
			if stdout.Len() != 0 || store.gets != 0 {
				t.Fatalf("parse error wrote stdout or read credentials: stdout=%q gets=%d", stdout, store.gets)
			}
			if got := json.Valid(stderr.Bytes()); got != test.json {
				t.Errorf("JSON stderr = %t; want %t; stderr = %s", got, test.json, stderr)
			}
			if !strings.Contains(stderr.String(), "CLI-001") {
				t.Errorf("missing usage error code: %s", stderr)
			}
		})
	}
}
