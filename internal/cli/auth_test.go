package cli

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/magarcia/cuentica-cli/internal/auth"
	"github.com/magarcia/cuentica-cli/internal/testutil"
)

type authTestStore struct {
	token                     string
	getErr, setErr, deleteErr error
	gets, sets, deletes       int
}

func (s *authTestStore) Get() (string, error) {
	s.gets++
	return s.token, s.getErr
}

func (s *authTestStore) Set(token string) error {
	s.sets++
	if s.setErr == nil {
		s.token = token
	}
	return s.setErr
}

func (s *authTestStore) Delete() error {
	s.deletes++
	if s.deleteErr == nil {
		s.token = ""
	}
	return s.deleteErr
}

func authTestApp(store *authTestStore, baseURL string) *App {
	return &App{
		In: strings.NewReader(""), Out: &bytes.Buffer{}, Err: &bytes.Buffer{},
		Getenv: func(string) string { return "" }, Store: store, BaseURL: baseURL,
		ReadPassword: func() (string, error) { return "", errors.New("unexpected prompt") },
	}
}

func authHandlerApp(t *testing.T, store *authTestStore, handler http.Handler) *App {
	t.Helper()
	a := authTestApp(store, "http://127.0.0.1")
	transport := testutil.Transport(handler)
	a.Transport = transport
	t.Cleanup(func() { _ = transport.Close() })
	return a
}

func runAuthTest(t *testing.T, a *App, action string, flags ...string) (string, string, int) {
	t.Helper()
	fs, o := a.flags(nil)
	if err := fs.Parse(append(flags, "--output", "json")); err != nil {
		t.Fatal(err)
	}
	err := a.authenticate(context.Background(), action, fs, o)
	exit := a.finish(err, o.output)
	return a.Out.(*bytes.Buffer).String(), a.Err.(*bytes.Buffer).String(), exit
}

func TestAuthLoginVerifiesBeforeSaving(t *testing.T) {
	store := &authTestStore{token: "previous-key"}
	requests := 0
	a := authHandlerApp(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.Method != http.MethodGet || r.URL.Path != "/company" || r.Header.Get("X-AUTH-TOKEN") != "new-key" {
			t.Error("login verification used the wrong request or credentials")
		}
		if store.token != "previous-key" || store.sets != 0 {
			t.Error("login replaced stored credentials before verification")
		}
		io.WriteString(w, `{"name":"Company","secret":"new-key"}`)
	}))
	a.In = strings.NewReader("new-key\r\n")
	stdout, stderr, exit := runAuthTest(t, a, "login", "--key-stdin")
	if exit != 0 || requests != 1 || store.token != "new-key" || store.sets != 1 {
		t.Fatalf("login failed: exit=%d requests=%d sets=%d stderr=%s", exit, requests, store.sets, stderr)
	}
	if !strings.Contains(stdout, `"authenticated": true`) || !strings.Contains(stdout, `"source": "keyring"`) {
		t.Fatalf("unexpected login output: %s", stdout)
	}
	if strings.Contains(stdout+stderr, "new-key") {
		t.Fatal("login displayed the API key")
	}
}

func TestAuthLoginRejectedKeyPreservesSavedKey(t *testing.T) {
	store := &authTestStore{token: "previous-key"}
	a := authHandlerApp(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		io.WriteString(w, `{"error":"rejected-key is invalid"}`)
	}))
	a.In = strings.NewReader("rejected-key")
	stdout, stderr, exit := runAuthTest(t, a, "login", "--key-stdin")
	if exit != 3 || store.token != "previous-key" || store.sets != 0 || stdout != "" {
		t.Fatalf("rejected key changed credentials: exit=%d sets=%d stdout=%s", exit, store.sets, stdout)
	}
	if !strings.Contains(stderr, "AUTH-002") || strings.Contains(stderr, "rejected-key") {
		t.Fatalf("unexpected auth error: %s", stderr)
	}
}

