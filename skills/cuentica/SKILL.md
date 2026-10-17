---
name: cuentica
description: Use the cuentica CLI to work with the Cuéntica API. Discover commands, flags, request bodies, and authentication through built-in help.
---

Use the installed CLI's help as the source of truth. Discover the relevant command before running it:

```sh
cuentica --help
cuentica <resource> --help
cuentica <resource> <action> --help
```

`cuentica api list --output llm` provides the complete operation catalog. Read action help for identifiers, filters, body fields, uploads, and downloads.

- Use `--output llm --no-input` for agent calls. LLM output preserves the complete response as JSON inside a Markdown code block under `# Result`.
- Preview writes with `--dry-run`. Pass `--yes` only when the user has authorized the action.
- Use the configured `CUENTICA_API_KEY` or OS keyring. Read `cuentica help environment` for setup; keep keys out of arguments and output.
- On failure, read stderr and `cuentica help exit-codes`. Check whether a write succeeded before retrying it after a timeout.
