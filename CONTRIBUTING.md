# Contributing

Keep changes focused. For new commands or behavior changes, describe the user problem and include an example invocation and expected output.

## Set up

Install Go 1.26 or newer, Python 3.11 or newer, [just](https://github.com/casey/just), and Bash. Windows contributors can run the `just` recipes in Git Bash or WSL.

```sh
just build
./bin/cuentica --help
just check
```

`just` lists all recipes. `just build` writes `bin/cuentica` with version, commit, and build date metadata. `just install` installs into `GOBIN`, or the first `GOPATH` entry's `bin` directory.

## Check a change

Run the focused tests while working, then run these before opening a pull request:

```sh
just check
just test-race
```

`just check` checks formatting, runs `go vet` and all tests, checks the build tooling, and builds the binary. Use `just fmt` to format Go files and `just coverage` to write `coverage.out`.

Tests don't need API credentials or a live Cuéntica account. HTTP tests serialize requests through an in-memory connection using `net.Pipe`; authentication tests use a fake credential store. Keep that boundary when adding coverage. Include full CLI invocations when behavior depends on arguments, stdout, stderr, or exit codes.

CI checks the minimum Go version and the current stable Go version on Linux, macOS, and Windows. The race detector runs on Linux. Cross-compilation checks all release targets; it doesn't prove those binaries run on every target architecture.

For packaging changes, also run:

```sh
just clean
just release-snapshot
VERSION=snapshot just verify-release
```

This writes and verifies six platform archives and `checksums.txt` in `dist/`, then smoke-tests the archive matching your host. `just clean` removes generated binaries, archives, and coverage output. Releases include the README, MIT license, and dependency notices. Update [THIRD_PARTY_NOTICES](THIRD_PARTY_NOTICES) when changing dependencies.

## Understand the code

| Path | Responsibility |
| --- | --- |
| `main.go` | Process setup, signals, and build metadata |
| `internal/cli/` | Command parsing, help, request construction, confirmation, and downloads |
| `internal/catalog/endpoints.json` | Embedded API operations, fields, and query parameters |
| `internal/api/` | HTTP requests, limits, and API errors |
| `internal/auth/` | Environment and OS keyring authentication |
| `internal/output/` | Text, JSON, and LLM output |
| `internal/testutil/` | HTTP transport for tests |
| `scripts/build` | Build metadata, installation, cross-compilation, and archive verification |

## Change API coverage

Check the [official API reference](https://apidocs.cuentica.com/versions/latest_release/) before changing an endpoint or payload. Update the catalog's source date when rechecking it.

For an operation change, update these together:

- `internal/catalog/endpoints.json`, including method, path, query parameters, body fields, pagination, downloads, and confirmation requirements.
- The independent endpoint matrix in `internal/cli/app_test.go`, plus focused request and validation tests.
- Command help expectations and [docs/reference.md](docs/reference.md). Keep the operation count in the README accurate.

Cuéntica enforces conditional fields and business rules. Keep local validation focused on safe request construction; don't turn an ambiguous documentation table into an unconditional restriction. When an operation replaces a list of charges, payments, or lines, explain that behavior and keep its confirmation requirement.

## Handle credentials and examples

Use fictional names, IDs, and payloads in fixtures and reports. Never commit keys, invoices, real customer data, `.env` files, or downloaded attachments. The CLI must keep secrets out of flags, logs, and error messages.

Live tests are optional and separate from the test suite. Use a sandbox company, explicitly replace any ambient production `CUENTICA_API_KEY`, and remove only records created by your test. Don't change a contributor's keyring as part of automated tests.

Open [issues](https://github.com/magarcia/cuentica-cli/issues) for bugs and feature requests. Include the CLI version, OS, architecture, sanitized command, expected result, actual result, and exit code. Follow [SECURITY.md](SECURITY.md) for security reports.

Maintainers can find repository setup and tag-based releases in [docs/releasing.md](docs/releasing.md).
