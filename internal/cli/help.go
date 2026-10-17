package cli

import (
	"fmt"
	"io"
	"sort"
	"strings"

	"github.com/magarcia/cuentica-cli/internal/catalog"
	"github.com/magarcia/cuentica-cli/internal/output"
)

const apiDocsURL = "https://apidocs.cuentica.com/versions/latest_release/"

func (a *App) rootHelp(full bool) {
	fmt.Fprintln(a.Out, `cuentica - Use the Cuéntica API from your terminal

Examples:
  cuentica auth login
  cuentica invoice list --all
  cuentica invoice get --id 123 --output json

Usage:
  cuentica <resource> <action> [flags]`)
	if !full {
		fmt.Fprintln(a.Out, `
Common commands:
  auth       Save, inspect, or remove an API key
  invoice    List, create, update, and download invoices
  expense    Manage expenses, payments, and attachments
  customer   Manage customers
  provider   Manage providers
  document   Manage uploaded documents

Run cuentica --help for every resource and flag.
Run cuentica <resource> <action> --help for examples and API fields.
Run cuentica help environment for authentication settings.`)
		return
	}
	fmt.Fprintf(a.Out, "\nAPI resources (%d operations):\n", len(catalog.Operations()))
	for _, resource := range apiResources() {
		var actions []string
		for _, op := range resourceOperations(resource) {
			actions = append(actions, op.Action)
		}
		fmt.Fprintf(a.Out, "  %-22s %s\n", resource, strings.Join(actions, ", "))
	}
	fmt.Fprintln(a.Out, `
Other commands:
  auth login|status|logout   Manage the API key in the OS keyring
  api list                   Show the complete operation catalog
  completion <shell>         Generate bash, zsh, fish, or powershell completion
  help <resource> [action]    Show command help
  help environment           Explain environment variables and authentication
  help exit-codes            Explain process exit codes

Flags:`)
	fs, _ := a.flags(nil)
	fmt.Fprint(a.Out, fs.FlagUsages())
	fmt.Fprintf(a.Out, "\nAPI reference: %s\n", apiDocsURL)
}

func (a *App) resourceHelp(resource string) {
	operations := resourceOperations(resource)
	fmt.Fprintln(a.Out, "Examples:")
	if len(operations) > 0 {
		fmt.Fprintf(a.Out, "  %s\n", operationExample(operations[0]))
	}
	fmt.Fprintf(a.Out, "  cuentica %s <action> --help\n\nUsage:\n  cuentica %s <action> [flags]\n\nActions:\n", resource, resource)
	for _, op := range operations {
		fmt.Fprintf(a.Out, "  %-10s %s\n", op.Action, op.Summary)
	}
	fmt.Fprintf(a.Out, "\nAPI reference: %s\n", apiDocsURL)
}

func (a *App) operationHelp(op catalog.Operation) {
	fmt.Fprintf(a.Out, "Examples:\n  %s\n\n%s\n\nUsage:\n  cuentica %s %s [flags]\n\nAPI: %s %s\n", operationExample(op), op.Summary, op.Resource, op.Action, op.Method, op.Path)
	if params := pathParameters(op.Path); len(params) > 0 {
		var flags []string
		for _, param := range params {
			flags = append(flags, "--"+strings.ReplaceAll(param, "_", "-"))
		}
		fmt.Fprintf(a.Out, "Required identifiers: %s\n", strings.Join(flags, ", "))
	}
	if op.Body {
		fmt.Fprintln(a.Out, "\nJSON body:")
		if len(op.Required) > 0 {
			fmt.Fprintf(a.Out, "  Documented required fields: %s\n", strings.Join(op.Required, ", "))
		}
		if len(op.Optional) > 0 {
			fmt.Fprintf(a.Out, "  Documented optional fields: %s\n", strings.Join(op.Optional, ", "))
		}
		fmt.Fprintln(a.Out, "  Some fields are conditional; Cuéntica validates the complete body.")
		fmt.Fprintln(a.Out, "  Use --data JSON, --data @file, or --data - for stdin.")
		fmt.Fprintln(a.Out, "  Repeat --field key=value to supply individual fields.")
	}
	if op.Resource == "document" && op.Action == "update" {
		fmt.Fprintln(a.Out, "\nThe API describes updates to date and expense_id only; its field table also lists attachment.")
	}
	if op.Paginated {
		fmt.Fprintln(a.Out, "\nUse --page and --page-size (maximum 300), or --all to fetch every page from page 1.")
	}
	if op.Binary {
		fmt.Fprintln(a.Out, "\nThe API returns Base64. Use --save FILE to decode it, or --save - for binary stdout.")
	}
	if op.Destructive {
		fmt.Fprintf(a.Out, "\n%s\nConfirm interactively or pass --yes. Use --dry-run to preview the request.\n", destructiveHelp(op))
	}
	fmt.Fprintln(a.Out, "\nFlags:")
	fs, _ := a.flags(&op)
	fmt.Fprint(a.Out, fs.FlagUsages())
	fmt.Fprintf(a.Out, "\nAPI reference: %s\n", apiDocsURL)
}

