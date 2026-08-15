package ynabmcp

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/justinswe/std/errors"
	"github.com/justinswe/ynab-mcp/internal/ynab"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ptr builds a pointer to any literal for optional-field inputs.
func ptr[T any](value T) *T { return &value }

// fakeAPI implements the api interface with per-method results and errors, and
// records the arguments of the last call to each method.
type fakeAPI struct {
	user            ynab.User
	userErr         error
	budgets         []ynab.Budget
	budgetsErr      error
	accounts        []ynab.Account
	accountsErr     error
	groups          []ynab.CategoryGroup
	groupsErr       error
	payees          []ynab.Payee
	payeesErr       error
	transactions    []ynab.Transaction
	transactionsErr error
	month           ynab.Month
	monthErr        error
	saved           ynab.Transaction
	saveErr         error

	gotBudgetID      string
	gotQuery         ynab.TransactionsQuery
	gotMonth         string
	gotSave          ynab.SaveTransaction
	gotTransactionID string
}

func (f *fakeAPI) GetUser(context.Context) (ynab.User, error) { return f.user, f.userErr }

func (f *fakeAPI) ListBudgets(context.Context) ([]ynab.Budget, error) {
	return f.budgets, f.budgetsErr
}

func (f *fakeAPI) ListAccounts(_ context.Context, budgetID string) ([]ynab.Account, error) {
	f.gotBudgetID = budgetID
	return f.accounts, f.accountsErr
}

func (f *fakeAPI) ListCategories(_ context.Context, budgetID string) ([]ynab.CategoryGroup, error) {
	f.gotBudgetID = budgetID
	return f.groups, f.groupsErr
}

func (f *fakeAPI) ListPayees(_ context.Context, budgetID string) ([]ynab.Payee, error) {
	f.gotBudgetID = budgetID
	return f.payees, f.payeesErr
}

func (f *fakeAPI) ListTransactions(_ context.Context, budgetID string, query ynab.TransactionsQuery) ([]ynab.Transaction, error) {
	f.gotBudgetID, f.gotQuery = budgetID, query
	return f.transactions, f.transactionsErr
}

func (f *fakeAPI) GetMonth(_ context.Context, budgetID, month string) (ynab.Month, error) {
	f.gotBudgetID, f.gotMonth = budgetID, month
	return f.month, f.monthErr
}

func (f *fakeAPI) CreateTransaction(_ context.Context, budgetID string, tx ynab.SaveTransaction) (ynab.Transaction, error) {
	f.gotBudgetID, f.gotSave = budgetID, tx
	return f.saved, f.saveErr
}

func (f *fakeAPI) UpdateTransaction(_ context.Context, budgetID, transactionID string, tx ynab.SaveTransaction) (ynab.Transaction, error) {
	f.gotBudgetID, f.gotTransactionID, f.gotSave = budgetID, transactionID, tx
	return f.saved, f.saveErr
}

// newFakeClient builds a Client over a fakeAPI, bypassing New's real client.
func newFakeClient(fake *fakeAPI, budgetID string, allowWrite bool) *Client {
	return &Client{api: fake, budgetID: budgetID, allowWrite: allowWrite}
}

func TestNewRequiresToken(t *testing.T) {
	_, err := New(Options{AccessToken: "  "})

	require.ErrorContains(t, err, "AccessToken is required")
}

func TestNewTrimsOptions(t *testing.T) {
	client, err := New(Options{AccessToken: " token ", BudgetID: " b1 "})

	require.NoError(t, err)
	assert.Equal(t, "b1", client.budgetID)
}

func TestResolveBudget(t *testing.T) {
	tests := []struct {
		name   string
		scoped string
		input  string
		want   string
	}{
		{name: "configured budget wins", scoped: "b1", input: "b2", want: "b1"},
		{name: "input used when unscoped", input: " b2 ", want: "b2"},
		{name: "defaults to last-used", want: "last-used"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeClient(&fakeAPI{}, test.scoped, false)

			assert.Equal(t, test.want, client.resolveBudget(test.input))
		})
	}
}

