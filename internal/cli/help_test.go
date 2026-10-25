package cli

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os/exec"
	"slices"
	"strings"
	"testing"

	"github.com/magarcia/cuentica-cli/internal/catalog"
)

func helpApp() (*App, *bytes.Buffer) {
	var out bytes.Buffer
	return &App{Out: &out, Err: io.Discard}, &out
}

func TestHelpListsAllDocumentedCommands(t *testing.T) {
	app, out := helpApp()
	app.rootHelp(false)
	if strings.Contains(out.String(), "company-series") {
		t.Fatal("no-argument help should keep the resource list concise")
	}
	out.Reset()
	app.rootHelp(true)
	for _, op := range catalog.Operations() {
		if !strings.Contains(out.String(), op.Resource) {
			t.Errorf("full help omits resource %q", op.Resource)
		}
	}
	for _, resource := range apiResources() {
		out.Reset()
		app.resourceHelp(resource)
		for _, op := range resourceOperations(resource) {
			if !strings.Contains(out.String(), op.Action) {
				t.Errorf("%s help omits action %q", resource, op.Action)
			}
		}
	}
}

func TestOperationHelpExplainsReplacementAndDownloadBehavior(t *testing.T) {
	cases := []struct {
		resource, action string
		want             []string
	}{
		{"invoice-charge", "update", []string{"PUT /invoice/:id/charges", "omitted from the body are deleted", "--yes", "--data", "charges"}},
		{"expense-payment", "update", []string{"entire payments list", "--id", "payments"}},
		{"invoice", "email", []string{"email recipients", "reply_to", "--dry-run"}},
		{"invoice", "pdf", []string{"Base64", "--save", "invoice.pdf"}},
		{"customer", "create", []string{"Some fields are conditional", "business_type", "tax_id"}},
	}
	for _, tc := range cases {
		t.Run(tc.resource+" "+tc.action, func(t *testing.T) {
			app, out := helpApp()
			for _, op := range resourceOperations(tc.resource) {
				if op.Action == tc.action {
					app.operationHelp(op)
					break
				}
			}
			for _, want := range tc.want {
				if !strings.Contains(out.String(), want) {
					t.Errorf("help omits %q", want)
				}
			}
		})
	}
}

func TestAPIListIncludesCompleteCatalog(t *testing.T) {
	app, out := helpApp()
	if err := app.apiCommands([]string{"api", "list"}, false, "json"); err != nil {
		t.Fatal(err)
	}
	var got []catalog.Operation
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	want := catalog.Operations()
	if len(got) != len(want) {
		t.Fatalf("got %d operations; want %d", len(got), len(want))
	}
	for i, op := range want {
		if got[i].Method != op.Method || got[i].Path != op.Path {
			t.Errorf("operation %d = %s %s; want %s %s", i, got[i].Method, got[i].Path, op.Method, op.Path)
		}
	}
	if err := app.apiCommands([]string{"api", "unknown"}, false, "json"); err == nil {
		t.Fatal("unknown API subcommand should fail")
	}
}

func TestCompletionScriptsParse(t *testing.T) {
	for _, shell := range []string{"bash", "zsh", "fish"} {
		t.Run(shell, func(t *testing.T) {
			binary, err := exec.LookPath(shell)
			if err != nil {
				t.Skip(shell + " is unavailable")
			}
			app, out := helpApp()
			if err := app.completion([]string{"completion", shell}, false); err != nil {
				t.Fatal(err)
			}
			cmd := exec.Command(binary, "-n")
			cmd.Stdin = strings.NewReader(out.String())
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("invalid %s completion: %v\n%s", shell, err, output)
			}
		})
	}
	app, out := helpApp()
	if err := app.completion([]string{"completion", "powershell"}, false); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "Register-ArgumentCompleter") {
		t.Fatal("PowerShell completion was not generated")
	}
	if err := app.completion([]string{"completion", "unknown"}, false); err == nil {
		t.Fatal("unknown completion shell should fail")
	}
}

func TestBashCompletionUsesCommandContext(t *testing.T) {
	binary, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash is unavailable")
	}
	app, out := helpApp()
	if err := app.completion([]string{"completion", "bash"}, false); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		words []string
		want  []string
		avoid []string
	}{
		{[]string{"cuentica", "in"}, []string{"invoice", "income"}, nil},
		{[]string{"cuentica", "invoice", "p"}, []string{"pdf", "public"}, []string{"provider"}},
		{[]string{"cuentica", "--output", "json", "invoice", "p"}, []string{"pdf", "public"}, nil},
		{[]string{"cuentica", "--q", "Acme", "provider", "l"}, []string{"list"}, nil},
		{[]string{"cuentica", "provider", "list", "--q"}, []string{"--q", "--quiet"}, []string{"-q"}},
		{[]string{"cuentica", "invoice", "get", "--u"}, nil, []string{"--upload"}},
		{[]string{"cuentica", "invoice", "get", "--output", "j"}, []string{"json"}, []string{"text"}},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.words, " "), func(t *testing.T) {
			words := make([]string, len(tc.words))
			for i, word := range tc.words {
				words[i] = shellQuote(word)
			}
			probe := fmt.Sprintf("\nCOMP_WORDS=(%s)\nCOMP_CWORD=%d\n_cuentica_complete\nprintf '%%s\\n' \"${COMPREPLY[@]}\"\n", strings.Join(words, " "), len(words)-1)
			cmd := exec.Command(binary, "-s")
			cmd.Stdin = strings.NewReader(out.String() + probe)
			output, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("completion failed: %v\n%s", err, output)
			}
			completions := strings.Fields(string(output))
			for _, want := range tc.want {
				if !slices.Contains(completions, want) {
					t.Errorf("got %q; want %q", completions, want)
				}
			}
			for _, avoid := range tc.avoid {
				if slices.Contains(completions, avoid) {
					t.Errorf("got unexpected completion %q", avoid)
				}
			}
		})
	}
}

func TestFishCompletionUsesCommandContext(t *testing.T) {
	binary, err := exec.LookPath("fish")
	if err != nil {
		t.Skip("fish is unavailable")
	}
	app, out := helpApp()
	if err := app.completion([]string{"completion", "fish"}, false); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		command string
		want    []string
		avoid   []string
	}{
		{"cuentica in", []string{"invoice", "income"}, []string{"pdf"}},
		{"cuentica invoice p", []string{"pdf", "public"}, []string{"provider"}},
		{"cuentica --output json invoice p", []string{"pdf", "public"}, nil},
		{"cuentica --q Acme provider l", []string{"list"}, nil},
		{"cuentica invoice get --u", nil, []string{"--upload"}},
		{"cuentica provider list --q", []string{"--q", "--quiet"}, []string{"-q"}},
	}
	for _, tc := range cases {
		t.Run(tc.command, func(t *testing.T) {
			cmd := exec.Command(binary, "--no-config", "-c", out.String()+"\ncomplete -C "+fishQuote(tc.command)+"\n")
			result, err := cmd.CombinedOutput()
			if err != nil {
				t.Fatalf("completion failed: %v\n%s", err, result)
			}
			completions := strings.Fields(string(result))
			for _, want := range tc.want {
				if !slices.Contains(completions, want) {
					t.Errorf("got %q; want %q", result, want)
				}
			}
			for _, avoid := range tc.avoid {
				if slices.Contains(completions, avoid) {
					t.Errorf("got unexpected completion %q", avoid)
				}
			}
		})
	}
}
