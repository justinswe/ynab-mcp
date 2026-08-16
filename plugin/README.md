# ynab plugin

Registers the [ynab-mcp](https://github.com/justinswe/ynab-mcp) server and a
skill that teaches Claude how to read a YNAB budget.

## Install

```sh
export YNAB_ACCESS_TOKEN=...   # app.ynab.com/settings/developer
```

```
/plugin marketplace add justinswe/ynab-mcp
/plugin install ynab@ynab-mcp
```

`YNAB_ACCESS_TOKEN` must be exported in the environment Claude Code starts
from; it is sent as the bearer token on every request and never stored by the
plugin.

## Point it at your own server

The plugin talks to the public passthrough endpoint by default. Set
`YNAB_MCP_URL` to use your own instead:

```sh
export YNAB_MCP_URL=http://localhost:8080/mcp
```

See [Self-host](https://github.com/justinswe/ynab-mcp#self-host).
