package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/config"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/logging"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/repository"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/service"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/sqliteutil"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/tokens"
)

func TestBrowserFlowHappyPath(t *testing.T) {
	t.Parallel()

	handler, accessURL := newTestHandler(t)

	exchangeBody := bytes.NewBufferString(`{"url":"` + accessURL + `"}`)
	exchangeReq := httptest.NewRequest(http.MethodPost, "/browser/exchange-link", exchangeBody)
	exchangeReq.Header.Set("Content-Type", "application/json")
	exchangeRes := httptest.NewRecorder()
	handler.ServeHTTP(exchangeRes, exchangeReq)

	if exchangeRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", exchangeRes.Code, exchangeRes.Body.String())
	}

	var exchangePayload struct {
		SessionToken string `json:"session_token"`
		DefaultNode  string `json:"default_node_id"`
	}
	if err := json.Unmarshal(exchangeRes.Body.Bytes(), &exchangePayload); err != nil {
		t.Fatalf("unmarshal exchange response: %v", err)
	}
	if exchangePayload.SessionToken == "" {
		t.Fatal("expected session token in exchange response")
	}
	if exchangePayload.DefaultNode != "node-1" {
		t.Fatalf("expected default node node-1, got %s", exchangePayload.DefaultNode)
	}

	sessionReq := httptest.NewRequest(http.MethodGet, "/browser/session", nil)
	sessionReq.Header.Set("Authorization", "Bearer "+exchangePayload.SessionToken)
	sessionRes := httptest.NewRecorder()
	handler.ServeHTTP(sessionRes, sessionReq)
	if sessionRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", sessionRes.Code, sessionRes.Body.String())
	}

	proxyReq := httptest.NewRequest(http.MethodGet, "/browser/proxy-config?node_id=node-1&mode=fixed_servers", nil)
	proxyReq.Header.Set("Authorization", "Bearer "+exchangePayload.SessionToken)
	proxyRes := httptest.NewRecorder()
	handler.ServeHTTP(proxyRes, proxyReq)
	if proxyRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", proxyRes.Code, proxyRes.Body.String())
	}

	var proxyPayload struct {
		Scheme   string `json:"scheme"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := json.Unmarshal(proxyRes.Body.Bytes(), &proxyPayload); err != nil {
		t.Fatalf("unmarshal proxy response: %v", err)
	}
	if proxyPayload.Scheme != "https" {
		t.Fatalf("expected https scheme, got %s", proxyPayload.Scheme)
	}
	if proxyPayload.Username == "" || proxyPayload.Password == "" {
		t.Fatal("expected proxy credentials in proxy response")
	}

	logoutReq := httptest.NewRequest(http.MethodPost, "/browser/logout", nil)
	logoutReq.Header.Set("Authorization", "Bearer "+exchangePayload.SessionToken)
	logoutRes := httptest.NewRecorder()
	handler.ServeHTTP(logoutRes, logoutReq)
	if logoutRes.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", logoutRes.Code, logoutRes.Body.String())
	}

	sessionAfterLogoutReq := httptest.NewRequest(http.MethodGet, "/browser/session", nil)
	sessionAfterLogoutReq.Header.Set("Authorization", "Bearer "+exchangePayload.SessionToken)
	sessionAfterLogoutRes := httptest.NewRecorder()
	handler.ServeHTTP(sessionAfterLogoutRes, sessionAfterLogoutReq)
	if sessionAfterLogoutRes.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401 after logout, got %d: %s", sessionAfterLogoutRes.Code, sessionAfterLogoutRes.Body.String())
	}
}

func TestExchangeLinkInvalid(t *testing.T) {
	t.Parallel()

	handler, _ := newTestHandler(t)

	exchangeBody := bytes.NewBufferString(`{"url":"https://example.com/access/not-a-real-token"}`)
	exchangeReq := httptest.NewRequest(http.MethodPost, "/browser/exchange-link", exchangeBody)
	exchangeReq.Header.Set("Content-Type", "application/json")
	exchangeRes := httptest.NewRecorder()
	handler.ServeHTTP(exchangeRes, exchangeReq)

	if exchangeRes.Code != http.StatusForbidden {
		t.Fatalf("expected 403, got %d: %s", exchangeRes.Code, exchangeRes.Body.String())
	}
}

func newTestHandler(t *testing.T) (http.Handler, string) {
	t.Helper()

	ctx := context.Background()
	dbPath := filepath.Join(t.TempDir(), "test.db")
	db, err := sqliteutil.Open(dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	if err := sqliteutil.Migrate(ctx, db); err != nil {
		t.Fatalf("migrate sqlite: %v", err)
	}

	repo := repository.New(db)
	if err := repo.UpsertNodes(ctx, []repository.Node{{
		ID:          "node-1",
		Name:        "Finland #1",
		Country:     "FI",
		City:        "Helsinki",
		Host:        "proxy.example.com",
		ProxyPort:   1443,
		ProxyScheme: "https",
		Status:      "online",
		IsDefault:   true,
	}}); err != nil {
		t.Fatalf("upsert nodes: %v", err)
	}

	tokenManager := tokens.NewManager("token-pepper", "proxy-pepper")
	rawAccessToken, accessHash, err := tokenManager.NewAccessToken()
	if err != nil {
		t.Fatalf("new access token: %v", err)
	}

	if _, err := repo.CreateAccessLink(ctx, repository.CreateAccessLinkParams{
		TokenHash:      accessHash,
		Label:          "test",
		Source:         "test",
		AllowedNodeIDs: []string{"node-1"},
		DefaultNodeID:  "node-1",
		ExpiresAt:      time.Now().UTC().Add(24 * time.Hour),
	}); err != nil {
		t.Fatalf("create access link: %v", err)
	}

	svc := service.New(
		repo,
		logging.New("error"),
		tokenManager,
		24*time.Hour,
		24*time.Hour,
		[]string{"<local>", "127.0.0.1"},
		"https://access.example.com",
		"local_only",
		nil,
		false,
	)

	server := New(svc, logging.New("error"), config.Config{
		CORSAllowChromeExtensionOrigins: true,
	})
	return server.Handler(), "https://access.example.com/access/" + rawAccessToken
}
