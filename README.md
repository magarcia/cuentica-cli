# cuentica

[![CI](https://github.com/magarcia/cuentica-cli/actions/workflows/ci.yml/badge.svg)](https://github.com/magarcia/cuentica-cli/actions/workflows/ci.yml)
[![Release](https://img.shields.io/github/v/release/magarcia/cuentica-cli)](https://github.com/magarcia/cuentica-cli/releases/latest)
[![MIT](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Cuéntica from your terminal. Manage invoices, income, expenses, contacts, and
documents with one binary, readable tables, and structured output for scripts
and agents.

Unofficial client for the [Cuéntica API](https://apidocs.cuentica.com/versions/latest_release/),
covering all 54 documented operations. This project isn't affiliated with or
supported by Cuéntica.

## Install

On macOS or Linux, use [Homebrew](https://brew.sh):

```sh
brew install magarcia/tap/cuentica
```

[Download binaries](https://github.com/magarcia/cuentica-cli/releases/latest)
for macOS, Linux, and Windows on amd64 and arm64. See
[installation](docs/installation.md) for checksums, completions, source builds,
and upgrades.

## Get started

```sh
cuentica auth login
cuentica company get
cuentica invoice list --page-size 20
```

Login prompts for an API key, verifies it, and saves it in your OS keyring.
For automation, set `CUENTICA_API_KEY`; it takes precedence over the saved key.
Tokens belong to one user and company. Sandbox companies use separate tokens.
See [authentication](docs/reference.md#authentication) for other login methods
and headless environments.

Filter invoices by date, then download a PDF. Replace the invoice ID with one
from your company:

```sh
cuentica invoice list --initial-date 2026-01-01 --end-date 2026-12-31 --all
cuentica invoice pdf --id 123 --save invoice.pdf
```

Preview a write with `--dry-run`. It validates local input without reading
credentials or calling Cuéntica:

```sh
cuentica document update --id 123 --field date=2026-10-17 --dry-run
```

Discover commands and flags with `cuentica --help` or
`cuentica invoice create --help`. See [confirmation and dry runs](docs/reference.md#confirmation-and-dry-runs)
before replacing or deleting financial data.

## Agents and scripts

Use `--no-input --output llm` for agents or `--output json` for scripts:

```sh
cuentica invoice list --no-input --output llm
```

Install the [agent skill](skills/cuentica/SKILL.md) to teach an agent how to learn
the CLI through its help:

```sh
gh skill install magarcia/cuentica-cli cuentica --agent codex --scope user
```

See [skill installation](docs/installation.md#agent-skill) for other agents and the
Vercel skills installer.

## Documentation

- [Installation](docs/installation.md): platforms, downloads, and shell completions.
- [Authentication](docs/reference.md#authentication): login, keyring storage, and environment keys.
- [Command reference](docs/reference.md): all 54 operations, pagination, uploads, output formats, and errors.
- [Contributing](CONTRIBUTING.md): build with Go and `just`, run tests, and contribute.
- [Releasing](docs/releasing.md): cross-platform builds, GitHub releases, and Homebrew updates.

[Report a bug](https://github.com/magarcia/cuentica-cli/issues) ·
[Security](SECURITY.md) · [MIT license](LICENSE)