func destructiveHelp(op catalog.Operation) string {
	switch {
	case strings.HasSuffix(op.Path, "/charges"):
		return "This replaces the entire charges list. Existing charges omitted from the body are deleted."
	case strings.HasSuffix(op.Path, "/payments"):
		return "This replaces the entire payments list. Existing payments omitted from the body are deleted."
	case op.Action == "email":
		return "This sends the invoice to the email recipients in the body."
	case op.Action == "void":
		return "This voids an issued invoice."
	case op.Action == "delete":
		return "This deletes the selected resource."
	case strings.HasSuffix(op.Resource, "-attachment"):
		return "This replaces the existing attachment."
	case op.Resource == "invoice":
		return "This replaces invoice lines and charges. Existing entries omitted from the body are deleted."
	case op.Resource == "income":
		return "This replaces income lines and charges. Existing entries omitted from the body are deleted."
	case op.Resource == "expense":
		return "This replaces expense lines and payments. Existing entries omitted from the body are deleted."
	default:
		return "This changes existing data."
	}
}

func operationExample(op catalog.Operation) string {
	example := "cuentica " + op.Resource + " " + op.Action
	for _, param := range pathParameters(op.Path) {
		example += " --" + strings.ReplaceAll(param, "_", "-") + " 123"
	}
	switch {
	case op.Binary:
		filename := "attachment.bin"
		if op.Action == "pdf" {
			filename = "invoice.pdf"
		}
		example += " --save " + filename
	case op.Body:
		example += " --data @request.json --dry-run"
	case op.Destructive:
		example += " --dry-run"
	case op.Paginated:
		example += " --all --output json"
	default:
		example += " --output json"
	}
	return example
}

func (a *App) topic(name string) error {
	var text string
	switch name {
	case "environment":
		text = `Examples:
  cuentica auth login
  cuentica auth status
  cuentica company get

Environment:
  CUENTICA_API_KEY  API token; takes precedence over the OS keyring.
  NO_COLOR         Disables color; output is plain by default.

Authentication:
  Generate a token in Cuéntica's API settings for the desired company.
  Run cuentica auth login to save it securely in the OS keyring.
  For automation, provide CUENTICA_API_KEY through your environment or use
  cuentica auth login --key-stdin or --key-file FILE.
  Run cuentica auth logout to remove the stored key.

  Tokens belong to one user and company. Sandbox companies have separate tokens.
  Keys are sent in X-AUTH-TOKEN over HTTPS.
  Keep API keys out of command-line arguments and shell history.

Output:
  Terminal output defaults to text; piped output defaults to JSON.
  Use --output text, json, or llm to select a format.
  stdout contains results; stderr contains errors and status messages.
`
	case "exit-codes":
		text = `Examples:
  cuentica company get --output json
  cuentica invoice get --id 123 --no-input

Exit codes:
  0    Success, help, or a closed output pipe
  1    API or file error, or cancelled action
  2    Invalid command, flag, or request input
  3    Missing API key, keyring error, or authentication/permission failure
  4    Requested API resource not found
  10   Network error or request timeout
  130  Interrupted with SIGINT
  143  Terminated with SIGTERM

Errors include a namespaced code and a next step on stderr.
Use --output json for structured errors.
`
	default:
		return usage("Unknown help topic. Use environment or exit-codes.")
	}
	_, err := io.WriteString(a.Out, text)
	return err
}

func (a *App) apiCommands(words []string, help bool, format string) error {
	if len(words) > 2 || len(words) == 2 && words[1] != "list" {
		return usage("Use cuentica api list to inspect the API operation catalog.")
	}
	if help || len(words) == 1 {
		_, err := fmt.Fprintln(a.Out, `Examples:
  cuentica api list --output json
  cuentica invoice create --help

Usage:
  cuentica api list [--output text|json|llm]

Lists every documented method, path, query parameter, and body field.
Use cuentica <resource> <action> --help for command usage.`)
		return err
	}
	return output.Write(a.Out, format, catalog.Operations())
}

func apiResources() []string {
	seen := make(map[string]bool)
	for _, op := range catalog.Operations() {
		seen[op.Resource] = true
	}
	resources := make([]string, 0, len(seen))
	for resource := range seen {
		resources = append(resources, resource)
	}
	sort.Strings(resources)
	return resources
}

func resourceOperations(resource string) []catalog.Operation {
	var operations []catalog.Operation
	for _, op := range catalog.Operations() {
		if op.Resource == resource {
			operations = append(operations, op)
		}
	}
	return operations
}
