package service

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/redact"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/remna"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/repository"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/tokens"
)

type AppError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Status  int    `json:"-"`
}

func (e *AppError) Error() string {
	return e.Message
}

type AccessNode struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Country     string `json:"country"`
	City        string `json:"city"`
	Host        string `json:"host"`
	ProxyPort   int    `json:"proxy_port"`
	ProxyScheme string `json:"proxy_scheme"`
	SupportsPAC bool   `json:"supports_pac"`
	Status      string `json:"status"`
	LatencyMS   int    `json:"latency_ms"`
}

type ExchangeLinkRequest struct {
	URL       string
	ClientIP  string
	UserAgent string
}

type ExchangeLinkResponse struct {
	SessionToken  string       `json:"session_token"`
	ExpiresAt     string       `json:"expires_at"`
	Nodes         []AccessNode `json:"nodes"`
	DefaultNodeID string       `json:"default_node_id"`
	DefaultMode   string       `json:"default_mode"`
}

type SessionResponse struct {
	OK            bool         `json:"ok"`
	ExpiresAt     string       `json:"expires_at"`
	Nodes         []AccessNode `json:"nodes"`
	DefaultNodeID string       `json:"default_node_id"`
}

type ProxyConfigResponse struct {
	Mode       string   `json:"mode"`
	Host       string   `json:"host"`
	Port       int      `json:"port"`
	Scheme     string   `json:"scheme"`
	Username   string   `json:"username"`
	Password   string   `json:"password"`
	BypassList []string `json:"bypass_list"`
}

type Service struct {
	repo                *repository.Repository
	logger              *slog.Logger
	tokenManager        *tokens.Manager
	sessionTTL          time.Duration
	proxyCredentialTTL  time.Duration
	defaultBypassList   []string
	accessLinkBaseURL   string
	accessSourceMode    string
	remnaClient         *remna.Client
	proxyAllowPrivateIP bool
}

func New(
	repo *repository.Repository,
	logger *slog.Logger,
	tokenManager *tokens.Manager,
	sessionTTL time.Duration,
	proxyCredentialTTL time.Duration,
	defaultBypassList []string,
	accessLinkBaseURL string,
	accessSourceMode string,
	remnaClient *remna.Client,
	proxyAllowPrivateIP bool,
) *Service {
	return &Service{
		repo:                repo,
		logger:              logger,
		tokenManager:        tokenManager,
		sessionTTL:          sessionTTL,
		proxyCredentialTTL:  proxyCredentialTTL,
		defaultBypassList:   append([]string(nil), defaultBypassList...),
		accessLinkBaseURL:   accessLinkBaseURL,
		accessSourceMode:    accessSourceMode,
		remnaClient:         remnaClient,
		proxyAllowPrivateIP: proxyAllowPrivateIP,
	}
}

func (s *Service) ExchangeLink(ctx context.Context, request ExchangeLinkRequest) (*ExchangeLinkResponse, error) {
	switch s.accessSourceMode {
	case "remna_only":
		return s.exchangeRemnaSubscription(ctx, request)
	case "remna_or_local":
		response, err := s.exchangeRemnaSubscription(ctx, request)
		if err == nil {
			return response, nil
		}
		var appErr *AppError
		if !errors.As(err, &appErr) || (appErr.Code != "invalid_subscription_link" && appErr.Code != "remna_subscription_not_found") {
			return nil, err
		}
		return s.exchangeLocalAccessLink(ctx, request)
	default:
		return s.exchangeLocalAccessLink(ctx, request)
	}
}

