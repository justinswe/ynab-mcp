package main

import (
	"fmt"
	"strings"

	"github.com/justinswe/std/errors"
	"github.com/spf13/cobra"
)

// serverConfig is the operator configuration for the MCP server.
type serverConfig struct {
	ynabAccessToken string
	ynabBaseURL     string
	budgetID        string
	allowWrite      bool
	port            string
	mcpAuthToken    string
}

func newRootCommand() *cobra.Command {
	var cfg serverConfig
	command := &cobra.Command{
		Use:   serviceName,
		Short: "Serves YNAB budgeting reads and writes as MCP tools over stateless HTTP",
		RunE:  func(cmd *cobra.Command, _ []string) error { return runServer(cmd, cfg) },
	}
	flags := command.Flags()
	flags.StringVar(&cfg.ynabAccessToken, "ynab-access-token", cfg.ynabAccessToken,
		"YNAB personal access token; empty serves in passthrough mode where each caller presents their own token")
	flags.StringVar(&cfg.ynabBaseURL, "ynab-base-url", cfg.ynabBaseURL, "Override the YNAB API endpoint; empty uses production")
	// Scoping to one budget lets every tool treat budget_id as optional, which removes
	// a required argument the agent would otherwise have to look up before each call.
	flags.StringVar(&cfg.budgetID, "budget-id", cfg.budgetID, "Restrict every tool to this YNAB budget; empty exposes all budgets the token can read")
	// Writes change financial records, so the tools are withheld until an operator asks.
	flags.BoolVar(&cfg.allowWrite, "allow-write", cfg.allowWrite, "Expose the create_transaction and update_transaction tools")
	flags.StringVar(&cfg.port, "port", "8080", "TCP port the HTTP server listens on")
	// The fixed-mode server proxies a YNAB credential, so anything reachable over
	// the network should have to present a secret of its own.
	flags.StringVar(&cfg.mcpAuthToken, "mcp-auth-token", cfg.mcpAuthToken, "Require this bearer token on MCP requests; only valid with --ynab-access-token")

	command.AddCommand(&cobra.Command{
		Use:   "version",
		Short: "Prints the build version",
		Run:   func(*cobra.Command, []string) { fmt.Println(serviceName, version) },
	})
	return command
}

// passthrough reports whether callers bring their own YNAB tokens.
func (cfg serverConfig) passthrough() bool { return cfg.ynabAccessToken == "" }

// validated returns the configuration with trimmed values, or an error.
func (cfg serverConfig) validated() (serverConfig, error) {
	cfg.ynabAccessToken = strings.TrimSpace(cfg.ynabAccessToken)
	cfg.ynabBaseURL = strings.TrimSpace(cfg.ynabBaseURL)
	cfg.budgetID = strings.TrimSpace(cfg.budgetID)
	cfg.mcpAuthToken = strings.TrimSpace(cfg.mcpAuthToken)
	cfg.port = strings.TrimSpace(cfg.port)
	// In passthrough mode the caller's YNAB token is the credential, and there
	// is only one Authorization header to carry it.
	if cfg.passthrough() && cfg.mcpAuthToken != "" {
		return cfg, errors.New("--mcp-auth-token requires --ynab-access-token; passthrough callers authenticate with their own YNAB token")
	}
	if cfg.port == "" {
		return cfg, errors.New("--port is required")
	}
	return cfg, nil
}
