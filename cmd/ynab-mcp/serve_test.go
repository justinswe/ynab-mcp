package main

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidated(t *testing.T) {
	tests := []struct {
		name     string
		cfg      serverConfig
		contains string
	}{
		{name: "missing port", cfg: serverConfig{ynabAccessToken: "token", port: " "}, contains: "--port"},
		{name: "auth token without fixed token", cfg: serverConfig{mcpAuthToken: "secret", port: "8080"}, contains: "--mcp-auth-token requires"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := test.cfg.validated()

			require.ErrorContains(t, err, test.contains)
		})
	}
}

func TestValidatedTrims(t *testing.T) {
	cfg, err := serverConfig{
		ynabAccessToken: " token ",
		budgetID:        " b1 ",
		mcpAuthToken:    " secret ",
		port:            " 8080 ",
	}.validated()

	require.NoError(t, err)
	assert.Equal(t, "token", cfg.ynabAccessToken)
	assert.Equal(t, "b1", cfg.budgetID)
	assert.Equal(t, "secret", cfg.mcpAuthToken)
	assert.Equal(t, "8080", cfg.port)
	assert.False(t, cfg.passthrough())
}

func TestValidatedAllowsPassthrough(t *testing.T) {
	cfg, err := serverConfig{port: "8080"}.validated()

	require.NoError(t, err)
	assert.True(t, cfg.passthrough())
}

func TestNewRootCommandVersion(t *testing.T) {
	command := newRootCommand()
	command.SetArgs([]string{"version"})

	require.NoError(t, command.Execute())
}

func TestRootCommandRejectsAuthTokenInPassthrough(t *testing.T) {
	command := newRootCommand()
	command.SetArgs([]string{"--mcp-auth-token", "secret"})
	command.SilenceUsage, command.SilenceErrors = true, true

	require.ErrorContains(t, command.Execute(), "--mcp-auth-token requires")
}

func TestBearerToken(t *testing.T) {
	tests := []struct {
		name   string
		header string
		token  string
		ok     bool
	}{
		{name: "canonical", header: "Bearer secret", token: "secret", ok: true},
		{name: "lowercase scheme", header: "bearer secret", token: "secret", ok: true},
		{name: "uppercase scheme", header: "BEARER secret", token: "secret", ok: true},
		{name: "padded token", header: "Bearer  secret ", token: "secret", ok: true},
		{name: "scheme-less", header: "secret", ok: false},
		{name: "wrong scheme", header: "Basic secret", ok: false},
		{name: "scheme only", header: "Bearer", ok: false},
		{name: "scheme with blank token", header: "Bearer   ", ok: false},
		{name: "empty header", header: "", ok: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			token, ok := bearerToken(test.header)

			assert.Equal(t, test.ok, ok)
			assert.Equal(t, test.token, token)
		})
	}
}

// initializeRequest is a minimal stateless MCP handshake request.
const initializeRequest = `{"jsonrpc":"2.0","id":1,"method":"initialize","params":` +
	`{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"0"}}}`

// postInitialize sends an MCP initialize call to the handler and returns the response.
func postInitialize(t *testing.T, handler http.Handler, authorization string) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(http.MethodPost, "/mcp", strings.NewReader(initializeRequest))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder
}

// newTestServer builds a bare MCP server for handler wiring tests.
func newTestServer() *mcp.Server {
	return mcp.NewServer(&mcp.Implementation{Name: serviceName, Version: "test"}, nil)
}

func TestFixedHandlerServesStatelessMCP(t *testing.T) {
	handler := fixedHandler(newTestServer(), "")

	response := postInitialize(t, handler, "")

	require.Equal(t, http.StatusOK, response.Code)
	assert.Contains(t, response.Body.String(), serviceName)
	// Stateless mode issues no session for the client to carry.
	assert.Empty(t, response.Header().Get("Mcp-Session-Id"))
	// The SDK forbids cache reuse without revalidation on financial responses.
	assert.Contains(t, response.Header().Get("Cache-Control"), "no-cache")
}

func TestFixedHandlerAuthMatrix(t *testing.T) {
	handler := fixedHandler(newTestServer(), "secret")
	tests := []struct {
		name          string
		authorization string
		want          int
	}{
		{name: "missing header", authorization: "", want: http.StatusUnauthorized},
		{name: "wrong token", authorization: "Bearer wrong", want: http.StatusUnauthorized},
		{name: "scheme-less token", authorization: "secret", want: http.StatusUnauthorized},
		{name: "canonical scheme", authorization: "Bearer secret", want: http.StatusOK},
		{name: "lowercase scheme", authorization: "bearer secret", want: http.StatusOK},
		{name: "uppercase scheme", authorization: "BEARER secret", want: http.StatusOK},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			response := postInitialize(t, handler, test.authorization)

			require.Equal(t, test.want, response.Code)
			if test.want == http.StatusUnauthorized {
				assert.Equal(t, "Bearer", response.Header().Get("WWW-Authenticate"))
			}
		})
	}
}