func (s *Service) exchangeLocalAccessLink(ctx context.Context, request ExchangeLinkRequest) (*ExchangeLinkResponse, error) {
	accessToken, err := extractAccessToken(request.URL)
	if err != nil {
		return nil, &AppError{
			Code:    "invalid_access_link",
			Message: "Access link is invalid.",
			Status:  400,
		}
	}

	link, err := s.repo.FindAccessLinkByTokenHash(ctx, s.tokenManager.HashAccessToken(accessToken))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, &AppError{
				Code:    "access_link_invalid",
				Message: "Access link is invalid or inactive.",
				Status:  403,
			}
		}
		return nil, fmt.Errorf("find access link: %w", err)
	}

	if err := validateAccessLink(link); err != nil {
		return nil, err
	}

	nodes, defaultNodeID, err := s.resolveNodesForAccessLink(ctx, link)
	if err != nil {
		return nil, err
	}

	sessionToken, session, expiresAt, err := s.createBrowserSession(ctx, nodes, repository.CreateSessionParams{
		AccessLinkID:   link.ID,
		SourceType:     "local_access_link",
		SourceRef:      link.ID,
		SelectedNodeID: defaultNodeID,
		DefaultNodeID:  defaultNodeID,
		ClientIP:       request.ClientIP,
		UserAgent:      request.UserAgent,
	})
	if err != nil {
		return nil, fmt.Errorf("create session: %w", err)
	}

	if err := s.repo.TouchAccessLinkExchanged(ctx, link.ID, time.Now().UTC()); err != nil {
		s.logger.Warn("touch access link exchange failed", slog.String("access_link_id", link.ID), slog.Any("error", err))
	}

	s.logger.Info(
		"browser session issued",
		slog.String("access_link", redact.URL(request.URL)),
		slog.String("session_id", session.ID),
		slog.String("client_ip", request.ClientIP),
	)

	return &ExchangeLinkResponse{
		SessionToken:  sessionToken,
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		Nodes:         mapNodes(nodes),
		DefaultNodeID: defaultNodeID,
		DefaultMode:   "fixed_servers",
	}, nil
}

func (s *Service) exchangeRemnaSubscription(ctx context.Context, request ExchangeLinkRequest) (*ExchangeLinkResponse, error) {
	if s.remnaClient == nil {
		return nil, &AppError{
			Code:    "remna_not_configured",
			Message: "Subscription validation is not configured.",
			Status:  503,
		}
	}

	shortUUID, err := extractAccessToken(request.URL)
	if err != nil {
		return nil, &AppError{
			Code:    "invalid_subscription_link",
			Message: "Subscription link is invalid.",
			Status:  400,
		}
	}

	subscription, err := s.remnaClient.GetSubscriptionByShortUUID(ctx, shortUUID)
	if err != nil {
		return nil, mapRemnaError(err)
	}
	if err := validateRemnaSubscription(subscription); err != nil {
		return nil, err
	}

	nodes, defaultNodeID, err := s.resolveAllNodes(ctx)
	if err != nil {
		return nil, err
	}

	sessionToken, _, expiresAt, err := s.createBrowserSession(ctx, nodes, repository.CreateSessionParams{
		SourceType:             "remna_subscription",
		SourceRef:              shortUUID,
		ExternalSubscriptionID: subscription.ShortUUID,
		SelectedNodeID:         defaultNodeID,
		DefaultNodeID:          defaultNodeID,
		ClientIP:               request.ClientIP,
		UserAgent:              request.UserAgent,
	})
	if err != nil {
		return nil, err
	}

	s.logger.Info(
		"browser session issued from remna",
		slog.String("subscription", redact.String(shortUUID)),
		slog.String("client_ip", request.ClientIP),
	)

	return &ExchangeLinkResponse{
		SessionToken:  sessionToken,
		ExpiresAt:     expiresAt.Format(time.RFC3339),
		Nodes:         mapNodes(nodes),
		DefaultNodeID: defaultNodeID,
		DefaultMode:   "fixed_servers",
	}, nil
}