func TestCheckToken(t *testing.T) {
	client := newFakeClient(&fakeAPI{user: ynab.User{ID: "u1"}}, "", false)

	require.NoError(t, client.CheckToken(context.Background()))
}

func TestCheckTokenRejectsBadToken(t *testing.T) {
	fake := &fakeAPI{userErr: &ynab.APIError{StatusCode: http.StatusUnauthorized}}
	client := newFakeClient(fake, "", false)

	err := client.CheckToken(context.Background())

	require.ErrorIs(t, err, ErrUnauthorized)
}

func TestFail(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		sentinel error
		contains string
	}{
		{name: "401 maps to ErrUnauthorized", err: &ynab.APIError{StatusCode: 401}, sentinel: ErrUnauthorized, contains: "Do not retry"},
		{name: "404 maps to ErrNotFound", err: &ynab.APIError{StatusCode: 404}, sentinel: ErrNotFound, contains: "list tools"},
		{name: "429 maps to ErrRateLimited", err: &ynab.APIError{StatusCode: 429}, sentinel: ErrRateLimited, contains: "200 requests per hour"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := fail("list_accounts", test.err)

			require.ErrorIs(t, err, test.sentinel)
			assert.ErrorContains(t, err, test.contains)
			assert.ErrorContains(t, err, "list_accounts failed")
		})
	}
}

func TestFailWrapsOtherErrors(t *testing.T) {
	apiErr := fail("get_month", &ynab.APIError{StatusCode: 500, Name: "internal"})
	assert.ErrorContains(t, apiErr, "get_month failed")
	assert.ErrorContains(t, apiErr, "internal")

	plain := fail("get_month", errors.New("timeout"))
	assert.ErrorContains(t, plain, "get_month failed")
	assert.ErrorContains(t, plain, "timeout")
}

func TestListBudgets(t *testing.T) {
	fake := &fakeAPI{budgets: []ynab.Budget{{ID: "b1", Name: "Family", FirstMonth: "2024-01-01", LastMonth: "2026-08-01"}}}
	client := newFakeClient(fake, "", false)

	output, err := client.ListBudgets(context.Background())

	require.NoError(t, err)
	require.Len(t, output.Budgets, 1)
	assert.Equal(t, Budget{ID: "b1", Name: "Family", FirstMonth: "2024-01-01", LastMonth: "2026-08-01"}, output.Budgets[0])
	assert.Empty(t, output.Note)
}

func TestListBudgetsExplainsEmptyList(t *testing.T) {
	client := newFakeClient(&fakeAPI{}, "", false)

	output, err := client.ListBudgets(context.Background())

	require.NoError(t, err)
	assert.Contains(t, output.Note, "no budgets")
}

func TestListAccountsSkipsClosedAndDeleted(t *testing.T) {
	fake := &fakeAPI{accounts: []ynab.Account{
		{ID: "a1", Name: "Checking", Type: "checking", OnBudget: true, Balance: 1000},
		{ID: "a2", Name: "Old", Closed: true},
		{ID: "a3", Name: "Gone", Deleted: true},
	}}
	client := newFakeClient(fake, "b1", false)

	output, err := client.ListAccounts(context.Background(), ListAccountsInput{})

	require.NoError(t, err)
	assert.Equal(t, "b1", fake.gotBudgetID)
	require.Len(t, output.Accounts, 1)
	assert.Equal(t, "a1", output.Accounts[0].ID)
	assert.Contains(t, output.Note, "milliunits")
}

func TestListAccountsIncludesClosedOnRequest(t *testing.T) {
	fake := &fakeAPI{accounts: []ynab.Account{
		{ID: "a1", Name: "Checking"},
		{ID: "a2", Name: "Old", Closed: true},
	}}
	client := newFakeClient(fake, "", false)

	output, err := client.ListAccounts(context.Background(), ListAccountsInput{IncludeClosed: true})

	require.NoError(t, err)
	assert.Len(t, output.Accounts, 2)
	// The milliunits explanation must reach the caller in every non-empty case.
	assert.Contains(t, output.Note, "milliunits")
	assert.NotContains(t, output.Note, "include_closed")
}

