# Command reference

The CLI covers the 54 operations recorded in the [embedded API catalog](../internal/catalog/endpoints.json). Its source is the [Cuéntica API reference](https://apidocs.cuentica.com/versions/latest_release/), checked on October 17, 2026.

Start with the [README](../README.md) for installation and your first request.

- [Authentication](#authentication)
- [Commands and help](#commands-and-help)
- [Query parameters and pagination](#query-parameters-and-pagination)
- [JSON bodies](#json-bodies)
- [Uploads and downloads](#uploads-and-downloads)
- [Confirmation and dry runs](#confirmation-and-dry-runs)
- [Output and global flags](#output-and-global-flags)
- [Shell completion](#shell-completion)
- [Exit codes](#exit-codes)

## Authentication

Generate an API token in Cuéntica for the company you want to access. Tokens belong to a user and company; sandbox companies use separate tokens.

For local use, save the key in your OS keyring:

```sh
cuentica auth login
cuentica auth status
cuentica auth logout
```

Login hides your input and verifies the key with `GET /company` before saving it. Status verifies the active key without displaying it. The keyring entry uses service `cuentica-cli` and account `api-key`.

For automation, inject `CUENTICA_API_KEY` through your environment or secret manager. A nonempty environment variable takes precedence over the keyring. An invalid environment key fails immediately instead of falling back to a saved key.

You can also save a key from stdin or a file:

```sh
printf '%s' "$CUENTICA_API_KEY" | cuentica auth login --key-stdin
cuentica auth login --key-file ./api-key
```

Keys aren't accepted as command-line values or stored in a plaintext config file. Keyring access needs a working OS credential store: macOS Keychain, Windows Credential Manager, or Linux Secret Service. Use `CUENTICA_API_KEY` in headless environments without a keyring. Logout removes the saved key; an environment key remains active until you unset it.

Requests go to `https://api.cuentica.com` over HTTPS with the `X-AUTH-TOKEN` header.

## Commands and help

Commands follow `cuentica <resource> <action> [flags]`. Use named identifiers, such as `--id 123`.

```sh
cuentica
cuentica --help
cuentica invoice create --help
cuentica help environment
cuentica help exit-codes
cuentica api list --output json
```

Running without arguments shows common commands. Each operation's help includes its HTTP method, path, query flags, and documented body fields. The embedded [operation catalog](../internal/catalog/endpoints.json) records the API reference checked on October 17, 2026.

| Command | Method | API path |
| --- | --- | --- |
| `company get` | `GET` | `/company` |
| `company-series list` | `GET` | `/company/serie` |
| `account list` | `GET` | `/account` |
| `account get` | `GET` | `/account/:id` |
| `provider list` | `GET` | `/provider` |
| `provider create` | `POST` | `/provider` |
| `provider get` | `GET` | `/provider/:id` |
| `provider update` | `PUT` | `/provider/:id` |
| `provider delete` | `DELETE` | `/provider/:id` |
| `customer list` | `GET` | `/customer` |
| `customer create` | `POST` | `/customer` |
| `customer get` | `GET` | `/customer/:id` |
| `customer update` | `PUT` | `/customer/:id` |
| `customer delete` | `DELETE` | `/customer/:id` |
| `invoice list` | `GET` | `/invoice` |
| `invoice create` | `POST` | `/invoice` |
| `invoice get` | `GET` | `/invoice/:id` |
| `invoice update` | `PUT` | `/invoice/:id` |
| `invoice delete` | `DELETE` | `/invoice/:id` |
| `invoice void` | `POST` | `/invoice/:id/void` |
| `invoice public` | `GET` | `/invoice/:id/public` |
| `invoice-charge update` | `PUT` | `/invoice/:id/charges` |
| `invoice email` | `POST` | `/invoice/:id/email` |
| `invoice pdf` | `GET` | `/invoice/:id/pdf` |
| `income list` | `GET` | `/income` |
| `income create` | `POST` | `/income` |
| `income get` | `GET` | `/income/:id` |
| `income update` | `PUT` | `/income/:id` |
| `income delete` | `DELETE` | `/income/:id` |
| `income-attachment get` | `GET` | `/income/:id/attachment` |
| `income-attachment delete` | `DELETE` | `/income/:id/attachment` |
| `income-attachment update` | `PUT` | `/income/:id/attachment` |
| `income-charge update` | `PUT` | `/income/:id/charges` |
| `expense list` | `GET` | `/expense` |
| `expense create` | `POST` | `/expense` |
| `expense get` | `GET` | `/expense/:id` |
| `expense update` | `PUT` | `/expense/:id` |
| `expense delete` | `DELETE` | `/expense/:id` |
| `expense-attachment get` | `GET` | `/expense/:id/attachment` |
| `expense-attachment delete` | `DELETE` | `/expense/:id/attachment` |
| `expense-attachment update` | `PUT` | `/expense/:id/attachment` |
| `expense-payment update` | `PUT` | `/expense/:id/payments` |
| `document list` | `GET` | `/document` |
| `document create` | `POST` | `/document` |
| `document get` | `GET` | `/document/:id` |
| `document update` | `PUT` | `/document/:id` |
| `document delete` | `DELETE` | `/document/:id` |
| `document-attachment get` | `GET` | `/document/:id/attachment` |
| `tag list` | `GET` | `/tag` |
| `transfer list` | `GET` | `/transfer` |
| `transfer create` | `POST` | `/transfer` |
| `transfer get` | `GET` | `/transfer/:id` |
| `transfer update` | `PUT` | `/transfer/:id` |
| `transfer delete` | `DELETE` | `/transfer/:id` |

Every `:id` in this table maps to `--id`.

## Query parameters and pagination

API query names use snake_case; flags use hyphens. For example, `page_size` becomes `--page-size`, and `initial_date` becomes `--initial-date`.

```sh
cuentica customer list --q studio --page 1 --page-size 100
cuentica invoice list --initial-date 2026-01-01 --end-date 2026-12-31 --issued true --sort desc
cuentica expense list --provider 123 --all --output json
cuentica document list --query assigned=false --all
```

`--q` searches customers and providers. `-q` means `--quiet`.

Repeat `--query key=value` to send query parameters with their API spelling. Known parameters have dedicated flags; `--query` also allows additional API parameters.

Paginated lists support `--page` and `--page-size`. Page numbers start at 1; page sizes range from 1 to 300. `--all` starts at page 1 and returns one combined JSON array. It supports customer, provider, invoice, income, expense, document, and transfer lists, and can't be combined with an explicit page.

`--all` stops if combined response bodies exceed 64 MiB. Use date filters or fetch individual pages for larger exports.

## JSON bodies

Send a JSON object inline, from a file, or through stdin:

```sh
cuentica invoice create --data @invoice.json --dry-run
cuentica document update --id 123 --data '{"date":"2026-10-17"}' --dry-run
cuentica invoice-charge update --id 123 --data - --yes < charges.json
```

The file marker `@` belongs to the CLI argument. `--data -` reads piped stdin and won't prompt for JSON in a terminal.

Use repeatable `--field key=value` flags to add or override top-level fields:

```sh
cuentica invoice create --data @invoice.json --field issued=false --dry-run
cuentica customer create --data @customer.json --field 'postal_code="28001"' --dry-run
```

Values parse as JSON when valid; other values become strings. Quote a JSON string when a numeric-looking value must stay a string. Nested objects and arrays work as JSON values. Field names retain their API spelling, and `--field` overrides the same field in `--data`.

The CLI validates identifiers, input shape, dates, pagination, and supported flags before sending requests. Cuéntica validates domain rules and conditional fields. The reference contains conditional requirements and some inconsistent field tables, so the CLI doesn't reject bodies based on those tables alone. For example, document update prose describes `date` and `expense_id`, while its field table also lists `attachment`.

## Uploads and downloads

`--upload` reads a local file and builds its Base64 attachment:

```sh
cuentica document create --upload receipt.pdf --field date=2026-10-17 --dry-run
cuentica expense create --data @expense.json --upload receipt.pdf --dry-run
cuentica expense-attachment update --id 123 --upload receipt.pdf --yes
```

Document, income, and expense bodies receive an `attachment` object containing `filename` and `data`. Attachment update commands receive those fields at the top level. Uploads override attachment fields supplied through `--data` or `--field`. JSON input and upload files each have a 32 MiB limit.

Use `--save` to decode Base64 downloads:

```sh
cuentica invoice pdf --id 123 --save invoice.pdf
cuentica document-attachment get --id 123 --save receipt.pdf
cuentica income-attachment get --id 123 --save - > receipt.pdf
```

Saved files must have a new filename. The CLI requests mode `0600` and refuses to overwrite an existing file; Windows controls access through filesystem permissions. `--save -` writes binary stdout and refuses to write binary data directly to a terminal. Without `--save`, download commands print the API response in the selected output format.

## Confirmation and dry runs

Deletes, invoice voids, invoice emails, attachment replacements, charge/payment replacements, and invoice/income/expense updates need confirmation. Charge and payment updates replace the full list; entries omitted from the body are deleted. Invoice, income, and expense updates can also replace their existing lines.

```sh
cuentica expense-payment update --id 123 --data @payments.json --dry-run
cuentica expense-payment update --id 123 --data @payments.json --yes
```

`--dry-run` prints the method, path, query, body, and confirmation requirement without reading credentials or contacting the API. It validates local input, not server rules.

Interactive commands prompt before the operations listed above. Scripts must pass `--yes`. Use `--no-input` to prevent prompts explicitly. The CLI doesn't retry failed API requests; check whether a write succeeded before repeating it after a timeout or server error.

## Output and global flags

| Flag | Behavior |
| --- | --- |
| `--output`, `-o` | `text`, `json`, or `llm` |
| `--quiet`, `-q` | Suppress progress and status messages |
| `--verbose`, `-v` | Show HTTP method, path, and response status on stderr |
| `--timeout` | Timeout per HTTP request, default `30s` |
| `--no-input` | Never prompt |
| `--yes`, `-y` | Confirm destructive operations and email delivery |
| `--dry-run` | Preview an API request without sending it |
| `--no-color` | Plain output; output is already uncolored |
| `--help`, `-h` | Show help |
| `--version` | Show version, commit, and build date |

Terminal stdout defaults to compact text; piped stdout defaults to JSON. JSON output contains the API payload without a success envelope. Text tables can shorten or omit fields; JSON and LLM output retain the full nested payload. LLM output wraps JSON in a Markdown code block. `NO_COLOR` is respected through plain output.

Results go to stdout. Errors and progress go to stderr. `--quiet` still prints results and errors. Explicit `--output json` produces structured errors on stderr:

```json
{"error":{"code":"AUTH-001","message":"No API key is configured.","hint":"Set CUENTICA_API_KEY or run cuentica auth login."}}
```

## Shell completion

```sh
cuentica completion bash > cuentica.bash
cuentica completion zsh > _cuentica
cuentica completion fish > cuentica.fish
cuentica completion powershell > cuentica.ps1
```

Load the generated file through your shell's completion setup. Bash can load it directly with `source cuentica.bash`.

## Exit codes

| Code | Meaning |
| --- | --- |
| `0` | Success, help, or closed output pipe |
| `1` | API or file error, or cancelled action |
| `2` | Invalid command, flag, input, or missing confirmation |
| `3` | Missing or invalid API key, keyring failure, or HTTP 401/403 |
| `4` | HTTP 404 |
| `10` | Network failure or request timeout |
| `130` | SIGINT |
| `143` | SIGTERM |

Errors include a namespaced code and a suggested next step. HTTP 429 errors retain the server's `Retry-After` hint when present.
