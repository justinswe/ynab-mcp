// Package ynab is a thin client for the endpoints of the YNAB REST API v1 the
// MCP tools use, keeping every amount in milliunits exactly as the API reports it.
package ynab

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/justinswe/std/errors"
)

// DefaultBaseURL is the production YNAB API endpoint.
const DefaultBaseURL = "https://api.ynab.com/v1"

// requestTimeout bounds one YNAB call so a hung upstream cannot pin goroutines forever.
const requestTimeout = 30 * time.Second

// Client calls the YNAB API with one personal access token.
type Client struct {
	// BaseURL is the API root without a trailing slash. Tests point it at a
	// local server; everything else uses DefaultBaseURL.
	BaseURL string
	// HTTPClient issues the requests. Defaults to http.DefaultClient.
	HTTPClient *http.Client

	token string
}

// New creates a Client from a YNAB personal access token.
func New(token string) *Client {
	return &Client{
		BaseURL:    DefaultBaseURL,
		HTTPClient: &http.Client{Timeout: requestTimeout},
		token:      token,
	}
}

// APIError is a non-2xx response from YNAB, decoded from its error envelope.
type APIError struct {
	StatusCode int
	Name       string
	Detail     string
}

func (e *APIError) Error() string {
	if e.Detail == "" {
		return "YNAB API error " + e.Name
	}
	return "YNAB API error " + e.Name + ": " + e.Detail
}

// User identifies the token's owner.
type User struct {
	ID string `json:"id"`
}

// Budget is one YNAB budget.
type Budget struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	FirstMonth string `json:"first_month"`
	LastMonth  string `json:"last_month"`
}

// Account is one budget account.
type Account struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Type     string `json:"type"`
	OnBudget bool   `json:"on_budget"`
	Closed   bool   `json:"closed"`
	Balance  int64  `json:"balance"`
	Deleted  bool   `json:"deleted"`
}

// CategoryGroup is one group of budget categories.
type CategoryGroup struct {
	ID         string     `json:"id"`
	Name       string     `json:"name"`
	Hidden     bool       `json:"hidden"`
	Deleted    bool       `json:"deleted"`
	Categories []Category `json:"categories"`
}

// Category is one budget category with its current-month amounts.
type Category struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Hidden   bool   `json:"hidden"`
	Deleted  bool   `json:"deleted"`
	Budgeted int64  `json:"budgeted"`
	Activity int64  `json:"activity"`
	Balance  int64  `json:"balance"`
	GoalType string `json:"goal_type,omitempty"`
}

// Payee is one budget payee.
type Payee struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Deleted bool   `json:"deleted"`
}

// Transaction is one budget transaction.
type Transaction struct {
	ID           string `json:"id"`
	Date         string `json:"date"`
	Amount       int64  `json:"amount"`
	Memo         string `json:"memo,omitempty"`
	Cleared      string `json:"cleared"`
	Approved     bool   `json:"approved"`
	FlagColor    string `json:"flag_color,omitempty"`
	AccountID    string `json:"account_id"`
	AccountName  string `json:"account_name,omitempty"`
	PayeeID      string `json:"payee_id,omitempty"`
	PayeeName    string `json:"payee_name,omitempty"`
	CategoryID   string `json:"category_id,omitempty"`
	CategoryName string `json:"category_name,omitempty"`
	Deleted      bool   `json:"deleted"`
}

// Month is one budget month with its per-category amounts.
type Month struct {
	Month        string     `json:"month"`
	Income       int64      `json:"income"`
	Budgeted     int64      `json:"budgeted"`
	Activity     int64      `json:"activity"`
	ToBeBudgeted int64      `json:"to_be_budgeted"`
	AgeOfMoney   int64      `json:"age_of_money"`
	Categories   []Category `json:"categories"`
}

// SaveTransaction is the writable subset of a transaction; nil fields stay unchanged on update.
type SaveTransaction struct {
	AccountID  *string `json:"account_id,omitempty"`
	Date       *string `json:"date,omitempty"`
	Amount     *int64  `json:"amount,omitempty"`
	PayeeID    *string `json:"payee_id,omitempty"`
	PayeeName  *string `json:"payee_name,omitempty"`
	CategoryID *string `json:"category_id,omitempty"`
	Memo       *string `json:"memo,omitempty"`
	Cleared    *string `json:"cleared,omitempty"`
	Approved   *bool   `json:"approved,omitempty"`
	FlagColor  *string `json:"flag_color,omitempty"`
}

// TransactionsQuery filters ListTransactions; at most one ID filter may be set.
type TransactionsQuery struct {
	AccountID  string
	CategoryID string
	PayeeID    string
	// SinceDate is an ISO date (2026-01-31); empty returns the full history.
	SinceDate string
	// Type filters to "uncategorized" or "unapproved" transactions.
	Type string
}

// GetUser returns the token owner, which doubles as a token validity probe.
func (c *Client) GetUser(ctx context.Context) (User, error) {
	var out struct {
		User User `json:"user"`
	}
	err := c.do(ctx, http.MethodGet, "/user", nil, nil, &out)
	return out.User, err
}

// ListBudgets returns every budget the token can read.
func (c *Client) ListBudgets(ctx context.Context) ([]Budget, error) {
	var out struct {
		Budgets []Budget `json:"budgets"`
	}
	err := c.do(ctx, http.MethodGet, "/budgets", nil, nil, &out)
	return out.Budgets, err
}