func TestAuthLoginRejectsInvalidInputBeforeRequests(t *testing.T) {
	for _, token := range []string{"", " ", "key with spaces", "key\n\n", "key\x00", strings.Repeat("x", 2049)} {
		t.Run(strings.ReplaceAll(token[:min(len(token), 24)], "\n", "newline"), func(t *testing.T) {
			store := &authTestStore{token: "previous-key"}
			a := authTestApp(store, "http://127.0.0.1:1")
			a.In = strings.NewReader(token)
			stdout, stderr, exit := runAuthTest(t, a, "login", "--key-stdin")
			if exit != 3 || store.sets != 0 || stdout != "" || !strings.Contains(stderr, "AUTH-002") {
				t.Fatalf("invalid key was not rejected before use: exit=%d sets=%d stderr=%s", exit, store.sets, stderr)
			}
			if strings.Contains(stderr, "NET-001") {
				t.Fatal("invalid credentials reached the network")
			}
		})
	}
}

type authCountingReader struct {
	read int
}

func (r *authCountingReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 'x'
	}
	r.read += len(p)
	return len(p), nil
}

func TestAuthLoginBoundsSecretInput(t *testing.T) {
	reader := &authCountingReader{}
	a := authTestApp(&authTestStore{}, "http://127.0.0.1:1")
	a.In = reader
	_, _, exit := runAuthTest(t, a, "login", "--key-stdin")
	if exit != 3 || reader.read > 2051 {
		t.Fatalf("input was not bounded: exit=%d bytes=%d", exit, reader.read)
	}
}

func TestAuthLoginPromptsOnlyInInteractiveTerminal(t *testing.T) {
	for _, tt := range []struct {
		name    string
		tty     bool
		noInput bool
	}{
		{name: "noninteractive"},
		{name: "no input", tty: true, noInput: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := authTestApp(&authTestStore{}, "http://127.0.0.1:1")
			a.InputTTY = tt.tty
			a.ReadPassword = func() (string, error) {
				t.Error("prompted without an interactive terminal")
				return "", nil
			}
			flags := []string{}
			if tt.noInput {
				flags = append(flags, "--no-input")
			}
			_, stderr, exit := runAuthTest(t, a, "login", flags...)
			if exit != 2 || !strings.Contains(stderr, "--key-stdin") {
				t.Fatalf("missing input did not explain how to authenticate: exit=%d stderr=%s", exit, stderr)
			}
		})
	}
}

func TestAuthLoginHiddenPrompt(t *testing.T) {
	store := &authTestStore{}
	a := authHandlerApp(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{}`)
	}))
	a.InputTTY = true
	prompts := 0
	a.ReadPassword = func() (string, error) {
		prompts++
		return "prompted-key", nil
	}
	stdout, stderr, exit := runAuthTest(t, a, "login")
	if exit != 0 || prompts != 1 || store.token != "prompted-key" || !strings.Contains(stderr, "API key:") {
		t.Fatalf("interactive login failed: exit=%d prompts=%d stderr=%s", exit, prompts, stderr)
	}
	if strings.Contains(stdout+stderr, "prompted-key") {
		t.Fatal("interactive login displayed the API key")
	}
}

func TestAuthLoginAcceptsMaximumKeyWithCRLF(t *testing.T) {
	store := &authTestStore{}
	a := authHandlerApp(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{}`)
	}))
	token := strings.Repeat("x", 2048)
	a.In = strings.NewReader(token + "\r\n")
	_, stderr, exit := runAuthTest(t, a, "login", "--key-stdin")
	if exit != 0 || store.token != token {
		t.Fatalf("maximum length key was rejected: exit=%d stderr=%s", exit, stderr)
	}
}

func TestAuthLoginKeyFile(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "api-key")
	if err := os.WriteFile(filename, []byte("file-key\n"), 0600); err != nil {
		t.Fatal(err)
	}
	store := &authTestStore{}
	a := authHandlerApp(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-AUTH-TOKEN") != "file-key" {
			t.Error("key file was not read correctly")
		}
		io.WriteString(w, `{}`)
	}))
	_, stderr, exit := runAuthTest(t, a, "login", "--key-file", filename)
	if exit != 0 || store.token != "file-key" {
		t.Fatalf("key file login failed: exit=%d stderr=%s", exit, stderr)
	}
}