func TestAccountsNote(t *testing.T) {
	tests := []struct {
		name          string
		count         int
		includeClosed bool
		contains      string
		excludes      string
	}{
		{name: "empty after asking for closed", count: 0, includeClosed: true, contains: "No accounts", excludes: "include_closed"},
		{name: "empty suggests closed", count: 0, includeClosed: false, contains: "include_closed=true"},
		{name: "results with closed shown", count: 2, includeClosed: true, contains: "milliunits", excludes: "omitted"},
		{name: "results hint at closed", count: 2, includeClosed: false, contains: "Closed accounts omitted"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			note := accountsNote(test.count, test.includeClosed)

			assert.Contains(t, note, test.contains)
			if test.excludes != "" {
				assert.NotContains(t, note, test.excludes)
			}
		})
	}
}

func TestListAccountsMapsAPIErrors(t *testing.T) {
	fake := &fakeAPI{accountsErr: &ynab.APIError{StatusCode: 404}}
	client := newFakeClient(fake, "", false)

	_, err := client.ListAccounts(context.Background(), ListAccountsInput{})

	require.ErrorIs(t, err, ErrNotFound)
}

func TestListCategoriesSkipsHiddenAndEmptyGroups(t *testing.T) {
	fake := &fakeAPI{groups: []ynab.CategoryGroup{
		{ID: "g1", Name: "Bills", Categories: []ynab.Category{
			{ID: "c1", Name: "Rent", Budgeted: 1000, Activity: -500, Balance: 500, GoalType: "NEED"},
			{ID: "c2", Name: "Hidden", Hidden: true},
		}},
		{ID: "g2", Name: "Hidden group", Hidden: true, Categories: []ynab.Category{{ID: "c3"}}},
		{ID: "g3", Name: "Emptied", Categories: []ynab.Category{{ID: "c4", Deleted: true}}},
	}}
	client := newFakeClient(fake, "", false)

	output, err := client.ListCategories(context.Background(), ListCategoriesInput{})

	require.NoError(t, err)
	require.Len(t, output.Groups, 1)
	require.Len(t, output.Groups[0].Categories, 1)
	assert.Equal(t, Category{ID: "c1", Name: "Rent", Budgeted: 1000, Activity: -500, Balance: 500, GoalType: "NEED"}, output.Groups[0].Categories[0])
}

func TestListCategoriesIncludesHiddenOnRequest(t *testing.T) {
	fake := &fakeAPI{groups: []ynab.CategoryGroup{
		{ID: "g1", Name: "Bills", Hidden: true, Categories: []ynab.Category{{ID: "c1", Name: "Rent", Hidden: true}}},
	}}
	client := newFakeClient(fake, "", false)

	output, err := client.ListCategories(context.Background(), ListCategoriesInput{IncludeHidden: true})

	require.NoError(t, err)
	require.Len(t, output.Groups, 1)
	assert.Len(t, output.Groups[0].Categories, 1)
}

func TestListCategoriesExplainsEmptyList(t *testing.T) {
	client := newFakeClient(&fakeAPI{}, "", false)

	output, err := client.ListCategories(context.Background(), ListCategoriesInput{})

	require.NoError(t, err)
	assert.Contains(t, output.Note, "include_hidden")
}

func TestListPayeesSkipsDeleted(t *testing.T) {
	fake := &fakeAPI{payees: []ynab.Payee{
		{ID: "p1", Name: "Grocer"},
		{ID: "p2", Name: "Gone", Deleted: true},
	}}
	client := newFakeClient(fake, "", false)

	output, err := client.ListPayees(context.Background(), ListPayeesInput{})

	require.NoError(t, err)
	require.Len(t, output.Payees, 1)
	assert.Equal(t, "Grocer", output.Payees[0].Name)
}

