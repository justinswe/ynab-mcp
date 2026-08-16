---
name: ynab-budgeting
description: Read and update a YNAB budget — spending by category or payee, account balances, monthly reports, uncategorized or unapproved transactions, and recording new ones. Use whenever the user asks about their budget, spending, transactions, account balances, or YNAB.
---

# Working with YNAB

The `ynab` MCP server exposes one YNAB budget as tools. What follows is the
handling this API needs and the tool schemas do not spell out.

## Amounts are milliunits

Every amount is 1/1000 of a currency unit, negative for outflows. A $12.34
purchase is `-12340`; $1,500 of income is `1500000`.

Divide by 1000 and format as currency before showing anything to the user.
Never print a raw milliunit figure — "you spent -84230" is not an answer.
When writing, convert the other way and get the sign right: spending is
negative.

## Finding the budget

`budget_id` is optional on every tool and defaults to YNAB's `last-used`
budget. Omit it.

Call `list_budgets` only when the user names a specific budget or you have
evidence they keep more than one. A speculative `list_budgets` on every
question is a wasted call against a 200 requests/hour quota.

If the server was started with `--budget-id`, `list_budgets` is not
registered at all and every tool is already pinned to that budget.

## Finding IDs

Tools take IDs, users speak names. Resolve in this order:

- `list_accounts` → `account_id`
- `list_categories` → `category_id`
- `list_payees` → `payee_id`

When **creating** a transaction, prefer `payee_name` over `payee_id`: YNAB
matches it case-insensitively to an existing payee or creates one. Don't call
`list_payees` first just to write.

## Listing transactions

`list_transactions` has two behaviors worth knowing:

- **At most one** of `account_id`, `category_id`, and `payee_id` may be set.
  To answer "what did I spend at Costco on groceries", filter by one and
  narrow the rest yourself from the results.
- An unfiltered call defaults to the **last 90 days**. Setting any filter —
  including `until_date` or `type` — turns that default off, so a filtered
  call with no `since_date` searches all history and may be slow. Pass
  `since_date` when you mean a window.

Results are newest-first, 50 by default, 500 max. The response carries a
`note` field that says when results were truncated or when the 90-day default
applied — read it and act on it rather than assuming you saw everything.

## Monthly analysis

`get_month` is the right tool for "how did last month go" — it returns income,
budgeted, activity, ready-to-assign, and every category's numbers in one call.
Don't reconstruct a month by summing `list_transactions`.

`month` takes the first of the month (`2026-07-01`) and defaults to the
current month.

## Writes

`create_transaction` and `update_transaction` exist **only** when the server
runs with `--allow-write`. If they are not in your tool list, tell the user
writes are disabled on their server and stop — don't retry or work around it.

When they are available:

- `update_transaction` only changes the fields you pass; omitted fields keep
  their current values.
- Approve with `approve: true`; categorize by passing `category_id`.
- Confirm with the user before creating or changing anything. These are real
  financial records and there is no undo through this API.

## Common requests

| The user asks | Do this |
| --- | --- |
| "What did I spend on X last month?" | `list_categories` for the ID, then `list_transactions` with `category_id` + `since_date`/`until_date` |
| "How much is left in X?" | `list_categories` — `available` is already the answer |
| "How did last month go?" | `get_month` |
| "What's my balance?" | `list_accounts` |
| "What needs categorizing?" | `list_transactions` with `type: "uncategorized"` |
| "What needs approving?" | `list_transactions` with `type: "unapproved"` |
| "I spent $X at Y" | `create_transaction` with `payee_name`, negative milliunits |

## Errors

- **401 / token rejected** — the YNAB personal access token is wrong or
  revoked. Point the user at `app.ynab.com/settings/developer`; don't retry.
- **429 / rate limited** — YNAB allows 200 requests/hour per token and it is
  spent. Say so and stop; retrying makes it worse.