// ListAccounts returns the accounts in one budget.
func (c *Client) ListAccounts(ctx context.Context, budgetID string) ([]Account, error) {
	var out struct {
		Accounts []Account `json:"accounts"`
	}
	err := c.do(ctx, http.MethodGet, "/budgets/"+url.PathEscape(budgetID)+"/accounts", nil, nil, &out)
	return out.Accounts, err
}

// ListCategories returns the category groups in one budget.
func (c *Client) ListCategories(ctx context.Context, budgetID string) ([]CategoryGroup, error) {
	var out struct {
		CategoryGroups []CategoryGroup `json:"category_groups"`
	}
	err := c.do(ctx, http.MethodGet, "/budgets/"+url.PathEscape(budgetID)+"/categories", nil, nil, &out)
	return out.CategoryGroups, err
}

// ListPayees returns the payees in one budget.
func (c *Client) ListPayees(ctx context.Context, budgetID string) ([]Payee, error) {
	var out struct {
		Payees []Payee `json:"payees"`
	}
	err := c.do(ctx, http.MethodGet, "/budgets/"+url.PathEscape(budgetID)+"/payees", nil, nil, &out)
	return out.Payees, err
}

// ListTransactions returns one budget's transactions, oldest first as YNAB reports them.
func (c *Client) ListTransactions(ctx context.Context, budgetID string, query TransactionsQuery) ([]Transaction, error) {
	path, err := transactionsPath(budgetID, query)
	if err != nil {
		return nil, err
	}
	values := url.Values{}
	if query.SinceDate != "" {
		values.Set("since_date", query.SinceDate)
	}
	if query.Type != "" {
		values.Set("type", query.Type)
	}
	var out struct {
		Transactions []Transaction `json:"transactions"`
	}
	err = c.do(ctx, http.MethodGet, path, values, nil, &out)
	return out.Transactions, err
}

// transactionsPath picks the endpoint that matches the query's filter.
func transactionsPath(budgetID string, query TransactionsQuery) (string, error) {
	base := "/budgets/" + url.PathEscape(budgetID)
	filters := 0
	path := base + "/transactions"
	if query.AccountID != "" {
		filters++
		path = base + "/accounts/" + url.PathEscape(query.AccountID) + "/transactions"
	}
	if query.CategoryID != "" {
		filters++
		path = base + "/categories/" + url.PathEscape(query.CategoryID) + "/transactions"
	}
	if query.PayeeID != "" {
		filters++
		path = base + "/payees/" + url.PathEscape(query.PayeeID) + "/transactions"
	}
	if filters > 1 {
		return "", errors.New("at most one of account, category, and payee may filter a transaction listing")
	}
	return path, nil
}

// GetMonth returns one budget month; month is an ISO first-of-month date or "current".
func (c *Client) GetMonth(ctx context.Context, budgetID, month string) (Month, error) {
	var out struct {
		Month Month `json:"month"`
	}
	err := c.do(ctx, http.MethodGet, "/budgets/"+url.PathEscape(budgetID)+"/months/"+url.PathEscape(month), nil, nil, &out)
	return out.Month, err
}

// CreateTransaction adds one transaction to a budget.
func (c *Client) CreateTransaction(ctx context.Context, budgetID string, tx SaveTransaction) (Transaction, error) {
	body := map[string]SaveTransaction{"transaction": tx}
	var out struct {
		Transaction Transaction `json:"transaction"`
	}
	err := c.do(ctx, http.MethodPost, "/budgets/"+url.PathEscape(budgetID)+"/transactions", nil, body, &out)
	return out.Transaction, err
}

// UpdateTransaction modifies one transaction; nil fields keep their value.
func (c *Client) UpdateTransaction(ctx context.Context, budgetID, transactionID string, tx SaveTransaction) (Transaction, error) {
	body := map[string]SaveTransaction{"transaction": tx}
	var out struct {
		Transaction Transaction `json:"transaction"`
	}
	path := "/budgets/" + url.PathEscape(budgetID) + "/transactions/" + url.PathEscape(transactionID)
	err := c.do(ctx, http.MethodPut, path, nil, body, &out)
	return out.Transaction, err
}

// do issues one authenticated request and decodes the data envelope into out.
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	target := c.BaseURL + path
	if len(query) > 0 {
		target += "?" + query.Encode()
	}
	var reader io.Reader
	if body != nil {
		encoded, err := json.Marshal(body)
		if err != nil {
			return errors.Wrap(err, "encode request")
		}
		reader = bytes.NewReader(encoded)
	}
	request, err := http.NewRequestWithContext(ctx, method, target, reader)
	if err != nil {
		return errors.Wrap(err, "build request")
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	response, err := c.httpClient().Do(request)
	if err != nil {
		return errors.Wrap(err, "call YNAB")
	}
	defer errors.Ignore(response.Body.Close)
	if response.StatusCode >= 300 {
		return decodeError(response)
	}
	envelope := struct {
		Data any `json:"data"`
	}{Data: out}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err != nil {
		return errors.Wrap(err, "decode response")
	}
	return nil
}

func (c *Client) httpClient() *http.Client {
	if c.HTTPClient != nil {
		return c.HTTPClient
	}
	return http.DefaultClient
}

// decodeError turns a non-2xx response into an *APIError.
func decodeError(response *http.Response) error {
	apiErr := &APIError{StatusCode: response.StatusCode, Name: response.Status}
	var envelope struct {
		Error struct {
			Name   string `json:"name"`
			Detail string `json:"detail"`
		} `json:"error"`
	}
	if err := json.NewDecoder(response.Body).Decode(&envelope); err == nil && envelope.Error.Name != "" {
		apiErr.Name = envelope.Error.Name
		apiErr.Detail = envelope.Error.Detail
	}
	return apiErr
}