func TestListTransactionsNewestFirstWithLimit(t *testing.T) {
	fake := &fakeAPI{transactions: []ynab.Transaction{
		{ID: "t1", Date: "2026-08-01", Amount: -100},
		{ID: "t2", Date: "2026-08-02", Amount: -200, Deleted: true},
		{ID: "t3", Date: "2026-08-03", Amount: -300},
		{ID: "t4", Date: "2026-08-04", Amount: -400, AccountName: "Checking", PayeeName: "Grocer", CategoryName: "Food"},
	}}
	client := newFakeClient(fake, "", false)

	output, err := client.ListTransactions(context.Background(), ListTransactionsInput{Limit: 2})

	require.NoError(t, err)
	require.Len(t, output.Transactions, 2)
	assert.Equal(t, "t4", output.Transactions[0].ID)
	assert.Equal(t, "Grocer", output.Transactions[0].Payee)
	assert.Equal(t, "t3", output.Transactions[1].ID)
	assert.Contains(t, output.Note, "truncated")
}

func TestListTransactionsPassesFiltersThrough(t *testing.T) {
	fake := &fakeAPI{}
	client := newFakeClient(fake, "b1", false)

	output, err := client.ListTransactions(context.Background(), ListTransactionsInput{
		AccountID: "a1", SinceDate: "2026-08-01", Type: "unapproved",
	})

	require.NoError(t, err)
	assert.Equal(t, "b1", fake.gotBudgetID)
	assert.Equal(t, ynab.TransactionsQuery{AccountID: "a1", SinceDate: "2026-08-01", Type: "unapproved"}, fake.gotQuery)
	assert.Contains(t, output.Note, "No matching transactions")
}

func TestListTransactionsMapsAPIErrors(t *testing.T) {
	fake := &fakeAPI{transactionsErr: &ynab.APIError{StatusCode: 429}}
	client := newFakeClient(fake, "", false)

	_, err := client.ListTransactions(context.Background(), ListTransactionsInput{})

	require.ErrorIs(t, err, ErrRateLimited)
}

func TestGetMonthDefaultsToCurrent(t *testing.T) {
	fake := &fakeAPI{month: ynab.Month{
		Month: "2026-08-01", Income: 500000, Budgeted: 450000, Activity: -200000, ToBeBudgeted: 50000, AgeOfMoney: 21,
		Categories: []ynab.Category{
			{ID: "c1", Name: "Rent", Budgeted: 1000},
			{ID: "c2", Name: "Hidden", Hidden: true},
		},
	}}
	client := newFakeClient(fake, "", false)

	output, err := client.GetMonth(context.Background(), GetMonthInput{})

	require.NoError(t, err)
	assert.Equal(t, "current", fake.gotMonth)
	assert.Equal(t, "last-used", fake.gotBudgetID)
	assert.Equal(t, int64(50000), output.ToBeBudgeted)
	require.Len(t, output.Categories, 1)
	assert.Equal(t, "Rent", output.Categories[0].Name)
}

func TestCreateTransactionValidatesInput(t *testing.T) {
	tests := []struct {
		name     string
		input    CreateTransactionInput
		contains string
	}{
		{name: "missing account", input: CreateTransactionInput{Date: "2026-08-14", Amount: -1}, contains: "account_id"},
		{name: "missing date", input: CreateTransactionInput{AccountID: "a1", Amount: -1}, contains: "date"},
		{name: "missing amount", input: CreateTransactionInput{AccountID: "a1", Date: "2026-08-14"}, contains: "amount_milliunits"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := newFakeClient(&fakeAPI{}, "", true)

			_, err := client.CreateTransaction(context.Background(), test.input)

			require.ErrorContains(t, err, test.contains)
		})
	}
}

