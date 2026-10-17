package cli

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/magarcia/cuentica-cli/internal/api"
	"github.com/magarcia/cuentica-cli/internal/auth"
	"github.com/magarcia/cuentica-cli/internal/catalog"
	"github.com/magarcia/cuentica-cli/internal/output"
	"github.com/spf13/pflag"
)

const (
	maxInputBytes  = 32 << 20
	maxExportBytes = 64 << 20
)

func (a *App) execute(ctx context.Context, op catalog.Operation, fs *pflag.FlagSet, o *options) error {
	if o.timeout <= 0 {
		return usage("--timeout must be greater than zero.")
	}
	path := op.Path
	for _, name := range pathParameters(path) {
		value := *o.values[strings.ReplaceAll(name, "_", "-")]
		if value == "" {
			return usage("Missing required flag --" + strings.ReplaceAll(name, "_", "-") + ".")
		}
		for _, r := range value {
			if r < '0' || r > '9' {
				return usage("--" + name + " must be a positive numeric ID.")
			}
		}
		if strings.TrimLeft(value, "0") == "" {
			return usage("--" + name + " must be a positive numeric ID.")
		}
		path = strings.ReplaceAll(path, ":"+name, value)
	}
	query, err := buildQuery(op, fs, o)
	if err != nil {
		return err
	}
	body, err := a.body(op, o)
	if err != nil {
		return err
	}
	if o.save != "" {
		if o.save == "-" && a.OutputTTY {
			return usage("Binary output needs a pipe or --save <file>.")
		}
		if o.save != "-" {
			if err := checkDestination(o.save); err != nil {
				return err
			}
		}
	}
	if o.dryRun {
		return output.Write(a.Out, o.output, map[string]any{"method": op.Method, "path": path, "query": query, "body": json.RawMessage(defaultJSON(body)), "requires_confirmation": op.Destructive})
	}
	if op.Destructive {
		if err := a.confirm(ctx, op.Method+" "+path+" can remove or replace data, or send an email.", o); err != nil {
			return err
		}
	}
	token, _, err := auth.Resolve(a.Getenv, a.Store)
	if err != nil {
		return credentialError(err)
	}
	client, err := a.client(token, o.timeout)
	if err != nil {
		return usage("Invalid request configuration: " + err.Error())
	}
	if o.all {
		result, err := a.allPages(ctx, client, op, path, query, o, maxExportBytes)
		if err != nil {
			return err
		}
		return output.Write(a.Out, o.output, result)
	}
	response, err := a.request(ctx, client, op.Method, path, query, body, o)
	if err != nil {
		return err
	}
	if o.save != "" {
		return a.save(response.Body, o.save, o)
	}
	value, err := responseValue(response, op.Binary)
	if err != nil {
		return err
	}
	return output.Write(a.Out, o.output, value)
}

func credentialError(err error) error {
	if errors.Is(err, auth.ErrNotFound) {
		return problem("AUTH-001", "No API key is configured.", "Set CUENTICA_API_KEY or run cuentica auth login.", 3)
	}
	return problem("AUTH-003", "The API key could not be loaded or is invalid.", "Check CUENTICA_API_KEY, unlock your OS keyring, or run cuentica auth login.", 3)
}

func buildQuery(op catalog.Operation, fs *pflag.FlagSet, o *options) (url.Values, error) {
	q := url.Values{}
	for _, name := range op.Query {
		flag := strings.ReplaceAll(name, "_", "-")
		if fs.Changed(flag) {
			q.Set(name, *o.values[flag])
		}
	}
	for _, entry := range o.queries {
		name, value, ok := strings.Cut(entry, "=")
		if !ok || name == "" {
			return nil, usage("--query requires key=value.")
		}
		if strings.EqualFold(name, "X-AUTH-TOKEN") || strings.Contains(strings.ToLower(name), "api_key") || strings.Contains(strings.ToLower(name), "api-key") {
			return nil, usage("Pass credentials through CUENTICA_API_KEY or auth login.")
		}
		q.Add(name, value)
	}
	for _, name := range []string{"page", "page_size"} {
		if values, exists := q[name]; exists {
			if len(values) != 1 {
				return nil, usage(name + " may only be specified once.")
			}
			n, err := strconv.Atoi(values[0])
			if err != nil || n < 1 || (name == "page_size" && n > 300) {
				return nil, usage(name + " must be positive; page_size cannot exceed 300.")
			}
		}
	}
	if o.all && q.Has("page") {
		return nil, usage("--all starts at page 1 and cannot be combined with --page or --query page=….")
	}
	for _, name := range []string{"initial_date", "end_date"} {
		for _, v := range q[name] {
			if _, err := time.Parse("2006-01-02", v); err != nil {
				return nil, usage(name + " must use YYYY-MM-DD.")
			}
		}
	}
	for _, value := range q["sort"] {
		if value != "asc" && value != "desc" {
			return nil, usage("--sort must be asc or desc.")
		}
	}
	for _, name := range []string{"issued", "draft", "assigned"} {
		for _, value := range q[name] {
			if value != "true" && value != "false" {
				return nil, usage("--" + name + " must be true or false.")
			}
		}
	}
	return q, nil
}

