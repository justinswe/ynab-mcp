// Package ynabmcp exposes YNAB budgeting reads and writes as ordinary Go
// methods on a Client, and as MCP tools for agents via RegisterTools.
package ynabmcp

import (
	"context"
	"net/http"
	"strings"

	"github.com/justinswe/std/errors"
	"github.com/justinswe/ynab-mcp/internal/ynab"
	"go.uber.org/zap"
)

// Sentinel errors a Go caller can branch on with errors.Is; the full error text carries agent guidance.
var (
	// ErrUnauthorized reports a YNAB 401: the access token was rejected.
	ErrUnauthorized = errors.New("the YNAB access token was rejected")
	// ErrNotFound reports a YNAB 404: the budget or resource does not exist.
	ErrNotFound = errors.New("not found")
	// ErrRateLimited reports a YNAB 429: the hourly request quota is spent.
	ErrRateLimited = errors.New("rate limited")
)

// lastUsedBudget is YNAB's server-side alias for the most recently used budget.
const lastUsedBudget = "last-used"

// api is the subset of the YNAB API the tools call, split out so tests can substitute a fake.
type api interface {
	GetUser(ctx context.Context) (ynab.User, error)
	ListBudgets(ctx context.Context) ([]ynab.Budget, error)
	ListAccounts(ctx context.Context, budgetID string) ([]ynab.Account, error)
	ListCategories(ctx context.Context, budgetID string) ([]ynab.CategoryGroup, error)
	ListPayees(ctx context.Context, budgetID string) ([]ynab.Payee, error)
	ListTransactions(ctx context.Context, budgetID string, query ynab.TransactionsQuery) ([]ynab.Transaction, error)
	GetMonth(ctx context.Context, budgetID, month string) (ynab.Month, error)
	CreateTransaction(ctx context.Context, budgetID string, tx ynab.SaveTransaction) (ynab.Transaction, error)
	UpdateTransaction(ctx context.Context, budgetID, transactionID string, tx ynab.SaveTransaction) (ynab.Transaction, error)
}

// Options configures a Client.
type Options struct {
	// AccessToken is the YNAB personal access token.
	AccessToken string
	// BudgetID restricts every call to one budget. When set, the budget_id
	// field on each input is ignored and list_budgets is not registered as a
	// tool.
	BudgetID string
	// AllowWrite exposes the create_transaction and update_transaction tools.
	// It gates MCP registration only; the Go methods are always available.
	AllowWrite bool
	// BaseURL overrides the YNAB API root; empty uses production.
	BaseURL string
}

// Client calls YNAB on behalf of a Go program or an MCP agent; safe for concurrent use.
type Client struct {
	api        api
	budgetID   string
	allowWrite bool
}

// New creates a Client from a YNAB personal access token.
func New(opts Options) (*Client, error) {
	token := strings.TrimSpace(opts.AccessToken)
	if token == "" {
		return nil, errors.New("AccessToken is required")
	}
	api := ynab.New(token)
	if opts.BaseURL != "" {
		api.BaseURL = strings.TrimSuffix(opts.BaseURL, "/")
	}
	return &Client{
		api:        api,
		budgetID:   strings.TrimSpace(opts.BudgetID),
		allowWrite: opts.AllowWrite,
	}, nil
}

// CheckToken validates the access token so a bad one fails startup instead of every tool call.
func (c *Client) CheckToken(ctx context.Context) error {
	user, err := c.api.GetUser(ctx)
	if err != nil {
		return fail("authenticate with YNAB", err)
	}
	zap.L().Info("Authenticated with YNAB", zap.String("user_id", user.ID))
	return nil
}

// resolveBudget picks the budget to act on.
func (c *Client) resolveBudget(budgetID string) string {
	// A configured budget wins over the argument: it is a restriction, not a
	// default, and list_budgets is not registered in that case.
	if c.budgetID != "" {
		return c.budgetID
	}
	if trimmed := strings.TrimSpace(budgetID); trimmed != "" {
		return trimmed
	}
	// YNAB's last-used alias serves single-budget users without a lookup.
	return lastUsedBudget
}

// fail converts a YNAB failure into guidance the caller or agent can act on.
func fail(operation string, err error) error {
	apiErr, ok := errors.AsType[*ynab.APIError](err)
	if !ok {
		return errors.Wrapf(err, "%s failed", operation)
	}
	// Both the sentinel and the API error are wrapped so errors.Is matches the
	// category while YNAB's detail text and errors.AsType survive for diagnosis.
	switch apiErr.StatusCode {
	case http.StatusUnauthorized:
		return errors.Errorf("%s failed: %w: %w. Do not retry; the access token is invalid.", operation, ErrUnauthorized, apiErr)
	case http.StatusNotFound:
		return errors.Errorf("%s failed: %w: %w. Check the budget and resource IDs with the list tools first.", operation, ErrNotFound, apiErr)
	case http.StatusTooManyRequests:
		return errors.Errorf("%s failed: %w: %w. YNAB allows 200 requests per hour per token; wait before retrying.", operation, ErrRateLimited, apiErr)
	}
	return errors.Wrapf(apiErr, "%s failed", operation)
}
