# Installation

Install the CLI with Homebrew, download a release binary, or build from source. Then [configure authentication](reference.md#authentication).

On Linux, keyring authentication needs a running D-Bus session and an unlocked Secret Service provider, such as GNOME Keyring. Use `CUENTICA_API_KEY` on headless machines without a keyring.

## Homebrew

On macOS or Linux, install the release binary and shell completions:

```sh
brew install magarcia/tap/cuentica
cuentica --version
```

Homebrew supports x86-64 and ARM64. To upgrade:

```sh
brew update
brew upgrade magarcia/tap/cuentica
```

## Download a binary

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

The digests must match exactly, ignoring letter case. Extract the archive, place `cuentica` (or `cuentica.exe`) in a directory on your `PATH`, and run `cuentica --version`. Archives include the README, MIT license, and dependency notices. Binaries aren't code signed or notarized.

You can also verify the archive's GitHub build provenance with the [GitHub CLI](https://cli.github.com/):

```sh
gh attestation verify cuentica_v0.1.0_darwin_arm64.tar.gz --repo magarcia/cuentica-cli
```

## Build from source

Requires Go 1.25 or newer, Python 3.11 or newer, [just](https://github.com/casey/just), and Bash. On Windows, use Git Bash or WSL for the `just` recipes.

```sh
git clone https://github.com/magarcia/cuentica-cli.git
cd cuentica-cli
just build
./bin/cuentica --version
just install
```

`just build` writes `bin/cuentica` with the version, commit, and build date. `just install` writes `cuentica` to `GOBIN`, or the first `GOPATH` entry's `bin` directory. Add that directory to your `PATH`.

## Shell completion

Homebrew installs Bash, Zsh, and Fish completions. For manual installations, follow the [shell completion instructions](reference.md#shell-completion), which also cover PowerShell.

## Agent skill

The [cuentica skill](../skills/cuentica/SKILL.md) teaches coding agents to discover commands through CLI help and use `--output llm`. Install the CLI separately and add `cuentica` to your `PATH`.

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