func (a *App) body(op catalog.Operation, o *options) ([]byte, error) {
	if !op.Body {
		return nil, nil
	}
	var object map[string]json.RawMessage
	if o.data != "" {
		var payload []byte
		var err error
		switch {
		case o.data == "-":
			if a.InputTTY {
				return nil, usage("--data - expects piped JSON on stdin.")
			}
			payload, err = readBounded(a.In, maxInputBytes)
		case strings.HasPrefix(o.data, "@"):
			payload, err = readFileBounded(strings.TrimPrefix(o.data, "@"), maxInputBytes)
		default:
			payload = []byte(o.data)
		}
		if err != nil {
			return nil, problem("IO-001", "Could not read the JSON body.", "Check the input file or pipe and keep it below 32 MiB.", 1)
		}
		if len(payload) > maxInputBytes {
			return nil, usage("JSON body exceeds 32 MiB.")
		}
		if err = json.Unmarshal(payload, &object); err != nil || object == nil {
			return nil, usage("--data must contain a JSON object. Use @file or - for stdin.")
		}
	}
	if object == nil {
		object = map[string]json.RawMessage{}
	}
	for _, field := range o.fields {
		key, value, ok := strings.Cut(field, "=")
		if !ok || key == "" {
			return nil, usage("--field requires key=value.")
		}
		raw := json.RawMessage(value)
		if !json.Valid(raw) {
			raw, _ = json.Marshal(value)
		}
		object[key] = raw
	}
	if o.upload != "" {
		allowed := op.Resource == "document" || op.Resource == "income" || op.Resource == "expense" || op.Resource == "income-attachment" || op.Resource == "expense-attachment"
		if !allowed {
			return nil, usage("--upload is supported for documents, income, expense, and their attachments.")
		}
		data, err := readFileBounded(o.upload, maxInputBytes)
		if err != nil {
			return nil, problem("IO-001", "Could not read the upload file.", "Check the file and keep it below 32 MiB.", 1)
		}
		attachment := map[string]string{"filename": filepath.Base(o.upload), "data": base64.StdEncoding.EncodeToString(data)}
		if strings.HasSuffix(op.Resource, "-attachment") {
			for key, value := range attachment {
				object[key], _ = json.Marshal(value)
			}
		} else {
			object["attachment"], _ = json.Marshal(attachment)
		}
	}
	if len(object) == 0 && o.data == "" {
		return nil, usage("This operation needs a JSON body. Use --data @file, --data -, --field key=value, or --upload.")
	}
	return json.Marshal(object)
}

func defaultJSON(body []byte) []byte {
	if body == nil {
		return []byte("null")
	}
	return body
}

