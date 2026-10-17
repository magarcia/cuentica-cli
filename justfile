set shell := ["bash", "-eu", "-o", "pipefail", "-c"]

# List development commands.
default:
    @just --list

# Build bin/cuentica with version, commit, and build date.
build:
    @./scripts/build build

# Run the complete test suite without cached results.
test:
    go test -count=1 ./...

# Test release packaging and artifact verification.
test-build:
    python3 -m unittest discover -s scripts -p 'test_*.py'

# Check for data races.
test-race:
    go test -race -count=1 ./...

# Write a coverage profile.
coverage:
    go test -count=1 -coverprofile=coverage.out ./...

# Run Go's static checks.
vet:
    go vet ./...

# Format Go source files.
fmt:
    gofmt -w .

# Check formatting without changing files.
fmt-check:
    @unformatted="$(gofmt -l .)"; if [[ -n "$unformatted" ]]; then printf 'Run just fmt to format:\n%s\n' "$unformatted" >&2; exit 1; fi

# Run the checks required before submitting a change.
check: fmt-check vet test test-build build

# Install to GOBIN or the first GOPATH's bin directory.
install:
    @./scripts/build install

# Build six platform archives and SHA-256 checksums in dist/.
release-snapshot:
    @VERSION=snapshot ./scripts/build release

# Build release archives using VERSION, COMMIT, and BUILD_DATE overrides.
release:
    @./scripts/build release

# Check release archives and smoke-test the native binary without credentials.
verify-release:
    @./scripts/build verify --run

# Remove generated binaries, archives, and coverage output.
clean:
    @./scripts/build clean
