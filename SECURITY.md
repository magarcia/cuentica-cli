# Security

Report suspected vulnerabilities privately through [GitHub Security Advisories](https://github.com/magarcia/cuentica-cli/security/advisories/new). If private reporting isn't available, open an issue asking for a private contact channel without including the vulnerability details.

Include the CLI version, operating system, architecture, impact, and a minimal reproduction with dummy data. Never include API keys, invoices, tax identifiers, or other business data in a public report.

## Credentials and API access

`CUENTICA_API_KEY` takes precedence over the OS keyring. Login verifies a key before saving it. The CLI doesn't accept keys as command-line flag values or write them to a plaintext configuration file.

Requests use HTTPS against `api.cuentica.com`. Redirects aren't followed, and failed API requests aren't retried automatically. API error messages redact the active key.

`--dry-run` includes the request body. Treat its output as business data before sharing it.

## Testing

Automated tests don't need a Cuéntica account, API key, or access to your OS keyring. Live API testing belongs in a Cuéntica sandbox with its own token. Keep production credentials out of test jobs and issue attachments.
