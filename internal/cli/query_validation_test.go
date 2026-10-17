package cli

import (
	"context"
	"net/http"
	"reflect"
	"testing"
)

func TestConstrainedQueriesRejectEveryInvalidValueBeforeAuthentication(t *testing.T) {
	for _, parameter := range []struct {
		name, resource, valid string
	}{
		{"sort", "invoice", "asc"},
		{"issued", "invoice", "true"},
		{"draft", "expense", "true"},
		{"assigned", "document", "true"},
	} {
		for _, input := range []struct {
			name string
			args []string
		}{
			{"dedicated plus invalid query", []string{"--" + parameter.name, parameter.valid, "--query", parameter.name + "=invalid"}},
			{"repeated queries with invalid value", []string{"--query", parameter.name + "=" + parameter.valid, "--query", parameter.name + "=invalid"}},
			{"empty dedicated value", []string{"--" + parameter.name + "="}},
			{"empty query value", []string{"--query", parameter.name + "="}},
			{"dedicated plus empty query", []string{"--" + parameter.name, parameter.valid, "--query", parameter.name + "="}},
		} {
			for _, dryRun := range []bool{false, true} {
				name := parameter.name + "/" + input.name
				if dryRun {
					name += "/dry-run"
				}
				t.Run(name, func(t *testing.T) {
					requests, environmentReads := 0, 0
					app, stdout, stderr, store := newTestApp(t, func(*http.Request) (*http.Response, error) {
						requests++
						return testResponse(http.StatusOK, `[]`), nil
					})
					app.Getenv = func(string) string {
						environmentReads++
						return ""
					}
					args := append([]string{parameter.resource, "list", "--output", "json"}, input.args...)
					if dryRun {
						args = append(args, "--dry-run")
					}
					if exit := app.Run(context.Background(), args); exit != 2 || stdout.Len() != 0 {
						t.Fatalf("exit=%d stdout=%q stderr=%s; want usage error without output", exit, stdout, stderr)
					}
					if requests != 0 || environmentReads != 0 || store.gets != 0 {
						t.Errorf("invalid input reached HTTP or credentials: requests=%d environment reads=%d keyring reads=%d", requests, environmentReads, store.gets)
					}
					assertExecuteErrorCode(t, stderr.Bytes(), "CLI-001")
				})
			}
		}
	}
}

func TestConstrainedQueriesPreserveValidRepeatsAndUnknownParameters(t *testing.T) {
	for _, parameter := range []struct {
		name, resource, first, second string
	}{
		{"sort", "invoice", "asc", "desc"},
		{"issued", "invoice", "true", "false"},
		{"draft", "expense", "true", "false"},
		{"assigned", "document", "true", "false"},
	} {
		t.Run(parameter.name, func(t *testing.T) {
			requests := 0
			app, _, stderr, _ := newTestApp(t, func(request *http.Request) (*http.Response, error) {
				requests++
				query := request.URL.Query()
				if got, want := query[parameter.name], []string{parameter.first, parameter.second, parameter.first}; !reflect.DeepEqual(got, want) {
					t.Errorf("%s values = %q; want %q", parameter.name, got, want)
				}
				if got := query["future"]; !reflect.DeepEqual(got, []string{"one", "two"}) {
					t.Errorf("unknown query values = %q", got)
				}
				if got := query["future-empty"]; !reflect.DeepEqual(got, []string{""}) {
					t.Errorf("unknown empty query = %q", got)
				}
				return testResponse(http.StatusOK, `[]`), nil
			})
			args := []string{parameter.resource, "list", "--output", "json",
				"--" + parameter.name, parameter.first,
				"--query", parameter.name + "=" + parameter.second,
				"--query", parameter.name + "=" + parameter.first,
				"--query", "future=one", "--query", "future=two", "--query", "future-empty="}
			if exit := app.Run(context.Background(), args); exit != 0 || requests != 1 {
				t.Fatalf("exit=%d requests=%d stderr=%s; want one successful request", exit, requests, stderr)
			}
		})
	}
}