func TestCreateTransaction(t *testing.T) {
	fake := &fakeAPI{saved: ynab.Transaction{ID: "t1", Date: "2026-08-14", Amount: -12340, AccountName: "Checking"}}
	client := newFakeClient(fake, "b1", true)

	output, err := client.CreateTransaction(context.Background(), CreateTransactionInput{
		AccountID: "a1", Date: "2026-08-14", Amount: -12340, PayeeName: "Grocer", Memo: "food",
	})

	require.NoError(t, err)
	assert.Equal(t, "b1", fake.gotBudgetID)
	require.NotNil(t, fake.gotSave.AccountID)
	assert.Equal(t, "a1", *fake.gotSave.AccountID)
	require.NotNil(t, fake.gotSave.PayeeName)
	assert.Equal(t, "Grocer", *fake.gotSave.PayeeName)
	assert.Nil(t, fake.gotSave.PayeeID)
	assert.Nil(t, fake.gotSave.CategoryID)
	assert.Equal(t, "t1", output.Transaction.ID)
	assert.Contains(t, output.Note, "unapproved")
}

func TestUpdateTransactionRequiresIDAndChanges(t *testing.T) {
	client := newFakeClient(&fakeAPI{}, "", true)

	_, err := client.UpdateTransaction(context.Background(), UpdateTransactionInput{})
	require.ErrorContains(t, err, "transaction_id")

	_, err = client.UpdateTransaction(context.Background(), UpdateTransactionInput{TransactionID: "t1"})
	require.ErrorContains(t, err, "nothing to update")
}

func TestUpdateTransaction(t *testing.T) {
	fake := &fakeAPI{saved: ynab.Transaction{ID: "t1", CategoryName: "Food"}}
	client := newFakeClient(fake, "", true)

	output, err := client.UpdateTransaction(context.Background(), UpdateTransactionInput{
		TransactionID: "t1", CategoryID: "c1", Amount: ptr(int64(-500)), Approve: ptr(true),
	})

	require.NoError(t, err)
	assert.Equal(t, "t1", fake.gotTransactionID)
	require.NotNil(t, fake.gotSave.CategoryID)
	assert.Equal(t, "c1", *fake.gotSave.CategoryID)
	require.NotNil(t, fake.gotSave.Amount)
	assert.Equal(t, int64(-500), *fake.gotSave.Amount)
	require.NotNil(t, fake.gotSave.Approved)
	assert.True(t, *fake.gotSave.Approved)
	assert.Nil(t, fake.gotSave.Date)
	assert.Equal(t, "Food", output.Transaction.Category)
}

// connect registers the client's tools and returns a live in-memory session.
func connect(t *testing.T, client *Client, names ...string) *mcp.ClientSession {
	t.Helper()
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
	require.NoError(t, client.RegisterTools(server, names...))
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	_, err := server.Connect(context.Background(), serverTransport, nil)
	require.NoError(t, err)
	session, err := mcp.NewClient(&mcp.Implementation{Name: "tester", Version: "0"}, nil).
		Connect(context.Background(), clientTransport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

// toolNames lists the registered tool names through the MCP protocol.
func toolNames(t *testing.T, session *mcp.ClientSession) []string {
	t.Helper()
	listed, err := session.ListTools(context.Background(), nil)
	require.NoError(t, err)
	names := make([]string, 0, len(listed.Tools))
	for _, tool := range listed.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func TestRegisterToolsDefaultSet(t *testing.T) {
	session := connect(t, newFakeClient(&fakeAPI{}, "", false))

	assert.ElementsMatch(t, []string{
		"list_budgets", "list_accounts", "list_categories", "list_payees", "list_transactions", "get_month",
	}, toolNames(t, session))
}

func TestRegisterToolsWithheldByScope(t *testing.T) {
	session := connect(t, newFakeClient(&fakeAPI{}, "b1", true))

	names := toolNames(t, session)
	assert.NotContains(t, names, "list_budgets")
	assert.Contains(t, names, "create_transaction")
	assert.Contains(t, names, "update_transaction")
}

func TestRegisterToolsExplicitSubset(t *testing.T) {
	session := connect(t, newFakeClient(&fakeAPI{}, "", false), "list_accounts", "get_month")

	assert.ElementsMatch(t, []string{"list_accounts", "get_month"}, toolNames(t, session))
}

func TestRegisterToolsRejectsWithheldName(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	err := newFakeClient(&fakeAPI{}, "", false).RegisterTools(server, "create_transaction")

	require.ErrorContains(t, err, "AllowWrite")
}

func TestRegisterToolsRejectsUnknownNames(t *testing.T) {
	server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)

	err := newFakeClient(&fakeAPI{}, "", false).RegisterTools(server, "list_accounts", "zz", "aa")

	require.ErrorContains(t, err, "unknown tools: aa, zz")
}

func TestCallToolReportsErrorsInResult(t *testing.T) {
	fake := &fakeAPI{accountsErr: &ynab.APIError{StatusCode: 404}}
	session := connect(t, newFakeClient(fake, "", false))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_accounts", Arguments: map[string]any{},
	})

	require.NoError(t, err)
	require.True(t, result.IsError)
	text, ok := result.Content[0].(*mcp.TextContent)
	require.True(t, ok)
	assert.Contains(t, text.Text, "list_accounts failed")
	// An error result must not also carry a schema-valid empty success payload.
	assert.Nil(t, result.StructuredContent)
}

