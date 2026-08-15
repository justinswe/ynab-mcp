// Command ynab-mcp serves YNAB budgeting reads and writes over MCP.
package main

import (
	"context"
	"os"

	"github.com/justinswe/std/app"
	"go.uber.org/zap"
)

const serviceName = "ynab-mcp"

var version = "development"

func main() {
	if err := app.RunCobraCommand(context.Background(), newRootCommand()); err != nil {
		app.L().Error("YNAB MCP server stopped", zap.Error(err))
		os.Exit(1)
	}
}