func TestHealthzOpenInBothModes(t *testing.T) {
	for name, handler := range map[string]http.Handler{
		"fixed":       fixedHandler(newTestServer(), "secret"),
		"passthrough": passthroughHandler(serverConfig{}),
	} {
		t.Run(name, func(t *testing.T) {
			request := httptest.NewRequest(http.MethodGet, "/healthz", nil)
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)

			// The probe stays open so orchestrators can check liveness without a token.
			assert.Equal(t, http.StatusOK, recorder.Code)
		})
	}
}

func TestMuxRejectsOversizedBodies(t *testing.T) {
	handler := fixedHandler(newTestServer(), "")
	body := bytes.Repeat([]byte("a"), maxRequestBytes+1)
	request := httptest.NewRequest(http.MethodPost, "/mcp", bytes.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Accept", "application/json, text/event-stream")
	recorder := httptest.NewRecorder()

	handler.ServeHTTP(recorder, request)

	assert.Equal(t, http.StatusRequestEntityTooLarge, recorder.Code)
}

func TestPassthroughRequiresBearer(t *testing.T) {
	handler := passthroughHandler(serverConfig{})

	for name, authorization := range map[string]string{"missing": "", "scheme-less": "sometoken"} {
		t.Run(name, func(t *testing.T) {
			response := postInitialize(t, handler, authorization)

			require.Equal(t, http.StatusUnauthorized, response.Code)
			assert.Equal(t, "Bearer", response.Header().Get("WWW-Authenticate"))
		})
	}
}

// authTransport stamps one bearer token on every outgoing request.
type authTransport struct{ token string }

func (a authTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	cloned := r.Clone(r.Context())
	cloned.Header.Set("Authorization", "Bearer "+a.token)
	return http.DefaultTransport.RoundTrip(cloned)
}

// connectPassthrough opens an MCP session against endpoint using the given YNAB token.
func connectPassthrough(t *testing.T, endpoint, token string) *mcp.ClientSession {
	t.Helper()
	transport := &mcp.StreamableClientTransport{
		Endpoint:             endpoint,
		HTTPClient:           &http.Client{Transport: authTransport{token: token}},
		DisableStandaloneSSE: true,
	}
	session, err := mcp.NewClient(&mcp.Implementation{Name: "tester", Version: "0"}, nil).
		Connect(context.Background(), transport, nil)
	require.NoError(t, err)
	t.Cleanup(func() { _ = session.Close() })
	return session
}

func TestPassthroughForwardsEachCallersToken(t *testing.T) {
	// The fake YNAB echoes the presented token as the budget name, so each
	// session can prove its own token — and only its own — reached upstream.
	fakeYNAB := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r.Header.Get("Authorization"))
		require.True(t, ok)
		require.Equal(t, "/budgets", r.URL.Path)
		fmt.Fprintf(w, `{"data":{"budgets":[{"id":"b1","name":"%s"}]}}`, token)
	}))
	t.Cleanup(fakeYNAB.Close)
	service := httptest.NewServer(passthroughHandler(serverConfig{ynabBaseURL: fakeYNAB.URL}))
	t.Cleanup(service.Close)

	first := connectPassthrough(t, service.URL+"/mcp", "token-one")
	second := connectPassthrough(t, service.URL+"/mcp", "token-two")

	var wg sync.WaitGroup
	for session, want := range map[*mcp.ClientSession]string{first: "token-one", second: "token-two"} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			result, err := session.CallTool(context.Background(), &mcp.CallToolParams{
				Name: "list_budgets", Arguments: map[string]any{},
			})
			if !assert.NoError(t, err) || !assert.False(t, result.IsError) {
				return
			}
			output, ok := result.StructuredContent.(map[string]any)
			if !assert.True(t, ok) {
				return
			}
			budgets, ok := output["budgets"].([]any)
			if !assert.True(t, ok) || !assert.Len(t, budgets, 1) {
				return
			}
			assert.Equal(t, want, budgets[0].(map[string]any)["name"])
		}()
	}
	wg.Wait()
}

func TestServeStopsGracefullyOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- serve(ctx, "127.0.0.1:0", http.NewServeMux()) }()
	time.Sleep(50 * time.Millisecond)

	cancel()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(2 * time.Second):
		t.Fatal("serve did not stop after cancellation")
	}
}

func TestServeReportsListenErrors(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = listener.Close() }()

	err = serve(context.Background(), listener.Addr().String(), http.NewServeMux())

	require.ErrorContains(t, err, "serve HTTP")
}
