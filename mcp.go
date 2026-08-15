package ynabmcp

import (
	"context"
	"maps"
	"slices"
	"strings"

	"github.com/justinswe/std/errors"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// RegisterTools adds every YNAB tool the configuration allows, or exactly the named subset.
func (c *Client) RegisterTools(server *mcp.Server, names ...string) error {
	registrations := c.toolRegistrations()
	explicit := len(names) > 0
	selected := map[string]bool{}
	for _, name := range names {
		selected[name] = true
	}
	// The whole selection is validated before anything is registered, so a
	// failed call leaves the caller's server untouched rather than half-configured.
	unknown := maps.Clone(selected)
	for _, tool := range registrations {
		delete(unknown, tool.name)
		// An explicitly named tool the configuration withholds is an error, not
		// a silent omission.
		if explicit && selected[tool.name] && tool.withheld != "" {
			return errors.New(tool.withheld)
		}
	}
	if len(unknown) > 0 {
		return errors.Errorf("unknown tools: %s", strings.Join(slices.Sorted(maps.Keys(unknown)), ", "))
	}
	for _, tool := range registrations {
		if (explicit && !selected[tool.name]) || tool.withheld != "" {
			continue
		}
		tool.register(server)
	}
	return nil
}

// toolRegistration pairs a tool name with its registration, or the reason it is withheld.
type toolRegistration struct {
	name     string
	withheld string
	register func(*mcp.Server)
}

// toolRegistrations lists every tool in registration order.
func (c *Client) toolRegistrations() []toolRegistration {
	budgetsWithheld := ""
	if c.budgetID != "" {
		// A scoped client has nothing to discover, so the tool would only cost context.
		budgetsWithheld = "list_budgets is unavailable on a budget-scoped client"
	}
	writeWithheld := ""
	if !c.allowWrite {
		// Writes change financial records, so an operator has to ask for them.
		writeWithheld = "write tools are withheld unless AllowWrite is set"
	}
	return []toolRegistration{
		{name: "list_budgets", withheld: budgetsWithheld, register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "list_budgets",
				Description: "List the YNAB budgets this token can access, with their IDs and month ranges. " +
					"Use this first to find a budget_id — though every other tool defaults to the last-used budget when budget_id is omitted.",
				Annotations: readOnly("List YNAB budgets"),
			}, func(ctx context.Context, _ listBudgetsInput) (ListBudgetsOutput, error) {
				return c.ListBudgets(ctx)
			})
		}},
		{name: "list_accounts", register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "list_accounts",
				Description: "List a budget's accounts with their IDs, types, and current balances. " +
					"Use this to find an account_id before listing its transactions or creating one, or to report balances.",
				Annotations: readOnly("List budget accounts"),
			}, c.ListAccounts)
		}},
		{name: "list_categories", register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "list_categories",
				Description: "List a budget's category groups and categories with their current-month budgeted, activity, and available amounts. " +
					"Use this to find a category_id or to review how the current month's budget is allocated.",
				Annotations: readOnly("List budget categories"),
			}, c.ListCategories)
		}},
		{name: "list_payees", register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "list_payees",
				Description: "List a budget's payees with their IDs. " +
					"Use this to find a payee_id for filtering transactions; when creating a transaction, prefer payee_name, which matches or creates a payee automatically.",
				Annotations: readOnly("List budget payees"),
			}, c.ListPayees)
		}},
		{name: "list_transactions", register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "list_transactions",
				Description: "List a budget's transactions, newest first, optionally narrowed to one account, category, or payee, a start date, or unapproved/uncategorized status. " +
					"Use this to review spending, find a transaction to update, or reconcile an account.",
				Annotations: readOnly("List transactions"),
			}, c.ListTransactions)
		}},
		{name: "get_month", register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "get_month",
				Description: "Fetch one budget month: income, amount budgeted, activity, ready-to-assign, and every category's budgeted, activity, and available amounts. " +
					"Use this for month-over-month comparisons or to check how much a category has left.",
				Annotations: readOnly("Get a budget month"),
			}, c.GetMonth)
		}},
		{name: "create_transaction", withheld: writeWithheld, register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "create_transaction",
				Description: "Add a transaction to a budget. Amounts are in milliunits and negative for outflows: a $12.34 purchase is -12340. " +
					"Find the account_id with list_accounts first; pass payee_name to match or create the payee by name.",
				Annotations: writes("Create a transaction", false),
			}, c.CreateTransaction)
		}},
		{name: "update_transaction", withheld: writeWithheld, register: func(server *mcp.Server) {
			addTool(server, &mcp.Tool{
				Name: "update_transaction",
				Description: "Change fields on an existing transaction; omitted fields keep their current values. " +
					"Find the transaction_id with list_transactions first. Useful for categorizing, approving, or correcting a transaction.",
				Annotations: writes("Update a transaction", true),
			}, c.UpdateTransaction)
		}},
	}
}

// addTool registers a Client method as an MCP tool.
func addTool[In, Out any](server *mcp.Server, tool *mcp.Tool, handle func(context.Context, In) (Out, error)) {
	mcp.AddTool(server, tool, func(ctx context.Context, _ *mcp.CallToolRequest, in In) (*mcp.CallToolResult, Out, error) {
		// The error is returned to the SDK, which reports it inside the result
		// (IsError, no structured payload) so the agent corrects its next call.
		out, err := handle(ctx, in)
		return nil, out, err
	})
}

// readOnly annotates a tool that never modifies YNAB.
func readOnly(title string) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, ReadOnlyHint: true, OpenWorldHint: boolPtr(true)}
}

// writes annotates a tool that changes YNAB, marking whether it is destructive.
func writes(title string, destructive bool) *mcp.ToolAnnotations {
	return &mcp.ToolAnnotations{Title: title, DestructiveHint: &destructive, OpenWorldHint: boolPtr(true)}
}

func boolPtr(value bool) *bool { return &value }
