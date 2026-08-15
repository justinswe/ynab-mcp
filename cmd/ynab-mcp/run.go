package main

import (
	"context"
	"crypto/subtle"
	"net/http"
	"strings"
	"time"

	"github.com/justinswe/std/errors"
	ynabmcp "github.com/justinswe/ynab-mcp"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/spf13/cobra"
	"go.uber.org/zap"
)

const (
	// shutdownTimeout bounds the graceful stop of the HTTP server.
	shutdownTimeout = 5 * time.Second
	// maxRequestBytes caps request bodies; tool calls are small JSON-RPC frames.
	maxRequestBytes = 1 << 20
)

// runServer serves the YNAB tools over a stateless streamable HTTP transport.
func runServer(cmd *cobra.Command, cfg serverConfig) error {
	cfg, err := cfg.validated()
	if err != nil {
		return err
	}
	ctx := cmd.Context()
	handler, err := newHandler(ctx, cfg)
	if err != nil {
		return err
	}
	return serve(ctx, ":"+cfg.port, handler)
}

// newHandler assembles the HTTP handler for the configured auth mode.
func newHandler(ctx context.Context, cfg serverConfig) (http.Handler, error) {
	if cfg.passthrough() {
		zap.L().Info("Serving YNAB MCP tools in passthrough mode over stateless HTTP",
			zap.String("port", cfg.port), zap.Bool("allow_write", cfg.allowWrite))
		return passthroughHandler(cfg), nil
	}
	client, err := ynabmcp.New(ynabmcp.Options{
		AccessToken: cfg.ynabAccessToken,
		BudgetID:    cfg.budgetID,
		AllowWrite:  cfg.allowWrite,
		BaseURL:     cfg.ynabBaseURL,
	})
	if err != nil {
		return nil, err
	}
	if err := client.CheckToken(ctx); err != nil {
		return nil, err
	}
	server, err := newMCPServer(client)
	if err != nil {
		return nil, err
	}
	zap.L().Info("Serving YNAB MCP tools over stateless HTTP",
		zap.String("port", cfg.port), zap.String("budget_id", cfg.budgetID), zap.Bool("allow_write", cfg.allowWrite))
	return fixedHandler(server, cfg.mcpAuthToken), nil
}

// newMCPServer builds an MCP server exposing the client's tools.
func newMCPServer(client *ynabmcp.Client) (*mcp.Server, error) {
	server := mcp.NewServer(&mcp.Implementation{Name: serviceName, Version: version}, nil)
	if err := client.RegisterTools(server); err != nil {
		return nil, err
	}
	return server, nil
}

// fixedHandler serves one operator-configured client behind an optional shared secret.
func fixedHandler(server *mcp.Server, authToken string) http.Handler {
	return newMux(bearerAuth(authToken, streamableHandler(func(*http.Request) *mcp.Server { return server })))
}

// passthroughHandler serves a per-request client built from each caller's own YNAB token.
func passthroughHandler(cfg serverConfig) http.Handler {
	return newMux(requireBearer(streamableHandler(passthroughServer(cfg))))
}

// passthroughServer builds the per-request MCP server for passthrough mode.
// ponytail: rebuilds tool schemas per request; add a per-token LRU if profiling shows it hot.
func passthroughServer(cfg serverConfig) func(*http.Request) *mcp.Server {
	return func(r *http.Request) *mcp.Server {
		// requireBearer already rejected absent tokens; a nil return here is a
		// backstop the SDK answers with 400.
		token, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok {
			return nil
		}
		client, err := ynabmcp.New(ynabmcp.Options{
			AccessToken: token,
			BudgetID:    cfg.budgetID,
			AllowWrite:  cfg.allowWrite,
			BaseURL:     cfg.ynabBaseURL,
		})
		if err != nil {
			return nil
		}
		server, err := newMCPServer(client)
		if err != nil {
			return nil
		}
		return server
	}
}

// streamableHandler serves MCP in stateless mode so any replica can answer any request.
func streamableHandler(getServer func(*http.Request) *mcp.Server) http.Handler {
	return mcp.NewStreamableHTTPHandler(getServer, &mcp.StreamableHTTPOptions{Stateless: true})
}

// newMux routes the health probe and the MCP endpoint with request-size capping.
func newMux(mcpHandler http.Handler) http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	// The SDK stamps Cache-Control: no-cache on /mcp responses, and authorized
	// POST responses carry no freshness info, so intermediaries never reuse them.
	mux.Handle("/mcp", mcpHandler)
	return http.MaxBytesHandler(mux, maxRequestBytes)
}

// bearerToken extracts the credential from an Authorization header, rejecting other schemes.
func bearerToken(header string) (string, bool) {
	// The scheme is case-insensitive per RFC 7235, and a scheme-less header is
	// rejected rather than treated as a bare token.
	scheme, token, found := strings.Cut(header, " ")
	if !found || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// bearerAuth requires the configured shared secret on every request; empty disables the check.
func bearerAuth(token string, next http.Handler) http.Handler {
	if token == "" {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		presented, ok := bearerToken(r.Header.Get("Authorization"))
		if !ok || subtle.ConstantTimeCompare([]byte(presented), []byte(token)) != 1 {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// requireBearer demands that some bearer token be present; YNAB judges its validity.
func requireBearer(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := bearerToken(r.Header.Get("Authorization")); !ok {
			unauthorized(w)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// unauthorized answers 401 with the challenge clients use to discover bearer auth.
func unauthorized(w http.ResponseWriter) {
	w.Header().Set("WWW-Authenticate", "Bearer")
	http.Error(w, "unauthorized", http.StatusUnauthorized)
}

// serve runs the HTTP server until ctx is canceled, then stops it gracefully.
func serve(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       30 * time.Second,
		// Stateless responses are single short-lived POSTs, so a minute
		// comfortably covers the 30s upstream cap.
		WriteTimeout: 60 * time.Second,
		IdleTimeout:  120 * time.Second,
	}
	served := make(chan error, 1)
	go func() {
		err := server.ListenAndServe()
		if errors.Is(err, http.ErrServerClosed) {
			err = nil
		}
		served <- err
	}()
	select {
	case err := <-served:
		return errors.Wrap(err, "serve HTTP")
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return errors.Wrap(err, "shut down HTTP server")
	}
	return <-served
}
