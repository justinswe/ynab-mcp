# Contributing

Issues and pull requests are welcome.

## Bump the version — every PR

Presubmit **fails** unless `version` in `MODULE.bazel` is a canonical
`MAJOR.MINOR.PATCH` strictly newer than the one on `main`:

```starlark
module(
    name = "ynab-mcp",
    version = "0.2.0",  # bump this
)
```

That one value stamps the binary's `version` subcommand *and* tags the Docker
image, so bumping it is the release action. Merging to `main` publishes
`justinswe/ynab-mcp:<version>` and `:latest` automatically.

This is the single most common CI failure, and it only shows up after you
push. Bump it first.

## Build and test

Bazel builds everything. There is no `go build` path — `go.mod` exists for
tooling and dependency resolution only.

```sh
bazel test //...                                         # all tests
bazel run //cmd/ynab-mcp                                 # run the server
bazel run //:gazelle                                     # after adding or moving Go files
bazel run @rules_go//go -- mod tidy && bazel mod tidy     # after changing dependencies
```

Gazelle generates the `BUILD.bazel` deps; if a build fails on a missing
dependency after you add an import, run it before debugging anything else.

## Testing against YNAB

Tests use a fake API (`internal/ynab`) and need no credentials. To exercise
the real thing, get a personal access token from
[app.ynab.com/settings/developer](https://app.ynab.com/settings/developer) and
run the server in passthrough mode:

```sh
bazel run //cmd/ynab-mcp
curl -s localhost:8080/healthz
```

Use a token you're willing to spend quota on — YNAB allows 200 requests/hour
per token. `--ynab-base-url` points the client at a stub if you'd rather not
touch production.

## Changing the plugin

The Claude Code plugin lives in `plugin/`, catalogued by
`.claude-plugin/marketplace.json`. Components go at the plugin root — only
`plugin.json` belongs inside `.claude-plugin/`.

```sh
claude plugin validate ./plugin      # schema check
claude --plugin-dir ./plugin         # load it in a real session
```

Keep `version` in `plugin/.claude-plugin/plugin.json` in step with
`MODULE.bazel`; users only receive plugin updates when it changes.

## Style

Go code follows the [Google Go Style Guide](https://google.github.io/styleguide/go/guide):
small functions with single-line doc comments, guard clauses over nested
conditionals, clarity ahead of cleverness. Comments explain *why*, not what —
match the density of the surrounding file.

New tools go in `mcp.go`'s `toolRegistrations()`, which is the single ordered
list of everything the server exposes, along with the reason a tool is
withheld.