func TestCallToolReturnsStructuredOutput(t *testing.T) {
	fake := &fakeAPI{accounts: []ynab.Account{{ID: "a1", Name: "Checking", Type: "checking", Balance: 1000}}}
	session := connect(t, newFakeClient(fake, "b1", false))

	result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
		Name: "list_accounts", Arguments: map[string]any{},
	})

	require.NoError(t, err)
	require.False(t, result.IsError)
	output, ok := result.StructuredContent.(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "b1", output["budget_id"])
}

func TestRegisterToolsFailedSelectionRegistersNothing(t *testing.T) {
	tests := []struct {
		name  string
		names []string
	}{
		{name: "withheld after valid", names: []string{"list_accounts", "create_transaction"}},
		{name: "unknown after valid", names: []string{"list_accounts", "zz"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			server := mcp.NewServer(&mcp.Implementation{Name: "test", Version: "0"}, nil)
			client := newFakeClient(&fakeAPI{}, "", false)

			require.Error(t, client.RegisterTools(server, test.names...))

			clientTransport, serverTransport := mcp.NewInMemoryTransports()
			_, err := server.Connect(context.Background(), serverTransport, nil)
			require.NoError(t, err)
			session, err := mcp.NewClient(&mcp.Implementation{Name: "tester", Version: "0"}, nil).
				Connect(context.Background(), clientTransport, nil)
			require.NoError(t, err)
			t.Cleanup(func() { _ = session.Close() })
			listed, err := session.ListTools(context.Background(), nil)
			require.NoError(t, err)
			assert.Empty(t, listed.Tools)
		})
	}
}

func TestFailPreservesAPIErrorDetail(t *testing.T) {
	apiErr := &ynab.APIError{StatusCode: 404, Name: "resource_not_found", Detail: "Category not found"}

	err := fail("get_month", apiErr)

	require.ErrorIs(t, err, ErrNotFound)
	assert.ErrorContains(t, err, "Category not found")
	recovered, ok := errors.AsType[*ynab.APIError](err)
	require.True(t, ok)
	assert.Equal(t, http.StatusNotFound, recovered.StatusCode)
}

func TestClampLimit(t *testing.T) {
	tests := []struct {
		name  string
		limit int
		want  int
	}{
		{name: "zero defaults", limit: 0, want: defaultTransactionLimit},
		{name: "negative defaults", limit: -5, want: defaultTransactionLimit},
		{name: "one passes", limit: 1, want: 1},
		{name: "exactly max passes", limit: maxTransactionLimit, want: maxTransactionLimit},
		{name: "over max clamps", limit: maxTransactionLimit + 1, want: maxTransactionLimit},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.want, clampLimit(test.limit))
		})
	}
}

