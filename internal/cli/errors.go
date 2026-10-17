package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"github.com/magarcia/cuentica-cli/internal/api"
)

type Error struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
	Status  int    `json:"status,omitempty"`
	Exit    int    `json:"-"`
}

func (e *Error) Error() string { return e.Message }

func problem(code, message, hint string, exit int) error {
	return &Error{Code: code, Message: message, Hint: hint, Exit: exit}
}

func usage(message string) error {
	return problem("CLI-001", message, "Run cuentica <resource> <action> --help for usage.", 2)
}

func classify(err error) *Error {
	var known *Error
	if errors.As(err, &known) {
		return known
	}
	var remote *api.Error
	if errors.As(err, &remote) {
		e := &Error{Code: "API-001", Message: fmt.Sprintf("Cuéntica returned HTTP %d.", remote.Status), Hint: "Check your request against the API documentation.", Exit: 1, Status: remote.Status}
		switch remote.Status {
		case 401, 403:
			e.Code, e.Exit, e.Hint = "AUTH-002", 3, "Check CUENTICA_API_KEY or run cuentica auth login. Tokens need permission for this operation."
		case 404:
			e.Code, e.Exit, e.Hint = "API-404", 4, "Check the resource ID and the company associated with your API key."
		case 429:
			e.Code, e.Hint = "API-429", "The API rate limit was reached. Wait before retrying."
			if remote.RetryAfter != "" {
				e.Hint += " Retry-After: " + remote.RetryAfter
			}
		default:
			if remote.Status >= 500 {
				e.Hint = "Cuéntica is temporarily unavailable. Check whether a write succeeded before retrying it."
			}
		}
		if remote.Body != "" {
			e.Message += " " + remote.Body
		}
		return e
	}
	return &Error{Code: "NET-001", Message: "The request could not be completed.", Hint: "Check your connection and try again. Use --timeout to adjust the request deadline.", Exit: 10}
}

func Report(w io.Writer, err error, format string) int {
	e := classify(err)
	if format == "json" {
		_ = json.NewEncoder(w).Encode(map[string]any{"error": e})
	} else {
		fmt.Fprintf(w, "Error [%s]: %s\n", e.Code, e.Message)
		if e.Hint != "" {
			fmt.Fprintln(w, e.Hint)
		}
	}
	return e.Exit
}
