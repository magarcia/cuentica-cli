package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strings"
	"syscall"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/magarcia/cuentica-cli/internal/api"
	"github.com/magarcia/cuentica-cli/internal/auth"
	"github.com/magarcia/cuentica-cli/internal/catalog"
	"github.com/spf13/pflag"
)

type App struct {
	In                            io.Reader
	Out, Err                      io.Writer
	Getenv                        func(string) string
	Store                         auth.Store
	BaseURL                       string
	Version                       string
	InputTTY, OutputTTY, ErrorTTY bool
	ReadPassword                  func() (string, error)
	Transport                     http.RoundTripper
}

func New(version string) *App {
	return &App{In: os.Stdin, Out: os.Stdout, Err: os.Stderr, Getenv: os.Getenv,
		Store: auth.Keyring{}, BaseURL: api.DefaultBaseURL, Version: version,
		InputTTY: term.IsTerminal(os.Stdin.Fd()), OutputTTY: term.IsTerminal(os.Stdout.Fd()), ErrorTTY: term.IsTerminal(os.Stderr.Fd()),
		ReadPassword: func() (string, error) { b, err := term.ReadPassword(os.Stdin.Fd()); return string(b), err },
	}
}

type options struct {
	help, version, quiet, verbose, noInput, yes, all, dryRun, keyStdin bool
	output, data, save, upload, keyFile                                string
	fields, queries                                                    []string
	timeout                                                            time.Duration
	values                                                             map[string]*string
}