func TestTransactionWindowValidation(t *testing.T) {
	tests := []struct {
		name     string
		input    ListTransactionsInput
		contains string
	}{
		{name: "invalid month", input: ListTransactionsInput{SinceDate: "2026-13-01"}, contains: "invalid since_date"},
		{name: "wrong format", input: ListTransactionsInput{UntilDate: "08/14/2026"}, contains: "invalid until_date"},
		{name: "whitespace date", input: ListTransactionsInput{SinceDate: " "}, contains: "invalid since_date"},
		{name: "inverted window", input: ListTransactionsInput{SinceDate: "2026-08-10", UntilDate: "2026-08-01"}, contains: "before since_date"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := transactionWindow(test.input)

			require.ErrorContains(t, err, test.contains)
		})
	}
}

func TestTransactionWindowDefaultsSinceWhenUnfiltered(t *testing.T) {
	// Computed before and after the call so a midnight rollover cannot flake.
	before := time.Now().AddDate(0, 0, -defaultSinceDays).Format(time.DateOnly)
	since, until, err := transactionWindow(ListTransactionsInput{})
	after := time.Now().AddDate(0, 0, -defaultSinceDays).Format(time.DateOnly)

	require.NoError(t, err)
	assert.Contains(t, []string{before, after}, since)
	assert.Empty(t, until)
}

func TestTransactionWindowSkipsDefaultWhenNarrowed(t *testing.T) {
	tests := []struct {
		name  string
		input ListTransactionsInput
	}{
		{name: "account filter", input: ListTransactionsInput{AccountID: "a1"}},
		{name: "category filter", input: ListTransactionsInput{CategoryID: "c1"}},
		{name: "payee filter", input: ListTransactionsInput{PayeeID: "p1"}},
		{name: "type filter", input: ListTransactionsInput{Type: "unapproved"}},
		{name: "until bound", input: ListTransactionsInput{UntilDate: "2026-08-14"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			since, _, err := transactionWindow(test.input)

			require.NoError(t, err)
			assert.Empty(t, since)
		})
	}
}

func TestTransactionWindowSingleDay(t *testing.T) {
	since, until, err := transactionWindow(ListTransactionsInput{SinceDate: "2026-08-14", UntilDate: "2026-08-14"})

	require.NoError(t, err)
	assert.Equal(t, "2026-08-14", since)
	assert.Equal(t, "2026-08-14", until)
}

func TestListTransactionsUntilDateInclusive(t *testing.T) {
	fake := &fakeAPI{transactions: []ynab.Transaction{
		{ID: "t1", Date: "2026-08-01"},
		{ID: "t2", Date: "2026-08-02"},
		{ID: "t3", Date: "2026-08-03"},
		{ID: "t4", Date: "2026-08-04"},
	}}
	client := newFakeClient(fake, "b1", false)

	output, err := client.ListTransactions(context.Background(), ListTransactionsInput{
		SinceDate: "2026-08-02", UntilDate: "2026-08-03",
	})

	require.NoError(t, err)
	// since_date is enforced server-side (passed through to the API); until_date
	// is the client-side inclusive bound, so t3 stays and t4 goes.
	assert.Equal(t, "2026-08-02", fake.gotQuery.SinceDate)
	require.Len(t, output.Transactions, 3)
	assert.Equal(t, "t3", output.Transactions[0].ID)
	assert.Equal(t, "t1", output.Transactions[2].ID)
}

func TestListTransactionsDeletedHeavyWindow(t *testing.T) {
	fake := &fakeAPI{transactions: []ynab.Transaction{
		{ID: "t1", Date: "2026-08-01"},
		{ID: "t2", Date: "2026-08-02"},
		{ID: "t3", Date: "2026-08-03", Deleted: true},
		{ID: "t4", Date: "2026-08-04", Deleted: true},
	}}
	client := newFakeClient(fake, "b1", false)

	output, err := client.ListTransactions(context.Background(), ListTransactionsInput{Limit: 1})

	require.NoError(t, err)
	// Deleted rows must not consume limit slots: the newest live row wins, and
	// the remaining live row past the cut still triggers the truncation note.
	require.Len(t, output.Transactions, 1)
	assert.Equal(t, "t2", output.Transactions[0].ID)
	assert.Contains(t, output.Note, "truncated")
}