func (s *Service) ValidateSession(ctx context.Context, rawSessionToken string) (*SessionResponse, *repository.SessionBundle, error) {
	bundle, err := s.loadAndValidateSession(ctx, rawSessionToken)
	if err != nil {
		return nil, nil, err
	}

	nodes, defaultNodeID, err := s.resolveNodesForSession(ctx, bundle)
	if err != nil {
		return nil, nil, err
	}

	if err := s.repo.TouchSessionSeen(ctx, bundle.Session.ID, time.Now().UTC()); err != nil {
		s.logger.Warn("touch session seen failed", slog.String("session_id", bundle.Session.ID), slog.Any("error", err))
	}

	return &SessionResponse{
		OK:            true,
		ExpiresAt:     bundle.Session.ExpiresAt.UTC().Format(time.RFC3339),
		Nodes:         mapNodes(nodes),
		DefaultNodeID: defaultNodeID,
	}, bundle, nil
}

func (s *Service) GetProxyConfig(
	ctx context.Context,
	rawSessionToken string,
	nodeID string,
	mode string,
) (*ProxyConfigResponse, error) {
	if mode == "" {
		mode = "fixed_servers"
	}
	if mode != "fixed_servers" {
		return nil, &AppError{
			Code:    "mode_not_supported",
			Message: "Only fixed proxy mode is enabled.",
			Status:  400,
		}
	}

	_, bundle, err := s.ValidateSession(ctx, rawSessionToken)
	if err != nil {
		return nil, err
	}

	nodes, defaultNodeID, err := s.resolveNodesForSession(ctx, bundle)
	if err != nil {
		return nil, err
	}

	if nodeID == "" {
		nodeID = bundle.Session.SelectedNodeID
	}
	if nodeID == "" {
		nodeID = defaultNodeID
	}

	node, err := pickNode(nodes, nodeID)
	if err != nil {
		return nil, &AppError{
			Code:    "node_not_found",
			Message: "Proxy node is not available.",
			Status:  404,
		}
	}
	if node.Status != "online" {
		return nil, &AppError{
			Code:    "node_offline",
			Message: "Proxy node is offline.",
			Status:  409,
		}
	}

	credential, err := s.ensureProxyCredential(ctx, &bundle.Session, node.ID)
	if err != nil {
		return nil, fmt.Errorf("ensure proxy credential: %w", err)
	}

	password := s.tokenManager.DeriveProxyPassword(bundle.Session.ID, node.ID, credential.PasswordVersion)
	s.logger.Info(
		"proxy config issued",
		slog.String("session_id", bundle.Session.ID),
		slog.String("node_id", node.ID),
		slog.String("proxy_username", redact.String(credential.Username)),
	)

	return &ProxyConfigResponse{
		Mode:       "fixed_servers",
		Host:       node.Host,
		Port:       node.ProxyPort,
		Scheme:     node.ProxyScheme,
		Username:   credential.Username,
		Password:   password,
		BypassList: append([]string(nil), s.defaultBypassList...),
	}, nil
}

func (s *Service) Logout(ctx context.Context, rawSessionToken string) error {
	_, bundle, err := s.ValidateSession(ctx, rawSessionToken)
	if err != nil {
		return err
	}

	if err := s.repo.RevokeSession(ctx, bundle.Session.ID, time.Now().UTC()); err != nil {
		return fmt.Errorf("revoke session: %w", err)
	}

	s.logger.Info("browser session revoked", slog.String("session_id", bundle.Session.ID))
	return nil
}

func (s *Service) ValidateProxyCredentials(ctx context.Context, username, password string) (bool, error) {
	bundle, err := s.repo.GetCredentialBundleByUsername(ctx, username)
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return false, nil
		}
		return false, err
	}

	now := time.Now().UTC()
	if bundle.Credential.RevokedAt != nil || bundle.Session.RevokedAt != nil {
		return false, nil
	}
	if bundle.AccessLink != nil && bundle.AccessLink.RevokedAt != nil {
		return false, nil
	}
	if bundle.Credential.ExpiresAt.Before(now) || bundle.Session.ExpiresAt.Before(now) {
		return false, nil
	}
	if bundle.AccessLink != nil && bundle.AccessLink.ExpiresAt.Before(now) {
		return false, nil
	}
	if bundle.Session.Status != "active" || bundle.Node.Status != "online" {
		return false, nil
	}
	if bundle.AccessLink != nil && bundle.AccessLink.Status != "active" {
		return false, nil
	}

	expected := s.tokenManager.DeriveProxyPassword(bundle.Session.ID, bundle.Node.ID, bundle.Credential.PasswordVersion)
	if subtle.ConstantTimeCompare([]byte(expected), []byte(password)) != 1 {
		return false, nil
	}

	return true, nil
}

