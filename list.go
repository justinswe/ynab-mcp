package ynabmcp

import (
	"context"

	"github.com/justinswe/ynab-mcp/internal/ynab"
)

// listBudgetsInput exists only to give the list_budgets tool an input schema.
type listBudgetsInput struct{}

// Budget is one YNAB budget the token can read.
type Budget struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	FirstMonth string `json:"first_month,omitempty"`
	LastMonth  string `json:"last_month,omitempty"`
}

// ListBudgetsOutput reports the budgets the token can read.
type ListBudgetsOutput struct {
	Budgets []Budget `json:"budgets"`
	Note    string   `json:"note,omitempty"`
}

// ListBudgets reports the budgets the token can read.
func (c *Client) ListBudgets(ctx context.Context) (ListBudgetsOutput, error) {
	budgets, err := c.api.ListBudgets(ctx)
	if err != nil {
		return ListBudgetsOutput{}, fail("list_budgets", err)
	}
	output := ListBudgetsOutput{Budgets: make([]Budget, 0, len(budgets))}
	for _, budget := range budgets {
		output.Budgets = append(output.Budgets, Budget{
			ID:         budget.ID,
			Name:       budget.Name,
			FirstMonth: budget.FirstMonth,
			LastMonth:  budget.LastMonth,
		})
	}
	if len(output.Budgets) == 0 {
		output.Note = "This token has no budgets. Create one at app.ynab.com first."
	}
	return output, nil
}

// ListAccountsInput selects the budget to list accounts from.
type ListAccountsInput struct {
	BudgetID      string `json:"budget_id,omitempty" jsonschema:"YNAB budget ID. Ignored when the client is scoped to one budget; defaults to the last-used budget."`
	IncludeClosed bool   `json:"include_closed,omitempty" jsonschema:"Include closed accounts, which are omitted by default."`
}

// Account is one budget account.
type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	OnBudget bool   `json:"on_budget"`
	Closed   bool   `json:"closed,omitempty"`
	Balance  int64  `json:"balance_milliunits"`
}

// ListAccountsOutput reports the accounts in one budget.
type ListAccountsOutput struct {
	BudgetID string    `json:"budget_id"`
	Accounts []Account `json:"accounts"`
	Note     string    `json:"note,omitempty"`
}

// ListAccounts reports the accounts in one budget.
func (c *Client) ListAccounts(ctx context.Context, input ListAccountsInput) (ListAccountsOutput, error) {
	budgetID := c.resolveBudget(input.BudgetID)
	accounts, err := c.api.ListAccounts(ctx, budgetID)
	if err != nil {
		return ListAccountsOutput{}, fail("list_accounts", err)
	}
	// Slices start non-nil so empty lists marshal as [] rather than null.
	output := ListAccountsOutput{BudgetID: budgetID, Accounts: []Account{}}
	for _, account := range accounts {
		if account.Deleted || (account.Closed && !input.IncludeClosed) {
			continue
		}
		output.Accounts = append(output.Accounts, Account{
			ID:       account.ID,
			Name:     account.Name,
			Type:     account.Type,
			OnBudget: account.OnBudget,
			Closed:   account.Closed,
			Balance:  account.Balance,
		})
	}
	output.Note = accountsNote(len(output.Accounts), input.IncludeClosed)
	return output, nil
}

// accountsNote explains an empty account list or how to reveal closed accounts.
func accountsNote(count int, includeClosed bool) string {
	// The include_closed advice only makes sense when the caller has not
	// already passed it, or the agent retries the identical call in a loop.
	if count == 0 && includeClosed {
		return "No accounts in this budget."
	}
	if count == 0 {
		return "No open accounts in this budget. Pass include_closed=true to include closed accounts."
	}
	note := "Amounts are milliunits: 1/1000 of a currency unit, negative for debt."
	if !includeClosed {
		note += " Closed accounts omitted; pass include_closed=true to list them."
	}
	return note
}