func TestListTransactionsDoesNotMutateAPISlice(t *testing.T) {
	fake := &fakeAPI{transactions: []ynab.Transaction{
		{ID: "t1", Date: "2026-08-01"},
		{ID: "t2", Date: "2026-08-02"},
	}}
	client := newFakeClient(fake, "b1", false)

	first, err := client.ListTransactions(context.Background(), ListTransactionsInput{})
	require.NoError(t, err)
	second, err := client.ListTransactions(context.Background(), ListTransactionsInput{})
	require.NoError(t, err)

	// The fake returns the same backing array both times; identical output
	// proves the listing never reorders the callee's slice in place.
	assert.Equal(t, first.Transactions, second.Transactions)
	assert.Equal(t, "t2", second.Transactions[0].ID)
	assert.Equal(t, "t1", fake.transactions[0].ID)
}

func TestListTransactionsDefaultedEmptyNote(t *testing.T) {
	client := newFakeClient(&fakeAPI{}, "b1", false)

	output, err := client.ListTransactions(context.Background(), ListTransactionsInput{})

	require.NoError(t, err)
	assert.Contains(t, output.Note, "last 90 days")
	assert.Contains(t, output.Note, "since_date")
}

func TestGetMonthIncludesHiddenOnRequest(t *testing.T) {
	fake := &fakeAPI{month: ynab.Month{
		Month: "2026-08-01",
		Categories: []ynab.Category{
			{ID: "c1", Name: "Rent"},
			{ID: "c2", Name: "Holiday Gifts", Hidden: true, Balance: 400000},
			{ID: "c3", Name: "Gone", Deleted: true},
		},
	}}
	client := newFakeClient(fake, "", false)

	output, err := client.GetMonth(context.Background(), GetMonthInput{IncludeHidden: true})

	require.NoError(t, err)
	require.Len(t, output.Categories, 2)
	assert.Equal(t, "Holiday Gifts", output.Categories[1].Name)
}

func TestUpdateTransactionUnapproveAndZeroAmount(t *testing.T) {
	fake := &fakeAPI{saved: ynab.Transaction{ID: "t1"}}
	client := newFakeClient(fake, "", true)

	_, err := client.UpdateTransaction(context.Background(), UpdateTransactionInput{
		TransactionID: "t1", Approve: ptr(false), Amount: ptr(int64(0)),
	})

	require.NoError(t, err)
	// false and zero are explicit values, not omissions.
	require.NotNil(t, fake.gotSave.Approved)
	assert.False(t, *fake.gotSave.Approved)
	require.NotNil(t, fake.gotSave.Amount)
	assert.Zero(t, *fake.gotSave.Amount)
}

func TestEmptyOutputsMarshalAsArrays(t *testing.T) {
	client := newFakeClient(&fakeAPI{}, "b1", false)
	ctx := context.Background()

	accounts, err := client.ListAccounts(ctx, ListAccountsInput{})
	require.NoError(t, err)
	categories, err := client.ListCategories(ctx, ListCategoriesInput{})
	require.NoError(t, err)
	payees, err := client.ListPayees(ctx, ListPayeesInput{})
	require.NoError(t, err)
	transactions, err := client.ListTransactions(ctx, ListTransactionsInput{})
	require.NoError(t, err)
	month, err := client.GetMonth(ctx, GetMonthInput{})
	require.NoError(t, err)

	for name, output := range map[string]any{
		"accounts": accounts, "category_groups": categories, "payees": payees,
		"transactions": transactions, "categories": month,
	} {
		encoded, err := json.Marshal(output)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), `"`+name+`":[]`, "field %s must marshal as [], not null", name)
	}
}