func (s *Service) AllowProxyDestination(host string) bool {
	if s.proxyAllowPrivateIP {
		return true
	}

	if ip := net.ParseIP(host); ip != nil {
		return !isPrivateIP(ip)
	}
	return true
}

func (s *Service) AccessLinkBaseURL() string {
	return s.accessLinkBaseURL
}

func (s *Service) ensureProxyCredential(
	ctx context.Context,
	session *repository.BrowserSession,
	nodeID string,
) (*repository.ProxyCredential, error) {
	credential, err := s.repo.GetProxyCredentialBySessionAndNode(ctx, session.ID, nodeID)
	if err == nil {
		if credential.RevokedAt == nil && credential.ExpiresAt.After(time.Now().UTC()) {
			return credential, nil
		}
		username, genErr := s.tokenManager.NewProxyUsername()
		if genErr != nil {
			return nil, genErr
		}
		return s.repo.RotateProxyCredential(
			ctx,
			credential.ID,
			username,
			credential.PasswordVersion+1,
			time.Now().UTC().Add(s.proxyCredentialTTL),
		)
	}
	if err != nil && !errors.Is(err, repository.ErrNotFound) {
		return nil, err
	}

	username, err := s.tokenManager.NewProxyUsername()
	if err != nil {
		return nil, err
	}
	return s.repo.CreateProxyCredential(
		ctx,
		session.ID,
		nodeID,
		username,
		1,
		time.Now().UTC().Add(s.proxyCredentialTTL),
	)
}

func (s *Service) loadAndValidateSession(ctx context.Context, rawSessionToken string) (*repository.SessionBundle, error) {
	rawSessionToken = strings.TrimSpace(rawSessionToken)
	if rawSessionToken == "" {
		return nil, &AppError{
			Code:    "session_missing",
			Message: "Session token is missing.",
			Status:  401,
		}
	}

	bundle, err := s.repo.GetSessionBundleByTokenHash(ctx, s.tokenManager.HashSessionToken(rawSessionToken))
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return nil, &AppError{
				Code:    "session_invalid",
				Message: "Session token is invalid.",
				Status:  401,
			}
		}
		return nil, fmt.Errorf("find session: %w", err)
	}

	now := time.Now().UTC()
	if bundle.Session.RevokedAt != nil || bundle.Session.Status != "active" {
		return nil, &AppError{
			Code:    "session_revoked",
			Message: "Session has been revoked.",
			Status:  401,
		}
	}
	if bundle.Session.ExpiresAt.Before(now) {
		return nil, &AppError{
			Code:    "session_expired",
			Message: "Session expired.",
			Status:  401,
		}
	}
	switch bundle.Session.SourceType {
	case "remna_subscription":
		if err := s.validateRemnaSessionSource(ctx, &bundle.Session); err != nil {
			return nil, err
		}
	default:
		if bundle.AccessLink == nil {
			return nil, &AppError{
				Code:    "access_source_missing",
				Message: "Access source is missing.",
				Status:  401,
			}
		}
		if err := validateAccessLink(bundle.AccessLink); err != nil {
			return nil, err
		}
	}
	return bundle, nil
}

