package ynab

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// capture records the one request a test expects the client to send.
type capture struct {
	method string
	path   string
	query  string
	auth   string
	body   string
}

// newTestClient serves response for every request and records what arrived.
func newTestClient(t *testing.T, status int, response string) (*Client, *capture) {
	t.Helper()
	captured := &capture{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		require.NoError(t, err)
		*captured = capture{
			method: r.Method,
			path:   r.URL.Path,
			query:  r.URL.RawQuery,
			auth:   r.Header.Get("Authorization"),
			body:   string(body),
		}
		w.WriteHeader(status)
		_, _ = w.Write([]byte(response))
	}))
	t.Cleanup(server.Close)
	client := New("the-token")
	client.BaseURL = server.URL
	return client, captured
}

func TestGetUser(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK, `{"data":{"user":{"id":"user-1"}}}`)

	user, err := client.GetUser(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "user-1", user.ID)
	assert.Equal(t, http.MethodGet, captured.method)
	assert.Equal(t, "/user", captured.path)
	assert.Equal(t, "Bearer the-token", captured.auth)
}

func TestListBudgets(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK,
		`{"data":{"budgets":[{"id":"b1","name":"Family","first_month":"2024-01-01","last_month":"2026-08-01"}]}}`)

	budgets, err := client.ListBudgets(context.Background())

	require.NoError(t, err)
	assert.Equal(t, "/budgets", captured.path)
	require.Len(t, budgets, 1)
	assert.Equal(t, Budget{ID: "b1", Name: "Family", FirstMonth: "2024-01-01", LastMonth: "2026-08-01"}, budgets[0])
}

func TestListAccounts(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK,
		`{"data":{"accounts":[{"id":"a1","name":"Checking","type":"checking","on_budget":true,"balance":102340}]}}`)

	accounts, err := client.ListAccounts(context.Background(), "b1")

	require.NoError(t, err)
	assert.Equal(t, "/budgets/b1/accounts", captured.path)
	require.Len(t, accounts, 1)
	assert.Equal(t, int64(102340), accounts[0].Balance)
	assert.True(t, accounts[0].OnBudget)
}

func TestListCategories(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK,
		`{"data":{"category_groups":[{"id":"g1","name":"Bills","categories":[{"id":"c1","name":"Rent","budgeted":1000,"activity":-500,"balance":500,"goal_type":"NEED"}]}]}}`)

	groups, err := client.ListCategories(context.Background(), "b1")

	require.NoError(t, err)
	assert.Equal(t, "/budgets/b1/categories", captured.path)
	require.Len(t, groups, 1)
	require.Len(t, groups[0].Categories, 1)
	assert.Equal(t, Category{ID: "c1", Name: "Rent", Budgeted: 1000, Activity: -500, Balance: 500, GoalType: "NEED"}, groups[0].Categories[0])
}

func TestListPayees(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK,
		`{"data":{"payees":[{"id":"p1","name":"Grocer","deleted":true}]}}`)

	payees, err := client.ListPayees(context.Background(), "b1")

	require.NoError(t, err)
	assert.Equal(t, "/budgets/b1/payees", captured.path)
	require.Len(t, payees, 1)
	assert.True(t, payees[0].Deleted)
}

func TestListTransactionsPaths(t *testing.T) {
	tests := []struct {
		name     string
		query    TransactionsQuery
		wantPath string
	}{
		{name: "unfiltered", query: TransactionsQuery{}, wantPath: "/budgets/b1/transactions"},
		{name: "by account", query: TransactionsQuery{AccountID: "a1"}, wantPath: "/budgets/b1/accounts/a1/transactions"},
		{name: "by category", query: TransactionsQuery{CategoryID: "c1"}, wantPath: "/budgets/b1/categories/c1/transactions"},
		{name: "by payee", query: TransactionsQuery{PayeeID: "p1"}, wantPath: "/budgets/b1/payees/p1/transactions"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client, captured := newTestClient(t, http.StatusOK, `{"data":{"transactions":[]}}`)

			_, err := client.ListTransactions(context.Background(), "b1", test.query)

			require.NoError(t, err)
			assert.Equal(t, test.wantPath, captured.path)
		})
	}
}

