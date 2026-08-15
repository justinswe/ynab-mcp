package ynabmcp

import (
	"context"
	"time"

	"github.com/justinswe/std/errors"
	"github.com/justinswe/ynab-mcp/internal/ynab"
)

// Listing bounds that keep one call from flooding the agent's context or YNAB's hourly quota.
const (
	defaultTransactionLimit = 50
	maxTransactionLimit     = 500
	defaultSinceDays        = 90
)

// ListTransactionsInput narrows a transaction listing.
type ListTransactionsInput struct {
	BudgetID   string `json:"budget_id,omitempty" jsonschema:"YNAB budget ID. Ignored when the client is scoped to one budget; defaults to the last-used budget."`
	AccountID  string `json:"account_id,omitempty" jsonschema:"Only transactions in this account. Find IDs with list_accounts. At most one of account_id, category_id, and payee_id may be set."`
	CategoryID string `json:"category_id,omitempty" jsonschema:"Only transactions in this category. Find IDs with list_categories."`
	PayeeID    string `json:"payee_id,omitempty" jsonschema:"Only transactions with this payee. Find IDs with list_payees."`
	SinceDate  string `json:"since_date,omitempty" jsonschema:"Inclusive start of the date window as an ISO date, e.g. 2026-08-01. Unfiltered calls default to the last 90 days."`
	UntilDate  string `json:"until_date,omitempty" jsonschema:"Inclusive end of the date window as an ISO date. Combine with since_date to select a specific period."`
	Type       string `json:"type,omitempty" jsonschema:"Only 'uncategorized' or 'unapproved' transactions."`
	Limit      int    `json:"limit,omitempty" jsonschema:"Maximum transactions to return, newest first. Defaults to 50, capped at 500."`
}

// Transaction is one budget transaction with its amount in milliunits.
type Transaction struct {
	ID        string `json:"id"`
	Date      string `json:"date"`
	Amount    int64  `json:"amount_milliunits"`
	Memo      string `json:"memo,omitempty"`
	Cleared   string `json:"cleared"`
	Approved  bool   `json:"approved"`
	FlagColor string `json:"flag_color,omitempty"`
	Account   string `json:"account,omitempty"`
	Payee     string `json:"payee,omitempty"`
	Category  string `json:"category,omitempty"`
}

// ListTransactionsOutput reports transactions newest first.
type ListTransactionsOutput struct {
	BudgetID     string        `json:"budget_id"`
	Transactions []Transaction `json:"transactions"`
	Note         string        `json:"note,omitempty"`
}

// ListTransactions reports a budget's transactions, newest first.
func (c *Client) ListTransactions(ctx context.Context, input ListTransactionsInput) (ListTransactionsOutput, error) {
	since, until, err := transactionWindow(input)
	if err != nil {
		return ListTransactionsOutput{}, err
	}
	budgetID := c.resolveBudget(input.BudgetID)
	fetched, err := c.api.ListTransactions(ctx, budgetID, ynab.TransactionsQuery{
		AccountID:  input.AccountID,
		CategoryID: input.CategoryID,
		PayeeID:    input.PayeeID,
		SinceDate:  since,
		Type:       input.Type,
	})
	if err != nil {
		return ListTransactionsOutput{}, fail("list_transactions", err)
	}
	limit := clampLimit(input.Limit)
	output := ListTransactionsOutput{BudgetID: budgetID, Transactions: []Transaction{}}
	truncated := false
	// YNAB reports oldest first; walking backwards yields newest first without
	// mutating the slice the api implementation still owns.
	for i := len(fetched) - 1; i >= 0; i-- {
		tx := fetched[i]
		// ISO dates compare correctly as strings, so the inclusive until bound
		// is one comparison per row.
		if tx.Deleted || (until != "" && tx.Date > until) {
			continue
		}
		if len(output.Transactions) == limit {
			truncated = true
			break
		}
		output.Transactions = append(output.Transactions, mapTransaction(tx))
	}
	output.Note = transactionsNote(len(output.Transactions), truncated, since != "" && input.SinceDate == "")
	return output, nil
}

// transactionWindow validates the date window and applies the default lookback.
func transactionWindow(input ListTransactionsInput) (since, until string, err error) {
	if since, err = parseDate("since_date", input.SinceDate); err != nil {
		return "", "", err
	}
	if until, err = parseDate("until_date", input.UntilDate); err != nil {
		return "", "", err
	}
	if since != "" && until != "" && until < since {
		return "", "", errors.Errorf("until_date %s is before since_date %s", until, since)
	}
	// A completely unfiltered call would download the budget's full history to
	// keep at most one page, so it defaults to a recent window instead.
	if since == "" && !narrowed(input) {
		since = time.Now().AddDate(0, 0, -defaultSinceDays).Format(time.DateOnly)
	}
	return since, until, nil
}