func (a *App) flags(op *catalog.Operation) (*pflag.FlagSet, *options) {
	o := &options{values: map[string]*string{}}
	fs := pflag.NewFlagSet("cuentica", pflag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.BoolVarP(&o.help, "help", "h", false, "Show help")
	fs.BoolVar(&o.version, "version", false, "Print version, commit and build date")
	fs.BoolVarP(&o.verbose, "verbose", "v", false, "Show HTTP method, path, and response status on stderr")
	fs.BoolVarP(&o.quiet, "quiet", "q", false, "Suppress progress and status messages")
	fs.Bool("no-color", false, "Disable color (output is plain by default)")
	fs.BoolVar(&o.noInput, "no-input", false, "Never prompt; fail when confirmation is needed")
	fs.BoolVarP(&o.yes, "yes", "y", false, "Confirm destructive actions and email delivery")
	def := "json"
	if a.OutputTTY {
		def = "text"
	}
	fs.StringVarP(&o.output, "output", "o", def, "Output format: text, json, llm")
	fs.DurationVar(&o.timeout, "timeout", 30*time.Second, "Timeout for each HTTP request")
	if op == nil || op.Body {
		fs.StringVarP(&o.data, "data", "d", "", "JSON body, @file, or - for stdin")
		fs.StringArrayVarP(&o.fields, "field", "f", nil, "Body field key=value; value parsed as JSON when valid (repeatable)")
		fs.StringVar(&o.upload, "upload", "", "Encode a file as a Base64 attachment")
	}
	if op == nil || op.Binary {
		fs.StringVar(&o.save, "save", "", "Decode a Base64 response to a new file, or - for stdout")
	}
	if op == nil || op.Paginated {
		fs.BoolVar(&o.all, "all", false, "Fetch all pages from page 1")
	}
	if op == nil || len(op.Query) > 0 {
		fs.StringArrayVar(&o.queries, "query", nil, "Query parameter key=value (repeatable; preserves API spelling)")
	}
	fs.BoolVar(&o.dryRun, "dry-run", false, "Validate and show method/path/body without sending a request")
	if op == nil {
		fs.BoolVar(&o.keyStdin, "key-stdin", false, "Read the API key from stdin for auth login")
		fs.StringVar(&o.keyFile, "key-file", "", "Read the API key from a file for auth login")
	}
	add := func(name string) {
		flag := strings.ReplaceAll(name, "_", "-")
		if fs.Lookup(flag) == nil {
			o.values[flag] = fs.String(flag, "", "API parameter: "+name)
		}
	}
	if op != nil {
		for _, p := range pathParameters(op.Path) {
			add(p)
		}
		for _, q := range op.Query {
			add(q)
		}
	} else {
		for _, item := range catalog.Operations() {
			for _, p := range pathParameters(item.Path) {
				add(p)
			}
			for _, q := range item.Query {
				add(q)
			}
		}
	}
	return fs, o
}

func pathParameters(path string) []string {
	var params []string
	for _, part := range strings.Split(path, "/") {
		if strings.HasPrefix(part, ":") {
			params = append(params, strings.TrimPrefix(part, ":"))
		}
	}
	return params
}

func (a *App) Run(ctx context.Context, args []string) int {
	run := *a
	run.Out = &commandOutput{writer: a.Out}
	return run.run(ctx, args)
}

func (a *App) run(ctx context.Context, args []string) int {
	fs, o := a.flags(nil)
	err := fs.Parse(args)
	if err != nil {
		return Report(a.Err, usage(err.Error()), o.output)
	}
	if o.output != "json" && o.output != "text" && o.output != "llm" {
		return Report(a.Err, usage("--output must be text, json, or llm."), "text")
	}
	if o.version {
		fmt.Fprintln(a.Out, a.Version)
		return a.finish(nil, o.output)
	}
	words := fs.Args()
	if len(words) > 0 && words[0] == "help" {
		o.help = true
		words = words[1:]
	}
	if len(words) == 0 {
		a.rootHelp(o.help)
		return a.finish(nil, o.output)
	}
	if words[0] == "completion" {
		err = a.completion(words, o.help)
		return a.finish(err, o.output)
	}
	if words[0] == "environment" || words[0] == "exit-codes" {
		err = a.topic(words[0])
		return a.finish(err, o.output)
	}
	if words[0] == "auth" {
		if o.help || len(words) == 1 {
			a.authHelp()
			return a.finish(nil, o.output)
		}
		if len(words) != 2 {
			return a.finish(usage("Use cuentica auth login, status, or logout."), o.output)
		}
		err = a.authenticate(ctx, words[1], fs, o)
		return a.finish(err, o.output)
	}
	if words[0] == "api" {
		err = a.apiCommands(words, o.help, o.output)
		return a.finish(err, o.output)
	}
	var op *catalog.Operation
	operations := catalog.Operations()
	for i := range operations {
		if operations[i].Resource == words[0] && len(words) > 1 && operations[i].Action == words[1] {
			op = &operations[i]
			break
		}
	}
	if op == nil {
		resources := map[string]bool{}
		for _, item := range operations {
			resources[item.Resource] = true
		}
		if resources[words[0]] && (o.help || len(words) == 1) {
			a.resourceHelp(words[0])
			return a.finish(nil, o.output)
		}
		return a.finish(usage(a.unknownCommand(words)), o.output)
	}
	if len(words) != 2 {
		return a.finish(usage("Use named flags for identifiers, such as --id 123."), o.output)
	}
	if o.help {
		a.operationHelp(*op)
		return a.finish(nil, o.output)
	}
	allowed, _ := a.flags(op)
	var invalid []string
	fs.Visit(func(f *pflag.Flag) {
		if allowed.Lookup(f.Name) == nil {
			invalid = append(invalid, "--"+f.Name)
		}
	})
	if len(invalid) > 0 {
		return a.finish(usage("Unsupported flags for this operation: "+strings.Join(invalid, ", ")), o.output)
	}
	err = a.execute(ctx, *op, fs, o)
	return a.finish(err, o.output)
}

func (a *App) finish(err error, format string) int {
	if output, ok := a.Out.(*commandOutput); ok && output.err != nil {
		if errors.Is(output.err, syscall.EPIPE) {
			return 0
		}
		err = problem("IO-001", "Could not write to stdout.", "Check the output destination, disk space, and permissions.", 1)
	}
	if err == nil {
		return 0
	}
	return Report(a.Err, err, format)
}

type commandOutput struct {
	writer io.Writer
	err    error
}

func (w *commandOutput) Write(data []byte) (int, error) {
	if w.err != nil {
		return 0, w.err
	}
	n, err := w.writer.Write(data)
	if err == nil && n != len(data) {
		err = io.ErrShortWrite
	}
	w.err = err
	return n, err
}

func (a *App) client(token string, timeout time.Duration) (*api.Client, error) {
	c, err := api.New(a.BaseURL, token, timeout)
	if err == nil && a.Transport != nil {
		c.SetTransport(a.Transport)
	}
	return c, err
}

func (a *App) unknownCommand(words []string) string {
	name := strings.Join(words, " ")
	candidates := []string{"auth login", "auth logout", "auth status", "api list", "completion"}
	for _, op := range catalog.Operations() {
		candidates = append(candidates, op.Resource+" "+op.Action)
	}
	sort.Strings(candidates)
	best, dist := "", 100
	for _, candidate := range candidates {
		if n := distance(name, candidate); n < dist {
			best, dist = candidate, n
		}
	}
	if dist <= 3 {
		return fmt.Sprintf("Unknown command %q. Did you mean 'cuentica %s'?", name, best)
	}
	return fmt.Sprintf("Unknown command %q. Run cuentica --help to list commands.", name)
}

func distance(a, b string) int {
	previous := make([]int, len(b)+1)
	for j := range previous {
		previous[j] = j
	}
	for i := 1; i <= len(a); i++ {
		current := make([]int, len(b)+1)
		current[0] = i
		for j := 1; j <= len(b); j++ {
			cost := 0
			if a[i-1] != b[j-1] {
				cost = 1
			}
			current[j] = min(current[j-1]+1, previous[j]+1, previous[j-1]+cost)
		}
		previous = current
	}
	return previous[len(b)]
}