func TestListTransactionsQueryParams(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK,
		`{"data":{"transactions":[{"id":"t1","date":"2026-08-01","amount":-12340,"cleared":"cleared","approved":true,"account_id":"a1","account_name":"Checking","payee_name":"Grocer","category_name":"Food"}]}}`)

	transactions, err := client.ListTransactions(context.Background(), "b1",
		TransactionsQuery{SinceDate: "2026-08-01", Type: "unapproved"})

	require.NoError(t, err)
	assert.Equal(t, "since_date=2026-08-01&type=unapproved", captured.query)
	require.Len(t, transactions, 1)
	assert.Equal(t, int64(-12340), transactions[0].Amount)
	assert.Equal(t, "Grocer", transactions[0].PayeeName)
}

func TestListTransactionsRejectsMultipleFilters(t *testing.T) {
	client := New("the-token")

	_, err := client.ListTransactions(context.Background(), "b1",
		TransactionsQuery{AccountID: "a1", CategoryID: "c1"})

	require.ErrorContains(t, err, "at most one")
}

func TestGetMonth(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK,
		`{"data":{"month":{"month":"2026-08-01","income":500000,"budgeted":450000,"activity":-200000,"to_be_budgeted":50000,"age_of_money":21,"categories":[{"id":"c1","name":"Rent"}]}}}`)

	month, err := client.GetMonth(context.Background(), "b1", "current")

	require.NoError(t, err)
	assert.Equal(t, "/budgets/b1/months/current", captured.path)
	assert.Equal(t, int64(50000), month.ToBeBudgeted)
	assert.Equal(t, int64(21), month.AgeOfMoney)
	require.Len(t, month.Categories, 1)
}

func TestCreateTransaction(t *testing.T) {
	client, captured := newTestClient(t, http.StatusCreated,
		`{"data":{"transaction":{"id":"t1","date":"2026-08-14","amount":-12340,"cleared":"uncleared","account_id":"a1"}}}`)
	accountID, date, amount := "a1", "2026-08-14", int64(-12340)

	created, err := client.CreateTransaction(context.Background(), "b1",
		SaveTransaction{AccountID: &accountID, Date: &date, Amount: &amount})

	require.NoError(t, err)
	assert.Equal(t, http.MethodPost, captured.method)
	assert.Equal(t, "/budgets/b1/transactions", captured.path)
	assert.JSONEq(t, `{"transaction":{"account_id":"a1","date":"2026-08-14","amount":-12340}}`, captured.body)
	assert.Equal(t, "t1", created.ID)
}

func TestUpdateTransactionOmitsNilFields(t *testing.T) {
	client, captured := newTestClient(t, http.StatusOK,
		`{"data":{"transaction":{"id":"t1","date":"2026-08-14","amount":-12340,"cleared":"cleared","account_id":"a1"}}}`)
	memo := "groceries"

	updated, err := client.UpdateTransaction(context.Background(), "b1", "t1", SaveTransaction{Memo: &memo})

	require.NoError(t, err)
	assert.Equal(t, http.MethodPut, captured.method)
	assert.Equal(t, "/budgets/b1/transactions/t1", captured.path)
	assert.JSONEq(t, `{"transaction":{"memo":"groceries"}}`, captured.body)
	assert.Equal(t, "t1", updated.ID)
}

func TestAPIErrorFromEnvelope(t *testing.T) {
	client, _ := newTestClient(t, http.StatusUnauthorized,
		`{"error":{"id":"401","name":"unauthorized","detail":"Unauthorized"}}`)

	_, err := client.GetUser(context.Background())

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusUnauthorized, apiErr.StatusCode)
	assert.Equal(t, "unauthorized", apiErr.Name)
	assert.Equal(t, "YNAB API error unauthorized: Unauthorized", apiErr.Error())
}

func TestAPIErrorWithoutEnvelope(t *testing.T) {
	client, _ := newTestClient(t, http.StatusBadGateway, "upstream broke")

	_, err := client.ListBudgets(context.Background())

	var apiErr *APIError
	require.ErrorAs(t, err, &apiErr)
	assert.Equal(t, http.StatusBadGateway, apiErr.StatusCode)
	assert.Contains(t, apiErr.Error(), "502")
}

func TestDoRejectsUnreachableServer(t *testing.T) {
	client := New("the-token")
	client.BaseURL = "http://127.0.0.1:1"

	_, err := client.GetUser(context.Background())

	require.ErrorContains(t, err, "call YNAB")
}

func TestSaveTransactionRoundTrips(t *testing.T) {
	// The wire format is the API contract: every non-nil field must appear
	// under its YNAB name, and nothing else may.
	amount, approved := int64(500), true
	encoded, err := json.Marshal(SaveTransaction{Amount: &amount, Approved: &approved})

	require.NoError(t, err)
	assert.JSONEq(t, `{"amount":500,"approved":true}`, string(encoded))
}