// parseDate validates one ISO date input, allowing empty.
func parseDate(field, value string) (string, error) {
	if value == "" {
		return "", nil
	}
	if _, err := time.Parse(time.DateOnly, value); err != nil {
		return "", errors.Errorf("invalid %s %q: use an ISO date like 2026-08-14", field, value)
	}
	return value, nil
}

// narrowed reports whether any input filter bounds the listing.
func narrowed(input ListTransactionsInput) bool {
	return input.AccountID != "" || input.CategoryID != "" || input.PayeeID != "" ||
		input.Type != "" || input.UntilDate != ""
}

// clampLimit bounds the requested page size to [1, maxTransactionLimit].
func clampLimit(limit int) int {
	if limit <= 0 {
		return defaultTransactionLimit
	}
	return min(limit, maxTransactionLimit)
}

// transactionsNote explains an empty, truncated, or defaulted listing.
func transactionsNote(count int, truncated, defaulted bool) string {
	if count == 0 {
		if defaulted {
			return "No transactions in the last 90 days. Pass since_date to look further back."
		}
		return "No matching transactions. Loosen the filters or check the IDs with the list tools."
	}
	note := "Amounts are milliunits: 1/1000 of a currency unit, negative for outflows."
	if truncated {
		note += " Older transactions truncated; raise limit or narrow the window with since_date/until_date."
	}
	if defaulted {
		note += " Showing the last 90 days by default; pass since_date to look further back."
	}
	return note
}

// mapTransaction shapes an API transaction for the agent.
func mapTransaction(tx ynab.Transaction) Transaction {
	return Transaction{
		ID:        tx.ID,
		Date:      tx.Date,
		Amount:    tx.Amount,
		Memo:      tx.Memo,
		Cleared:   tx.Cleared,
		Approved:  tx.Approved,
		FlagColor: tx.FlagColor,
		Account:   tx.AccountName,
		Payee:     tx.PayeeName,
		Category:  tx.CategoryName,
	}
}

// GetMonthInput selects the budget month to fetch.
type GetMonthInput struct {
	BudgetID      string `json:"budget_id,omitempty" jsonschema:"YNAB budget ID. Ignored when the client is scoped to one budget; defaults to the last-used budget."`
	Month         string `json:"month,omitempty" jsonschema:"Budget month as an ISO date on the first, e.g. 2026-08-01. Defaults to the current month."`
	IncludeHidden bool   `json:"include_hidden,omitempty" jsonschema:"Include hidden categories, which are omitted by default."`
}

// GetMonthOutput reports one budget month with milliunit amounts.
type GetMonthOutput struct {
	BudgetID     string     `json:"budget_id"`
	Month        string     `json:"month"`
	Income       int64      `json:"income_milliunits"`
	Budgeted     int64      `json:"budgeted_milliunits"`
	Activity     int64      `json:"activity_milliunits"`
	ToBeBudgeted int64      `json:"ready_to_assign_milliunits"`
	AgeOfMoney   int64      `json:"age_of_money_days,omitempty"`
	Categories   []Category `json:"categories"`
	Note         string     `json:"note,omitempty"`
}

// GetMonth reports one budget month.
func (c *Client) GetMonth(ctx context.Context, input GetMonthInput) (GetMonthOutput, error) {
	budgetID := c.resolveBudget(input.BudgetID)
	month := input.Month
	if month == "" {
		// "current" is YNAB's server-side alias for the present month.
		month = "current"
	}
	fetched, err := c.api.GetMonth(ctx, budgetID, month)
	if err != nil {
		return GetMonthOutput{}, fail("get_month", err)
	}
	output := GetMonthOutput{
		BudgetID:     budgetID,
		Month:        fetched.Month,
		Income:       fetched.Income,
		Budgeted:     fetched.Budgeted,
		Activity:     fetched.Activity,
		ToBeBudgeted: fetched.ToBeBudgeted,
		AgeOfMoney:   fetched.AgeOfMoney,
		Categories:   []Category{},
		Note:         "Amounts are milliunits: 1/1000 of a currency unit. Hidden categories omitted unless include_hidden=true.",
	}
	for _, category := range fetched.Categories {
		if !visibleCategory(category, input.IncludeHidden) {
			continue
		}
		output.Categories = append(output.Categories, Category{
			ID:       category.ID,
			Name:     category.Name,
			Budgeted: category.Budgeted,
			Activity: category.Activity,
			Balance:  category.Balance,
			GoalType: category.GoalType,
		})
	}
	return output, nil
}