func TestAuthStatusEnvironmentPrecedence(t *testing.T) {
	store := &authTestStore{token: "saved-key"}
	a := authHandlerApp(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-AUTH-TOKEN") != "environment-key" {
			t.Error("status did not use environment credentials")
		}
		io.WriteString(w, `{}`)
	}))
	a.Getenv = func(string) string { return "environment-key" }
	stdout, stderr, exit := runAuthTest(t, a, "status")
	if exit != 0 || store.gets != 0 || !strings.Contains(stdout, `"source": "environment"`) {
		t.Fatalf("status failed: exit=%d gets=%d stdout=%s stderr=%s", exit, store.gets, stdout, stderr)
	}
	if strings.Contains(stdout+stderr, "environment-key") {
		t.Fatal("status displayed the API key")
	}
}

func TestAuthStatusCredentialErrors(t *testing.T) {
	for _, tt := range []struct {
		name string
		err  error
		code string
	}{
		{name: "missing", err: auth.ErrNotFound, code: "AUTH-001"},
		{name: "keyring unavailable", err: errors.New("secret-key in keyring error"), code: "AUTH-003"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			a := authTestApp(&authTestStore{getErr: tt.err}, "http://127.0.0.1:1")
			stdout, stderr, exit := runAuthTest(t, a, "status")
			if exit != 3 || stdout != "" || !strings.Contains(stderr, tt.code) || strings.Contains(stderr, "secret-key") {
				t.Fatalf("unexpected credential error: exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
			}
		})
	}
}

func TestAuthLogoutRemovesOnlyStoredCredentials(t *testing.T) {
	for _, quiet := range []bool{false, true} {
		store := &authTestStore{token: "saved-key"}
		a := authTestApp(store, "http://127.0.0.1:1")
		a.Getenv = func(string) string { return "environment-key" }
		var flags []string
		if quiet {
			flags = append(flags, "--quiet")
		}
		stdout, stderr, exit := runAuthTest(t, a, "logout", flags...)
		if exit != 0 || store.deletes != 1 || store.token != "" || !strings.Contains(stdout, `"stored": false`) {
			t.Fatalf("logout failed: exit=%d deletes=%d stdout=%s stderr=%s", exit, store.deletes, stdout, stderr)
		}
		if strings.Contains(stderr, "CUENTICA_API_KEY") == quiet || strings.Contains(stdout+stderr, "environment-key") {
			t.Fatalf("unexpected environment warning: quiet=%t stderr=%s", quiet, stderr)
		}
	}
}

func TestAuthKeyringWriteErrorsAreSanitized(t *testing.T) {
	for _, action := range []string{"login", "logout"} {
		t.Run(action, func(t *testing.T) {
			store := &authTestStore{token: "previous-key", setErr: errors.New("saved-key exposed"), deleteErr: errors.New("saved-key exposed")}
			a := authHandlerApp(t, store, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				io.WriteString(w, `{}`)
			}))
			a.In = strings.NewReader("new-key")
			var flags []string
			if action == "login" {
				flags = append(flags, "--key-stdin")
			}
			stdout, stderr, exit := runAuthTest(t, a, action, flags...)
			if exit != 3 || stdout != "" || !strings.Contains(stderr, "AUTH-003") || strings.Contains(stderr, "saved-key") || strings.Contains(stderr, "new-key") {
				t.Fatalf("keyring error was not sanitized: exit=%d stdout=%s stderr=%s", exit, stdout, stderr)
			}
		})
	}
}

func TestAuthRejectsUnsupportedFlagsBeforeSideEffects(t *testing.T) {
	for _, tt := range []struct {
		action string
		flags  []string
	}{
		{action: "login", flags: []string{"--dry-run", "--key-stdin"}},
		{action: "login", flags: []string{"--key-stdin", "--key-file", "missing-file"}},
		{action: "status", flags: []string{"--all"}},
		{action: "status", flags: []string{"--key-stdin"}},
		{action: "logout", flags: []string{"--data", `{}`}},
	} {
		t.Run(tt.action+strings.Join(tt.flags, " "), func(t *testing.T) {
			store := &authTestStore{}
			a := authTestApp(store, "http://127.0.0.1:1")
			_, stderr, exit := runAuthTest(t, a, tt.action, tt.flags...)
			if exit != 2 || store.gets+store.sets+store.deletes != 0 || !strings.Contains(stderr, "CLI-001") {
				t.Fatalf("invalid flags caused side effects: exit=%d stderr=%s", exit, stderr)
			}
		})
	}
}
