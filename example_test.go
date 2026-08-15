package ynabmcp_test

import (
	"context"
	"fmt"
	"log"

	ynabmcp "github.com/justinswe/ynab-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ExampleNew lists account balances from an ordinary Go program.
func ExampleNew() {
	client, err := ynabmcp.New(ynabmcp.Options{
		AccessToken: "the-access-token",
		BudgetID:    "9c07274b-8e3f-4d1f-a2c9-example", // optional: pin every call to one budget
	})
	if err != nil {
		log.Fatal(err)
	}
	accounts, err := client.ListAccounts(context.Background(), ynabmcp.ListAccountsInput{})
	if err != nil {
		log.Fatal(err)
	}
	for _, account := range accounts.Accounts {
		fmt.Println(account.Name, account.Balance)
	}
}

// ExampleClient_RegisterTools embeds the YNAB tools in a larger MCP server.
func ExampleClient_RegisterTools() {
	client, err := ynabmcp.New(ynabmcp.Options{AccessToken: "the-access-token"})
	if err != nil {
		log.Fatal(err)
	}
	server := mcp.NewServer(&mcp.Implementation{Name: "my-agent", Version: "v1"}, nil)
	// No names registers every tool the Options allow; naming tools exposes a subset.
	if err := client.RegisterTools(server, "list_transactions", "get_month"); err != nil {
		log.Fatal(err)
	}
}