// ListCategoriesInput selects the budget to list categories from.
type ListCategoriesInput struct {
	BudgetID      string `json:"budget_id,omitempty" jsonschema:"YNAB budget ID. Ignored when the client is scoped to one budget; defaults to the last-used budget."`
	IncludeHidden bool   `json:"include_hidden,omitempty" jsonschema:"Include hidden categories and groups, which are omitted by default."`
}

// CategoryGroup is one group of budget categories.
type CategoryGroup struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Categories []Category `json:"categories"`
}

// Category is one budget category with its current-month amounts in milliunits.
type Category struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Budgeted int64  `json:"budgeted_milliunits"`
	Activity int64  `json:"activity_milliunits"`
	Balance  int64  `json:"available_milliunits"`
	GoalType string `json:"goal_type,omitempty"`
}

// ListCategoriesOutput reports the category groups in one budget.
type ListCategoriesOutput struct {
	BudgetID string          `json:"budget_id"`
	Groups   []CategoryGroup `json:"category_groups"`
	Note     string          `json:"note,omitempty"`
}

// ListCategories reports the category groups in one budget.
func (c *Client) ListCategories(ctx context.Context, input ListCategoriesInput) (ListCategoriesOutput, error) {
	budgetID := c.resolveBudget(input.BudgetID)
	groups, err := c.api.ListCategories(ctx, budgetID)
	if err != nil {
		return ListCategoriesOutput{}, fail("list_categories", err)
	}
	output := ListCategoriesOutput{BudgetID: budgetID, Groups: []CategoryGroup{}}
	for _, group := range groups {
		if group.Deleted || (group.Hidden && !input.IncludeHidden) {
			continue
		}
		mapped := CategoryGroup{ID: group.ID, Name: group.Name}
		for _, category := range group.Categories {
			if !visibleCategory(category, input.IncludeHidden) {
				continue
			}
			mapped.Categories = append(mapped.Categories, Category{
				ID:       category.ID,
				Name:     category.Name,
				Budgeted: category.Budgeted,
				Activity: category.Activity,
				Balance:  category.Balance,
				GoalType: category.GoalType,
			})
		}
		if len(mapped.Categories) == 0 {
			continue
		}
		output.Groups = append(output.Groups, mapped)
	}
	if len(output.Groups) == 0 {
		output.Note = "No visible categories. Pass include_hidden=true to include hidden ones."
	} else {
		output.Note = "Amounts are current-month milliunits: 1/1000 of a currency unit."
	}
	return output, nil
}

// ListPayeesInput selects the budget to list payees from.
type ListPayeesInput struct {
	BudgetID string `json:"budget_id,omitempty" jsonschema:"YNAB budget ID. Ignored when the client is scoped to one budget; defaults to the last-used budget."`
}

// Payee is one budget payee.
type Payee struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// ListPayeesOutput reports the payees in one budget.
type ListPayeesOutput struct {
	BudgetID string  `json:"budget_id"`
	Payees   []Payee `json:"payees"`
	Note     string  `json:"note,omitempty"`
}

// ListPayees reports the payees in one budget.
func (c *Client) ListPayees(ctx context.Context, input ListPayeesInput) (ListPayeesOutput, error) {
	budgetID := c.resolveBudget(input.BudgetID)
	payees, err := c.api.ListPayees(ctx, budgetID)
	if err != nil {
		return ListPayeesOutput{}, fail("list_payees", err)
	}
	output := ListPayeesOutput{BudgetID: budgetID, Payees: []Payee{}}
	for _, payee := range payees {
		if payee.Deleted {
			continue
		}
		output.Payees = append(output.Payees, Payee{ID: payee.ID, Name: payee.Name})
	}
	if len(output.Payees) == 0 {
		output.Note = "No payees yet; they are created with transactions."
	}
	return output, nil
}

// visibleCategory reports whether a category should appear in tool output.
func visibleCategory(category ynab.Category, includeHidden bool) bool {
	return !category.Deleted && (includeHidden || !category.Hidden)
}