func (s *Service) createBrowserSession(
	ctx context.Context,
	nodes []repository.Node,
	params repository.CreateSessionParams,
) (string, *repository.BrowserSession, time.Time, error) {
	sessionToken, sessionHash, err := s.tokenManager.NewSessionToken()
	if err != nil {
		return "", nil, time.Time{}, fmt.Errorf("generate session token: %w", err)
	}

	expiresAt := time.Now().UTC().Add(s.sessionTTL)
	availableNodeIDs := make([]string, 0, len(nodes))
	for _, node := range nodes {
		availableNodeIDs = append(availableNodeIDs, node.ID)
	}
	params.SessionTokenHash = sessionHash
	params.AvailableNodeIDs = availableNodeIDs
	params.ExpiresAt = expiresAt

	session, err := s.repo.CreateSession(ctx, params)
	if err != nil {
		return "", nil, time.Time{}, fmt.Errorf("create session: %w", err)
	}

	if _, err := s.ensureProxyCredential(ctx, session, params.DefaultNodeID); err != nil {
		return "", nil, time.Time{}, fmt.Errorf("ensure proxy credential: %w", err)
	}

	return sessionToken, session, expiresAt, nil
}

func (s *Service) resolveNodesForAccessLink(ctx context.Context, link *repository.AccessLink) ([]repository.Node, string, error) {
	if link == nil {
		return s.resolveAllNodes(ctx)
	}

	nodes, err := s.repo.ListNodes(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list nodes: %w", err)
	}
	if len(nodes) == 0 {
		return nil, "", &AppError{
			Code:    "no_nodes_available",
			Message: "No proxy nodes are configured.",
			Status:  503,
		}
	}

	allowedSet := make(map[string]struct{})
	if len(link.AllowedNodeIDs) > 0 {
		for _, id := range link.AllowedNodeIDs {
			allowedSet[id] = struct{}{}
		}
	}

	filtered := make([]repository.Node, 0, len(nodes))
	for _, node := range nodes {
		if len(allowedSet) > 0 {
			if _, ok := allowedSet[node.ID]; !ok {
				continue
			}
		}
		filtered = append(filtered, node)
	}
	if len(filtered) == 0 {
		return nil, "", &AppError{
			Code:    "no_nodes_available",
			Message: "No proxy nodes are available for this access link.",
			Status:  403,
		}
	}

	defaultNodeID := chooseDefaultNode(link.DefaultNodeID, filtered)
	return filtered, defaultNodeID, nil
}

func (s *Service) resolveNodesForSession(ctx context.Context, bundle *repository.SessionBundle) ([]repository.Node, string, error) {
	if bundle.Session.SourceType == "remna_subscription" {
		return s.resolveAllNodes(ctx)
	}
	return s.resolveNodesForAccessLink(ctx, bundle.AccessLink)
}

func (s *Service) resolveAllNodes(ctx context.Context) ([]repository.Node, string, error) {
	nodes, err := s.repo.ListNodes(ctx)
	if err != nil {
		return nil, "", fmt.Errorf("list nodes: %w", err)
	}
	if len(nodes) == 0 {
		return nil, "", &AppError{
			Code:    "no_nodes_available",
			Message: "No proxy nodes are configured.",
			Status:  503,
		}
	}
	return nodes, chooseDefaultNode("", nodes), nil
}

func (s *Service) validateRemnaSessionSource(ctx context.Context, session *repository.BrowserSession) error {
	if s.remnaClient == nil {
		return &AppError{
			Code:    "remna_not_configured",
			Message: "Subscription validation is not configured.",
			Status:  503,
		}
	}
	subscription, err := s.remnaClient.GetSubscriptionByShortUUID(ctx, session.SourceRef)
	if err != nil {
		return mapRemnaError(err)
	}
	return validateRemnaSubscription(subscription)
}