func readBounded(r io.Reader, limit int64) ([]byte, error) {
	b, err := io.ReadAll(io.LimitReader(r, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > limit {
		return nil, errors.New("input too large")
	}
	return b, nil
}

func readFileBounded(path string, limit int64) ([]byte, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, errors.New("input must be a regular file")
	}
	return readBounded(f, limit)
}

func (a *App) confirm(ctx context.Context, message string, o *options) error {
	if o.yes {
		return nil
	}
	if o.noInput || !a.InputTTY {
		return problem("CLI-002", "This operation requires confirmation.", "Review the operation, then repeat it with --yes. Use --dry-run to preview the request.", 2)
	}
	fmt.Fprintln(a.Err, message)
	fmt.Fprint(a.Err, "Continue? [y/N] ")
	var line strings.Builder
	for line.Len() < 16 {
		var b [1]byte
		n, err := a.In.Read(b[:])
		if n > 0 {
			if b[0] == '\n' {
				break
			}
			line.WriteByte(b[0])
		}
		if err != nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	answer := strings.TrimSpace(line.String())
	if !strings.EqualFold(answer, "y") && !strings.EqualFold(answer, "yes") {
		return problem("CLI-003", "Operation cancelled.", "No request was sent.", 1)
	}
	return nil
}

func (a *App) request(ctx context.Context, c *api.Client, method, path string, q url.Values, body []byte, o *options) (*api.Response, error) {
	if o.verbose && !o.quiet {
		fmt.Fprintf(a.Err, "%s %s\n", method, path)
	} else if a.ErrorTTY && !o.quiet {
		fmt.Fprintf(a.Err, "%s %s…\n", method, path)
	}
	r, err := c.Do(ctx, method, path, q, body)
	if r != nil && o.verbose && !o.quiet {
		fmt.Fprintf(a.Err, "HTTP %d\n", r.Status)
	}
	return r, err
}

func responseValue(r *api.Response, binary bool) (any, error) {
	trimmed := bytes.TrimSpace(r.Body)
	if len(trimmed) == 0 {
		return nil, nil
	}
	if binary {
		if bytes.HasPrefix(r.Body, []byte("%PDF-")) {
			return base64.StdEncoding.EncodeToString(r.Body), nil
		}
		if trimmed[0] != '"' && trimmed[0] != '{' {
			encoded := string(trimmed)
			if _, err := base64.StdEncoding.DecodeString(encoded); err == nil {
				return encoded, nil
			}
			return nil, problem("API-002", "Cuéntica returned an invalid Base64 response.", "Check the service status or retry the download.", 1)
		}
	}
	var value any
	decoder := json.NewDecoder(bytes.NewReader(r.Body))
	decoder.UseNumber()
	if err := decoder.Decode(&value); err == nil {
		var extra any
		if decoder.Decode(&extra) == io.EOF {
			return value, nil
		}
	}
	if !binary && strings.HasPrefix(r.Header.Get("Content-Type"), "text/plain") {
		return string(r.Body), nil
	}
	return nil, problem("API-002", "Cuéntica returned an invalid JSON response.", "Check the service status. For downloads, use --save <file>.", 1)
}

func (a *App) allPages(ctx context.Context, c *api.Client, op catalog.Operation, path string, q url.Values, o *options, byteLimit int64) ([]any, error) {
	size := 300
	if q.Has("page_size") {
		size, _ = strconv.Atoi(q.Get("page_size"))
	} else {
		q.Set("page_size", strconv.Itoa(size))
	}
	items := []any{}
	var responseBytes int64
	for page := 1; page <= 10000; page++ {
		q.Set("page", strconv.Itoa(page))
		r, err := a.request(ctx, c, op.Method, path, q, nil, o)
		if err != nil {
			return nil, err
		}
		responseBytes += int64(len(r.Body))
		if responseBytes > byteLimit {
			return nil, problem("API-003", fmt.Sprintf("The combined page responses exceed the %d-byte export limit.", byteLimit), "Use date filters to narrow the query, or fetch individual pages without --all.", 1)
		}
		value, err := responseValue(r, false)
		if err != nil {
			return nil, err
		}
		batch, ok := value.([]any)
		if !ok {
			return nil, problem("API-003", "Pagination expected a JSON array.", "Omit --all to inspect the server response.", 1)
		}
		items = append(items, batch...)
		if current := r.Header.Get("X-Pagination-CurrentPage"); current != "" && current != strconv.Itoa(page) {
			return nil, problem("API-003", "The API returned an unexpected page.", "Fetch individual pages to inspect pagination.", 1)
		}
		pageSize := size
		if n, err := strconv.Atoi(r.Header.Get("X-Pagination-PageSize")); err == nil && n > 0 {
			pageSize = n
		}
		total, totalErr := strconv.Atoi(r.Header.Get("X-Pagination-TotalElements"))
		if totalErr == nil && total >= 0 {
			if page*pageSize >= total {
				return items, nil
			}
		}
		if len(batch) == 0 || (totalErr != nil && len(batch) < pageSize) {
			return items, nil
		}
	}
	return nil, problem("API-003", "Pagination exceeded 10,000 pages.", "Use date filters to narrow the query.", 1)
}

func checkDestination(path string) error {
	if _, err := os.Lstat(path); err == nil {
		return problem("IO-002", "The destination already exists.", "Choose a new --save path. Existing files are never overwritten.", 1)
	} else if !errors.Is(err, os.ErrNotExist) {
		return problem("IO-002", "Cannot access the destination.", "Check the --save path and permissions.", 1)
	}
	parent, err := os.Stat(filepath.Dir(path))
	if err != nil || !parent.IsDir() {
		return problem("IO-002", "The destination directory does not exist.", "Create the directory or choose another --save path.", 1)
	}
	return nil
}

func decodeDownload(body []byte) ([]byte, error) {
	if bytes.HasPrefix(body, []byte("%PDF-")) {
		return body, nil
	}
	encoded := strings.TrimSpace(string(body))
	var value any
	if strings.HasPrefix(encoded, `"`) || strings.HasPrefix(encoded, "{") {
		if err := json.Unmarshal(body, &value); err != nil {
			return nil, errors.New("invalid download JSON")
		}
		switch v := value.(type) {
		case string:
			encoded = v
		case map[string]any:
			data, ok := v["data"].(string)
			if !ok {
				if attachment, ok := v["attachment"].(map[string]any); ok {
					data, ok = attachment["data"].(string)
					if !ok {
						return nil, errors.New("missing attachment data")
					}
				} else {
					return nil, errors.New("missing data")
				}
			}
			encoded = data
		default:
			return nil, errors.New("unexpected download response")
		}
	}
	return base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
}

func (a *App) save(body []byte, path string, o *options) error {
	data, err := decodeDownload(body)
	if err != nil {
		return problem("API-004", "The response could not be decoded as Base64.", "Omit --save to inspect the response format.", 1)
	}
	if path == "-" {
		_, err = a.Out.Write(data)
		return err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
	if err != nil {
		return problem("IO-002", "Could not create the download file.", "Choose a new --save path and check its permissions.", 1)
	}
	_, writeErr := f.Write(data)
	closeErr := f.Close()
	if writeErr != nil || closeErr != nil {
		_ = os.Remove(path)
		return problem("IO-002", "Could not finish saving the download.", "Check disk space and permissions, then retry.", 1)
	}
	if !o.quiet {
		fmt.Fprintf(a.Err, "Saved %d bytes to %s\n", len(data), path)
	}
	return nil
}
