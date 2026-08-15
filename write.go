package ynabmcp

import (
	"context"

	"github.com/justinswe/std/errors"
	"github.com/justinswe/ynab-mcp/internal/ynab"
)

// CreateTransactionInput describes the transaction to add.
type CreateTransactionInput struct {
	BudgetID   string `json:"budget_id,omitempty" jsonschema:"YNAB budget ID. Ignored when the client is scoped to one budget; defaults to the last-used budget."`
	AccountID  string `json:"account_id" jsonschema:"Account the transaction belongs to. Find IDs with list_accounts."`
	Date       string `json:"date" jsonschema:"Transaction date as an ISO date, e.g. 2026-08-14."`
	Amount     int64  `json:"amount_milliunits" jsonschema:"Amount in milliunits: 1/1000 of a currency unit, negative for outflows. A $12.34 purchase is -12340."`
	PayeeID    string `json:"payee_id,omitempty" jsonschema:"Existing payee ID. Prefer payee_name, which matches or creates a payee automatically."`
	PayeeName  string `json:"payee_name,omitempty" jsonschema:"Payee name; matched case-insensitively to an existing payee or created."`
	CategoryID string `json:"category_id,omitempty" jsonschema:"Category to assign. Find IDs with list_categories; omit to leave uncategorized."`
	Memo       string `json:"memo,omitempty" jsonschema:"Free-form note on the transaction."`
	Cleared    string `json:"cleared,omitempty" jsonschema:"'cleared' or 'uncleared' (default)."`
}

// TransactionOutput reports the transaction a write produced.
type TransactionOutput struct {
	BudgetID    string      `json:"budget_id"`
	Transaction Transaction `json:"transaction"`
	Note        string      `json:"note,omitempty"`
}

// CreateTransaction adds one transaction to a budget.
func (c *Client) CreateTransaction(ctx context.Context, input CreateTransactionInput) (TransactionOutput, error) {
	if input.AccountID == "" {
		return TransactionOutput{}, errors.New("account_id is required; find it with list_accounts")
	}
	if input.Date == "" {
		return TransactionOutput{}, errors.New("date is required, as an ISO date such as 2026-08-14")
	}
	if input.Amount == 0 {
		return TransactionOutput{}, errors.New("amount_milliunits is required and must be non-zero; negative for outflows")
	}
	budgetID := c.resolveBudget(input.BudgetID)
	// New transactions start unapproved so they surface for review in YNAB.
	tx := ynab.SaveTransaction{
		AccountID:  &input.AccountID,
		Date:       &input.Date,
		Amount:     &input.Amount,
		PayeeID:    optional(input.PayeeID),
		PayeeName:  optional(input.PayeeName),
		CategoryID: optional(input.CategoryID),
		Memo:       optional(input.Memo),
		Cleared:    optional(input.Cleared),
	}
	created, err := c.api.CreateTransaction(ctx, budgetID, tx)
	if err != nil {
		return TransactionOutput{}, fail("create_transaction", err)
	}
	return TransactionOutput{
		BudgetID:    budgetID,
		Transaction: mapTransaction(created),
		Note:        "Created unapproved; the user reviews it in YNAB.",
	}, nil
}

// UpdateTransactionInput names the transaction and the fields to change.
type UpdateTransactionInput struct {
	BudgetID      string `json:"budget_id,omitempty" jsonschema:"YNAB budget ID. Ignored when the client is scoped to one budget; defaults to the last-used budget."`
	TransactionID string `json:"transaction_id" jsonschema:"Transaction to change. Find IDs with list_transactions."`
	AccountID     string `json:"account_id,omitempty" jsonschema:"Move the transaction to this account."`
	Date          string `json:"date,omitempty" jsonschema:"New ISO date, e.g. 2026-08-14."`
	Amount        *int64 `json:"amount_milliunits,omitempty" jsonschema:"New amount in milliunits, negative for outflows. Zero is a valid amount."`
	PayeeID       string `json:"payee_id,omitempty" jsonschema:"New payee ID. Prefer payee_name."`
	PayeeName     string `json:"payee_name,omitempty" jsonschema:"New payee name; matched or created."`
	CategoryID    string `json:"category_id,omitempty" jsonschema:"New category. Find IDs with list_categories."`
	Memo          string `json:"memo,omitempty" jsonschema:"New memo."`
	Cleared       string `json:"cleared,omitempty" jsonschema:"'cleared', 'uncleared', or 'reconciled'."`
	Approve       *bool  `json:"approve,omitempty" jsonschema:"true marks the transaction approved; false returns it to needing approval."`
}

// UpdateTransaction changes fields on one transaction; omitted fields keep their values.
func (c *Client) UpdateTransaction(ctx context.Context, input UpdateTransactionInput) (TransactionOutput, error) {
	if input.TransactionID == "" {
		return TransactionOutput{}, errors.New("transaction_id is required; find it with list_transactions")
	}
	budgetID := c.resolveBudget(input.BudgetID)
	// Pointer inputs pass through directly: nil means "leave unchanged", so
	// approve=false and amount=0 are expressible updates rather than omissions.
	tx := ynab.SaveTransaction{
		AccountID:  optional(input.AccountID),
		Date:       optional(input.Date),
		Amount:     input.Amount,
		PayeeID:    optional(input.PayeeID),
		PayeeName:  optional(input.PayeeName),
		CategoryID: optional(input.CategoryID),
		Memo:       optional(input.Memo),
		Cleared:    optional(input.Cleared),
		Approved:   input.Approve,
	}
	if tx == (ynab.SaveTransaction{}) {
		return TransactionOutput{}, errors.New("nothing to update; pass at least one field to change")
	}
	updated, err := c.api.UpdateTransaction(ctx, budgetID, input.TransactionID, tx)
	if err != nil {
		return TransactionOutput{}, fail("update_transaction", err)
	}
	return TransactionOutput{BudgetID: budgetID, Transaction: mapTransaction(updated)}, nil
}

// optional returns nil for an empty string so the field is omitted on the wire.
func optional(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}
