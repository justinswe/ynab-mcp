# ynab-mcp

Serves [YNAB](https://www.ynab.com) budgeting reads and writes as MCP tools
over a stateless streamable HTTP transport.

## Run

```sh
bazel run //cmd/ynab-mcp                            # passthrough: callers bring their own YNAB token
YNAB_ACCESS_TOKEN=... bazel run //cmd/ynab-mcp      # fixed: serve one operator token
```

The MCP endpoint is `http://localhost:8080/mcp`; `/healthz` reports liveness.

**Passthrough mode** (no `--ynab-access-token`) is built for public exposure:
each caller authenticates with their own YNAB personal access token —
`claude mcp add --transport http ynab https://<host>/mcp --header "Authorization: Bearer <your YNAB token>"` —
the server holds no secrets, and every caller spends their own YNAB quota.

| Flag | Env | Default | Purpose |
| --- | --- | --- | --- |
| `--ynab-access-token` | `YNAB_ACCESS_TOKEN` | empty (passthrough) | Fixed-mode YNAB token |
| `--ynab-base-url` | `YNAB_BASE_URL` | production | Override the YNAB API endpoint |
| `--budget-id` | `BUDGET_ID` | all budgets | Restrict every tool to one budget |
| `--allow-write` | `ALLOW_WRITE` | off | Expose create_transaction and update_transaction |
| `--port` | `PORT` | 8080 | HTTP listen port |
| `--mcp-auth-token` | `MCP_AUTH_TOKEN` | off | Fixed mode only: require this bearer token on MCP requests |

## Security posture

- Bearer scheme parsed case-insensitively; scheme-less headers rejected; 401s carry `WWW-Authenticate: Bearer`.
- Request bodies capped at 1 MiB; read/write/idle timeouts on the server; 30s timeout on every YNAB call.
- Stateless transport: no sessions, no shared mutable state between callers; passthrough builds a fresh client per request.
- Tokens are never logged; MCP responses are marked `no-cache` and, being authorized POST responses, are not reusable by intermediaries.
- Deliberately omitted: server-side rate limiting (YNAB's 200 req/hr per token self-limits each caller; cap cost with platform max-instances) and CORS headers (browsers cannot read the responses, and MCP clients are not browsers).

## Develop

```sh
bazel test //...
bazel run //:gazelle            # after adding or moving Go files
bazel run @rules_go//go -- mod tidy && bazel mod tidy   # after changing dependencies
```
