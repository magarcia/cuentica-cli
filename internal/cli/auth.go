package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/magarcia/cuentica-cli/internal/auth"
	"github.com/magarcia/cuentica-cli/internal/output"
	"github.com/spf13/pflag"
)

func (a *App) authenticate(ctx context.Context, action string, fs *pflag.FlagSet, o *options) error {
	if action != "login" && action != "status" && action != "logout" {
		return usage(a.unknownCommand([]string{"auth", action}))
	}
	allowed := map[string]bool{
		"help": true, "version": true, "verbose": true, "quiet": true,
		"no-color": true, "no-input": true, "yes": true, "output": true, "timeout": true,
	}
	if action == "login" {
		allowed["key-stdin"], allowed["key-file"] = true, true
	}
	var invalid []string
	fs.Visit(func(f *pflag.Flag) {
		if !allowed[f.Name] {
			invalid = append(invalid, "--"+f.Name)
		}
	})
	if len(invalid) > 0 {
		return usage("Unsupported flags for auth " + action + ": " + strings.Join(invalid, ", "))
	}
	if o.timeout <= 0 {
		return usage("--timeout must be greater than zero.")
	}

	switch action {
	case "login":
		if fs.Changed("key-stdin") && fs.Changed("key-file") {
			return usage("Use either --key-stdin or --key-file, not both.")
		}
		var reader io.Reader
		var file *os.File
		switch {
		case o.keyStdin:
			reader = a.In
		case fs.Changed("key-file"):
			if o.keyFile == "" {
				return usage("--key-file requires a file path.")
			}
			var err error
			file, err = os.Open(o.keyFile)
			if err != nil {
				return problem("AUTH-002", "Could not read the API key file.", "Check that --key-file names a readable file.", 3)
			}
			defer file.Close()
			reader = file
		default:
			if !a.InputTTY || o.noInput {
				return usage("Provide the API key with --key-stdin or --key-file, or run auth login in a terminal.")
			}
		}

		var token string
		if reader != nil {
			const maxKeyInputBytes = 2048 + len("\r\n")
			value, err := io.ReadAll(io.LimitReader(reader, int64(maxKeyInputBytes)+1))
			if err != nil {
				return problem("AUTH-002", "Could not read the API key.", "Provide a readable key file or pipe one key to --key-stdin.", 3)
			}
			if len(value) > maxKeyInputBytes {
				return problem("AUTH-002", "API key exceeds the maximum length of 2048 bytes.", "Provide only the API key, with at most one trailing newline.", 3)
			}
			token = string(value)
		} else {
			fmt.Fprint(a.Err, "API key: ")
			var err error
			token, err = a.ReadPassword()
			fmt.Fprintln(a.Err)
			if err != nil {
				return problem("AUTH-002", "Could not read the API key from the terminal.", "Try --key-stdin or --key-file.", 3)
			}
		}
		if strings.HasSuffix(token, "\n") {
			token = strings.TrimSuffix(strings.TrimSuffix(token, "\n"), "\r")
		}
		if err := auth.Validate(token); err != nil {
			return problem("AUTH-002", err.Error(), "Provide one API key without spaces or extra lines.", 3)
		}
		if err := a.verifyKey(ctx, token, o); err != nil {
			return err
		}
		if err := a.Store.Set(token); err != nil {
			return keyringProblem("Could not save the API key to the OS keyring.")
		}
		if a.Getenv("CUENTICA_API_KEY") != "" && !o.quiet {
			fmt.Fprintln(a.Err, "CUENTICA_API_KEY is set and takes precedence over the saved key.")
		}
		return output.Write(a.Out, o.output, map[string]any{"authenticated": true, "source": "keyring"})
	case "status":
		if token := a.Getenv("CUENTICA_API_KEY"); token != "" {
			if err := auth.Validate(token); err != nil {
				return problem("AUTH-002", "CUENTICA_API_KEY contains an invalid API key.", "Set it to one key without whitespace, or unset it to use the OS keyring.", 3)
			}
		}
		token, source, err := auth.Resolve(a.Getenv, a.Store)
		if errors.Is(err, auth.ErrNotFound) {
			return problem("AUTH-001", "No API key is configured.", "Set CUENTICA_API_KEY or run cuentica auth login.", 3)
		}
		if err != nil {
			return keyringProblem("Could not load a usable API key from the OS keyring.")
		}
		if err := a.verifyKey(ctx, token, o); err != nil {
			return err
		}
		return output.Write(a.Out, o.output, map[string]any{"authenticated": true, "source": source})
	case "logout":
		if err := a.Store.Delete(); err != nil && !errors.Is(err, auth.ErrNotFound) {
			return keyringProblem("Could not delete the saved API key from the OS keyring.")
		}
		if a.Getenv("CUENTICA_API_KEY") != "" && !o.quiet {
			fmt.Fprintln(a.Err, "CUENTICA_API_KEY is still set. Unset it to stop authenticating through the environment.")
		}
		return output.Write(a.Out, o.output, map[string]any{"stored": false})
	}
	return nil
}

func keyringProblem(message string) error {
	return problem("AUTH-003", message, "Unlock or enable your OS keyring, or use CUENTICA_API_KEY instead.", 3)
}

func (a *App) verifyKey(ctx context.Context, token string, o *options) error {
	client, err := a.client(token, o.timeout)
	if err != nil {
		return usage(err.Error())
	}
	if o.verbose && !o.quiet {
		fmt.Fprintln(a.Err, "GET /company")
	} else if a.ErrorTTY && !o.quiet {
		fmt.Fprintln(a.Err, "Verifying API key...")
	}
	response, err := client.Do(ctx, http.MethodGet, "/company", nil, nil)
	if response != nil && o.verbose && !o.quiet {
		fmt.Fprintf(a.Err, "HTTP %d\n", response.Status)
	}
	return err
}

func (a *App) authHelp() {
	io.WriteString(a.Out, `Examples:
  cuentica auth login
  printf '%s' "$CUENTICA_API_KEY" | cuentica auth login --key-stdin
  cuentica auth login --key-file ./api-key
  cuentica auth status --output json
  cuentica auth logout

Usage: cuentica auth <login|status|logout> [flags]

login   Verify an API key and save it in the OS keyring.
status  Verify the active API key without displaying it.
logout  Delete the saved API key; CUENTICA_API_KEY remains active.

Login flags:
  --key-stdin       Read one API key from stdin.
  --key-file PATH   Read one API key from a file.

Global flags:
  --no-input        Never prompt; use --key-stdin or --key-file to log in.
  --output FORMAT   text, json, or llm.
  --timeout VALUE   HTTP request timeout (default 30s).
  --quiet, -q       Suppress progress and status messages.
  --verbose, -v     Show HTTP method, path, and response status.

CUENTICA_API_KEY takes precedence over the OS keyring. Without an input flag,
login securely prompts in a terminal. API keys are never accepted as flags.
`)
}
