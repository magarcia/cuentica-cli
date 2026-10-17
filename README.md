# cuentica

An unofficial CLI for the [Cuéntica API](https://apidocs.cuentica.com/versions/latest_release/). Manage invoices, income, expenses, contacts, documents, accounts, tags, and transfers from your terminal.

```sh
cuentica auth login
cuentica invoice list --all
cuentica invoice pdf --id 123 --save invoice.pdf
```

- All 54 documented API operations, with command help for each one.
- API keys from `CUENTICA_API_KEY` or your OS keyring.
- Text for terminals, JSON for scripts, and Markdown JSON blocks for LLMs.
- Filtering, pagination, attachment uploads, and PDF downloads.
- Request previews with `--dry-run`, plus confirmation for destructive actions.
- Single binaries for macOS, Linux, and Windows on Intel/AMD and ARM.

This project isn't affiliated with or supported by Cuéntica.

## Install

### Download a binary

Download an archive and `checksums.txt` from [GitHub Releases](https://github.com/magarcia/cuentica-cli/releases). The binary needs no Go, Python, or `just` installation.

| System | Architecture | Archive suffix |
| --- | --- | --- |
| macOS | Apple Silicon | `darwin_arm64.tar.gz` |
| macOS | Intel | `darwin_amd64.tar.gz` |
| Linux | x86-64 | `linux_amd64.tar.gz` |
| Linux | ARM64 | `linux_arm64.tar.gz` |
| Windows | x86-64 | `windows_amd64.zip` |
| Windows | ARM64 | `windows_arm64.zip` |

Archives are named `cuentica_<version>_<suffix>`, for example `cuentica_v0.1.0_darwin_arm64.tar.gz`. Replace the example version with the release you downloaded.

Before extracting, calculate the archive's SHA-256 digest and compare it with its line in `checksums.txt`:

```sh
# macOS
shasum -a 256 cuentica_v0.1.0_darwin_arm64.tar.gz

# Linux
sha256sum cuentica_v0.1.0_linux_amd64.tar.gz
```

```powershell
# Windows
Get-FileHash .\cuentica_v0.1.0_windows_amd64.zip -Algorithm SHA256
```

The digests must match exactly, ignoring letter case. Extract the archive, place `cuentica` (or `cuentica.exe`) in a directory on your `PATH`, and run `cuentica --version`. Archives also include the README, MIT license, and dependency notices. Binaries aren't code signed or notarized.

For a published public release, you can also verify the archive's GitHub build provenance with the [GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify cuentica_v0.1.0_darwin_arm64.tar.gz --repo magarcia/cuentica-cli
```

### Build from source

Requires Go 1.25 or newer, Python 3.11 or newer, [just](https://github.com/casey/just), and Bash. On Windows, use Git Bash or WSL for the `just` recipes.

Clone, build, and install:

```sh
git clone https://github.com/magarcia/cuentica-cli.git
cd cuentica-cli
just build
./bin/cuentica --version
just install
```

`just install` writes `cuentica` to `GOBIN`, or the first `GOPATH` entry's `bin` directory. Add that directory to your `PATH`. Builds include the version, commit, and build date.

## Authenticate

Generate an API token in Cuéntica for the company you want to access. Tokens belong to one user and company. Sandbox companies use separate tokens.

For local use, save the key in your OS keyring through a hidden prompt:

```sh
cuentica auth login
cuentica auth status
```

Login verifies the key before saving it. Keyring storage uses macOS Keychain, Windows Credential Manager, or Linux Secret Service. Linux needs a running D-Bus session and an unlocked Secret Service provider, such as GNOME Keyring.

For CI, containers, and headless machines, inject `CUENTICA_API_KEY` through your environment or secret manager, then run:

```sh
cuentica company get --no-input --output json
```

A nonempty `CUENTICA_API_KEY` always takes precedence over the saved key. An invalid environment key fails instead of falling back to the keyring. The CLI never accepts a key as a command-line value or stores it in a plaintext config file.

`cuentica auth logout` removes the saved key. Unset `CUENTICA_API_KEY` separately if you want to stop using an environment key. See [authentication details](docs/reference.md#authentication) for stdin and file-based login.

## Use it

Commands follow `cuentica <resource> <action> [flags]`. Identifiers use named flags:

```sh
cuentica company get
cuentica customer list --q studio --page-size 100
cuentica invoice list --initial-date 2026-01-01 --end-date 2026-12-31 --all
cuentica invoice get --id 123 --output json
cuentica invoice create --data @invoice.json --dry-run
```

IDs in examples are fictional. Replace them with IDs from your company. `--dry-run` validates local input and prints the request without reading credentials or contacting the API.

Terminal output defaults to compact text. Piped output defaults to JSON. Results go to stdout; errors and progress go to stderr.

For example, a request preview needs no API key:

```sh
cuentica document update --id 123 --field date=2026-10-17 --dry-run --output json
```

```json
{
  "body": {"date": "2026-10-17"},
  "method": "PUT",
  "path": "/document/123",
  "query": {},
  "requires_confirmation": false
}
```

Deletes and operations that replace financial data require confirmation. Interactive commands prompt; scripts must pass `--yes`. Review a request with `--dry-run` first. The CLI doesn't retry failed requests; check whether a write succeeded before repeating it after a timeout.

Find commands, flags, and body fields through built-in help:

```sh
cuentica --help
cuentica invoice create --help
cuentica help environment
cuentica help exit-codes
cuentica api list --output json
```

The [command reference](docs/reference.md) covers all 54 endpoints, JSON bodies, uploads, downloads, output formats, completion, and exit codes. Use [Cuéntica's API documentation](https://apidocs.cuentica.com/versions/latest_release/) for domain rules and conditional fields.

## Agent skill

The [cuentica skill](skills/cuentica/SKILL.md) teaches coding agents to discover commands through CLI help and use `--output llm`. Install the CLI separately and add `cuentica` to your `PATH`.

Install the skill for Codex across all your projects with either tool.

With [GitHub CLI's `gh skill` command](https://cli.github.com/manual/gh_skill_install), currently in preview:

```sh
gh skill install magarcia/cuentica-cli cuentica --agent codex --scope user
```

Or with [Vercel Skills](https://github.com/vercel-labs/skills), which requires Node.js 22.20 or newer, npm, and Git:

```sh
npx skills add magarcia/cuentica-cli --skill cuentica --agent codex --global
```

Replace `codex` with your agent, such as `claude-code` or `cursor`. For one project, run from its root and use `--scope project` with `gh`, or omit `--global` with Vercel Skills.

## Contribute

See [CONTRIBUTING.md](CONTRIBUTING.md) for setup, tests, and API changes. CI runs checks on Linux, macOS, and Windows. Tagged releases build all six platform archives with SHA-256 checksums; the [release guide](docs/releasing.md) explains the maintainer workflow.

Report bugs and suggest changes through [GitHub Issues](https://github.com/magarcia/cuentica-cli/issues). Keep API keys and customer data out of reports. Follow [SECURITY.md](SECURITY.md) to report a security issue.

## License

[MIT](LICENSE).
