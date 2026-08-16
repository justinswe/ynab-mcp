<h1 align="center">ynab-mcp</h1>

<p align="center">
  Your YNAB budget as MCP tools — ask Claude where the money went.
</p>

<p align="center">
  <img alt="Publish passing" src="https://img.shields.io/badge/publish-passing-brightgreen">
  <a href="https://hub.docker.com/r/justinswe/ynab-mcp"><img alt="Docker pulls" src="https://img.shields.io/docker/pulls/justinswe/ynab-mcp"></a>
  <a href="https://hub.docker.com/r/justinswe/ynab-mcp/tags"><img alt="Docker image version" src="https://img.shields.io/docker/v/justinswe/ynab-mcp?sort=semver"></a>
  <a href="https://github.com/justinswe/ynab-mcp/blob/main/LICENSE"><img alt="MIT license" src="https://img.shields.io/github/license/justinswe/ynab-mcp"></a>
</p>

<p align="center">
  <a href="#quick-start">Quick start</a> ·
  <a href="#tools">Tools</a> ·
  <a href="#self-host">Self-host</a> ·
  <a href="#use-it-from-go">Go library</a> ·
  <a href="https://hub.docker.com/r/justinswe/ynab-mcp">Docker Hub</a>
</p>

[YNAB](https://www.ynab.com) knows where your money went. This serves that
knowledge to an AI agent as [MCP](https://modelcontextprotocol.io) tools, so you
can ask questions in plain language instead of clicking through reports —
"what did I spend on groceries last month", "which categories are overspent",
"how much is left in travel". Reads work out of the box. Writing transactions
is off unless the operator turns it on.

## Quick start

**1. Get a YNAB token.** Create a personal access token at
[app.ynab.com/settings/developer](https://app.ynab.com/settings/developer).

**2. Export it** in the shell you launch Claude Code from:

```sh
export YNAB_ACCESS_TOKEN=...
```

**3. Install the plugin:**

```
/plugin marketplace add justinswe/ynab-mcp
/plugin install ynab@ynab-mcp
```

That registers the MCP server and a skill that teaches Claude how to read a
YNAB budget — milliunit amounts, which tool answers which question, when not
to burn a request against the hourly quota.

Then just ask:

> What did I spend on groceries last month, and how does it compare to July?

The plugin points at a public endpoint that runs in [passthrough
mode](#security): your token is the credential, the server stores nothing. To
run the server yourself instead, see [Self-host](#self-host).

## Tools

Eight tools, six of them read-only. Every `budget_id` is optional and defaults
to your last-used budget.

| Tool | What it does | Key parameters |
| --- | --- | --- |
| `list_budgets` | Budgets the token can reach, with IDs and month ranges | — |
| `list_accounts` | Accounts with types and current balances | `include_closed?` |
| `list_categories` | Category groups with budgeted, activity, and available amounts | `include_hidden?` |
| `list_payees` | Payees with their IDs | `budget_id?` |
| `list_transactions` | Transactions newest-first, narrowed by account, category, payee, date window, or status | `account_id?`, `since_date?`, `until_date?`, `type?`, `limit?` |
| `get_month` | One month whole: income, budgeted, activity, ready-to-assign, and every category's numbers | `month?` |
| `create_transaction` | Record a transaction | `account_id`, `date`, `amount_milliunits`, `payee_name?` |
| `update_transaction` | Change fields on an existing transaction; omitted fields keep their values | `transaction_id`, plus any field to change |

Two rules decide which tools an agent actually sees:

- `list_budgets` is **not registered** when the server is scoped with
  `--budget-id`. A scoped server has nothing to discover, so the tool would
  only cost context.
- `create_transaction` and `update_transaction` are **withheld unless
  `--allow-write` is set**. Writes change financial records, so an operator has
  to ask for them.

Amounts are milliunits — 1/1000 of a currency unit, negative for outflows. A
$12.34 purchase is `-12340`. `list_transactions` returns at most 50 rows by
default (500 max) and, when called with no filters, covers the last 90 days.

## Self-host

The image is multi-arch (`linux/amd64`, `linux/arm64`) and distroless.

```sh
docker run --rm -p 8080:8080 justinswe/ynab-mcp:latest
```

That serves passthrough mode on `http://localhost:8080/mcp`, where each caller
supplies their own YNAB token. Point the plugin at it:

```sh
export YNAB_MCP_URL=http://localhost:8080/mcp
export YNAB_ACCESS_TOKEN=...
```

Or register it directly, without the plugin:

```sh
claude mcp add --transport http ynab http://localhost:8080/mcp \
  --header "Authorization: Bearer $YNAB_ACCESS_TOKEN"
```

To serve a single operator token instead — one budget, no per-caller token —
pass it in and gate the endpoint with a shared secret:

```sh
docker run --rm -p 8080:8080 \
  -e YNAB_ACCESS_TOKEN=... \
  -e MCP_AUTH_TOKEN=... \
  -e ALLOW_WRITE=true \
  justinswe/ynab-mcp:latest
```

`/healthz` reports liveness. There is **no stdio transport** — every client
config uses `--transport http`.

## Configuration

Flags and environment variables are interchangeable; an explicitly set flag
wins. A `.env` file in the working directory is loaded if present.

| Flag | Env | Default | Purpose |
| --- | --- | --- | --- |
| `--ynab-access-token` | `YNAB_ACCESS_TOKEN` | empty (passthrough) | Serve one operator token instead of per-caller tokens |
| `--ynab-base-url` | `YNAB_BASE_URL` | production | Override the YNAB API endpoint |
| `--budget-id` | `BUDGET_ID` | all budgets | Restrict every tool to one budget |
| `--allow-write` | `ALLOW_WRITE` | off | Expose `create_transaction` and `update_transaction` |
| `--port` | `PORT` | `8080` | HTTP listen port |
| `--mcp-auth-token` | `MCP_AUTH_TOKEN` | off | Require this bearer token on MCP requests; fixed mode only |
| `--log-level` | `LOG_LEVEL` | `0` | Verbosity: `-1` debug, `0` info, `1` warn, `2` error |
| `--log-format` | `LOG_FORMAT` | `console` | `console` for a terminal, `json` for a log collector |

`--mcp-auth-token` without `--ynab-access-token` is rejected at startup: in
passthrough mode the caller's YNAB token is the credential, and there is only
one `Authorization` header to carry it.

## Use it from Go

The tools are ordinary methods on a `Client`, so the package is useful without
MCP at all:

```go
client, err := ynabmcp.New(ynabmcp.Options{
    AccessToken: os.Getenv("YNAB_ACCESS_TOKEN"),
    BudgetID:    "9c07274b-8e3f-4d1f-a2c9-example", // optional
})
if err != nil {
    log.Fatal(err)
}
accounts, err := client.ListAccounts(ctx, ynabmcp.ListAccountsInput{})
```

Or embed the tools in a larger MCP server you already run, choosing which ones
to expose:

```go
server := mcp.NewServer(&mcp.Implementation{Name: "my-agent", Version: "v1"}, nil)

// No names registers every tool the Options allow.
if err := client.RegisterTools(server, "list_transactions", "get_month"); err != nil {
    log.Fatal(err)
}
```

Errors are wrapped sentinels you can branch on: `ErrUnauthorized`,
`ErrNotFound`, `ErrRateLimited`.

## Security

- **Passthrough mode holds no secrets.** Each request carries its own YNAB
  token, a fresh client is built for it, and nothing survives the response. The
  transport is stateless: no sessions, no shared mutable state between callers.
- **Tokens are never logged**, and MCP responses are marked `no-cache` — being
  authorized POST responses, they are not reusable by intermediaries.
- Bearer scheme is parsed case-insensitively per RFC 7235; scheme-less headers
  are rejected; 401s carry `WWW-Authenticate: Bearer`.
- Request bodies are capped at 1 MiB, with read, write, and idle timeouts on
  the server and a 30s timeout on every YNAB call.
- **Deliberately omitted:** server-side rate limiting — YNAB's 200 requests/hour
  is per token, so each caller self-limits — and CORS headers, since browsers
  cannot read the responses and MCP clients are not browsers.

**On the public endpoint.** `ynab.ju2tin.dev` is a convenience, run in
passthrough mode: your token is forwarded to YNAB and never stored or logged,
but it does transit someone else's infrastructure. It carries no uptime or
warranty commitment of any kind. If that trade-off doesn't suit you — and for
anything you'd call sensitive, it shouldn't — [self-host](#self-host); the
plugin works identically against your own server via `YNAB_MCP_URL`.

## Development

Bazel builds everything; there is no `go build` path.

```sh
bazel test //...
bazel run //:gazelle                                    # after adding or moving Go files
bazel run @rules_go//go -- mod tidy && bazel mod tidy    # after changing dependencies
```

See [CONTRIBUTING.md](CONTRIBUTING.md) — note that every PR must bump the
version in `MODULE.bazel`, which presubmit enforces.

## License

[MIT](LICENSE).