func validateRemnaSubscription(subscription *remna.Subscription) error {
	if subscription == nil {
		return &AppError{
			Code:    "remna_subscription_not_found",
			Message: "Subscription was not found.",
			Status:  403,
		}
	}
	if !subscription.IsActive || strings.EqualFold(subscription.Status, "DISABLED") || strings.EqualFold(subscription.Status, "REVOKED") {
		return &AppError{
			Code:    "remna_subscription_disabled",
			Message: "Subscription is disabled or revoked.",
			Status:  403,
		}
	}
	if subscription.ExpiresAt != nil && subscription.ExpiresAt.Before(time.Now().UTC()) {
		return &AppError{
			Code:    "remna_subscription_expired",
			Message: "Subscription has expired.",
			Status:  403,
		}
	}
	return nil
}

func mapRemnaError(err error) error {
	switch {
	case errors.Is(err, remna.ErrNotFound):
		return &AppError{
			Code:    "remna_subscription_not_found",
			Message: "Subscription was not found.",
			Status:  403,
		}
	case errors.Is(err, remna.ErrUnauthorized):
		return &AppError{
			Code:    "remna_auth_failed",
			Message: "Subscription validation service auth failed.",
			Status:  502,
		}
	case errors.Is(err, remna.ErrUnavailable):
		return &AppError{
			Code:    "remna_unavailable",
			Message: "Subscription validation service is unavailable.",
			Status:  503,
		}
	default:
		return fmt.Errorf("remna validation: %w", err)
	}
}

func extractAccessToken(raw string) (string, error) {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return "", err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return "", errors.New("missing scheme or host")
	}
	if token := strings.TrimSpace(parsed.Query().Get("token")); token != "" {
		return token, nil
	}

	segments := strings.Split(strings.Trim(parsed.Path, "/"), "/")
	for idx := len(segments) - 1; idx >= 0; idx-- {
		if strings.TrimSpace(segments[idx]) != "" {
			return segments[idx], nil
		}
	}
	return "", errors.New("token not found in access link")
}

func validateAccessLink(link *repository.AccessLink) error {
	now := time.Now().UTC()
	if link.RevokedAt != nil || link.Status != "active" {
		return &AppError{
			Code:    "access_link_revoked",
			Message: "Access link is not active.",
			Status:  403,
		}
	}
	if link.ExpiresAt.Before(now) {
		return &AppError{
			Code:    "access_link_expired",
			Message: "Access link has expired.",
			Status:  403,
		}
	}
	return nil
}

func chooseDefaultNode(preferred string, nodes []repository.Node) string {
	if preferred != "" {
		for _, node := range nodes {
			if node.ID == preferred {
				return node.ID
			}
		}
	}
	for _, node := range nodes {
		if node.IsDefault {
			return node.ID
		}
	}
	for _, node := range nodes {
		if node.Status == "online" {
			return node.ID
		}
	}
	return nodes[0].ID
}

func pickNode(nodes []repository.Node, nodeID string) (repository.Node, error) {
	for _, node := range nodes {
		if node.ID == nodeID {
			return node, nil
		}
	}
	return repository.Node{}, errors.New("node not found")
}

func mapNodes(nodes []repository.Node) []AccessNode {
	items := make([]AccessNode, 0, len(nodes))
	for _, node := range nodes {
		items = append(items, AccessNode{
			ID:          node.ID,
			Name:        node.Name,
			Country:     node.Country,
			City:        node.City,
			Host:        node.Host,
			ProxyPort:   node.ProxyPort,
			ProxyScheme: node.ProxyScheme,
			SupportsPAC: node.SupportsPAC,
			Status:      node.Status,
			LatencyMS:   node.LatencyMS,
		})
	}
	return items
}

func isPrivateIP(ip net.IP) bool {
	privateBlocks := []string{
		"127.0.0.0/8",
		"10.0.0.0/8",
		"172.16.0.0/12",
		"192.168.0.0/16",
		"169.254.0.0/16",
		"::1/128",
		"fc00::/7",
		"fe80::/10",
	}
	for _, cidr := range privateBlocks {
		_, block, _ := net.ParseCIDR(cidr)
		if block.Contains(ip) {
			return true
		}
	}
	return false
}
