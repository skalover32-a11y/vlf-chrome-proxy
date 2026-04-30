package repository

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
)

var ErrNotFound = errors.New("not found")

type Node struct {
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
	IsDefault   bool   `json:"is_default"`
}

type AccessLink struct {
	ID              string
	TokenHash       string
	Label           string
	Source          string
	Status          string
	AllowedNodeIDs  []string
	DefaultNodeID   string
	ExpiresAt       time.Time
	LastExchangedAt *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type BrowserSession struct {
	ID                     string
	AccessLinkID           string
	SourceType             string
	SourceRef              string
	ExternalSubscriptionID string
	SessionTokenHash       string
	SelectedNodeID         string
	DefaultNodeID          string
	AvailableNodeIDs       []string
	Status                 string
	ExpiresAt              time.Time
	LastSeenAt             *time.Time
	RevokedAt              *time.Time
	ClientIP               string
	UserAgent              string
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

type ProxyCredential struct {
	ID              string
	SessionID       string
	NodeID          string
	Username        string
	PasswordVersion int
	ExpiresAt       time.Time
	LastUsedAt      *time.Time
	RevokedAt       *time.Time
	CreatedAt       time.Time
	UpdatedAt       time.Time
}

type SessionBundle struct {
	Session    BrowserSession
	AccessLink *AccessLink
}

type CredentialBundle struct {
	Credential ProxyCredential
	Session    BrowserSession
	AccessLink *AccessLink
	Node       Node
}

type CreateAccessLinkParams struct {
	TokenHash      string
	Label          string
	Source         string
	AllowedNodeIDs []string
	DefaultNodeID  string
	ExpiresAt      time.Time
}

type CreateSessionParams struct {
	AccessLinkID           string
	SourceType             string
	SourceRef              string
	ExternalSubscriptionID string
	SessionTokenHash       string
	SelectedNodeID         string
	DefaultNodeID          string
	AvailableNodeIDs       []string
	ExpiresAt              time.Time
	ClientIP               string
	UserAgent              string
}

type Repository struct {
	db *sql.DB
}

func New(db *sql.DB) *Repository {
	return &Repository{db: db}
}

func (r *Repository) CountAccessLinks(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM access_links`).Scan(&count); err != nil {
		return 0, err
	}
	return count, nil
}

func (r *Repository) CreateAccessLink(ctx context.Context, params CreateAccessLinkParams) (*AccessLink, error) {
	now := time.Now().UTC()
	link := &AccessLink{
		ID:             uuid.NewString(),
		TokenHash:      params.TokenHash,
		Label:          strings.TrimSpace(params.Label),
		Source:         fallback(params.Source, "manual"),
		Status:         "active",
		AllowedNodeIDs: append([]string(nil), params.AllowedNodeIDs...),
		DefaultNodeID:  strings.TrimSpace(params.DefaultNodeID),
		ExpiresAt:      params.ExpiresAt.UTC(),
		CreatedAt:      now,
		UpdatedAt:      now,
	}

	allowedJSON, err := marshalStringSlice(link.AllowedNodeIDs)
	if err != nil {
		return nil, err
	}

	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO access_links (
			id, token_hash, label, source, status, allowed_node_ids, default_node_id,
			expires_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		link.ID,
		link.TokenHash,
		link.Label,
		link.Source,
		link.Status,
		allowedJSON,
		nullString(link.DefaultNodeID),
		timeString(link.ExpiresAt),
		timeString(link.CreatedAt),
		timeString(link.UpdatedAt),
	)
	if err != nil {
		return nil, err
	}

	return link, nil
}

func (r *Repository) FindAccessLinkByTokenHash(ctx context.Context, tokenHash string) (*AccessLink, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, token_hash, label, source, status, allowed_node_ids, default_node_id,
		       expires_at, last_exchanged_at, revoked_at, created_at, updated_at
		FROM access_links
		WHERE token_hash = ?
	`, tokenHash)

	var (
		link        AccessLink
		allowedJSON string
		defaultNode sql.NullString
		expiresAt   string
		lastEx      sql.NullString
		revoked     sql.NullString
		createdAt   string
		updatedAt   string
	)
	if err := row.Scan(
		&link.ID,
		&link.TokenHash,
		&link.Label,
		&link.Source,
		&link.Status,
		&allowedJSON,
		&defaultNode,
		&expiresAt,
		&lastEx,
		&revoked,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	allowed, err := unmarshalStringSlice(allowedJSON)
	if err != nil {
		return nil, err
	}
	link.AllowedNodeIDs = allowed
	link.DefaultNodeID = defaultNode.String
	link.ExpiresAt = mustParseTime(expiresAt)
	link.LastExchangedAt = parseNullTime(lastEx)
	link.RevokedAt = parseNullTime(revoked)
	link.CreatedAt = mustParseTime(createdAt)
	link.UpdatedAt = mustParseTime(updatedAt)
	return &link, nil
}

func (r *Repository) TouchAccessLinkExchanged(ctx context.Context, id string, when time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE access_links SET last_exchanged_at = ?, updated_at = ? WHERE id = ?`,
		timeString(when),
		timeString(when),
		id,
	)
	return err
}

func (r *Repository) UpsertNodes(ctx context.Context, nodes []Node) error {
	now := time.Now().UTC()
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(ctx, `UPDATE nodes SET is_default = 0`); err != nil {
		return err
	}

	stmt := `
		INSERT INTO nodes (
			id, name, country, city, host, proxy_port, proxy_scheme, supports_pac,
			status, latency_ms, is_default, metadata_json, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, '{}', ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name = excluded.name,
			country = excluded.country,
			city = excluded.city,
			host = excluded.host,
			proxy_port = excluded.proxy_port,
			proxy_scheme = excluded.proxy_scheme,
			supports_pac = excluded.supports_pac,
			status = excluded.status,
			latency_ms = excluded.latency_ms,
			is_default = excluded.is_default,
			updated_at = excluded.updated_at
	`

	for _, node := range nodes {
		_, err := tx.ExecContext(
			ctx,
			stmt,
			node.ID,
			node.Name,
			node.Country,
			node.City,
			node.Host,
			node.ProxyPort,
			node.ProxyScheme,
			boolToInt(node.SupportsPAC),
			node.Status,
			node.LatencyMS,
			boolToInt(node.IsDefault),
			timeString(now),
			timeString(now),
		)
		if err != nil {
			return fmt.Errorf("upsert node %s: %w", node.ID, err)
		}
	}

	return tx.Commit()
}

func (r *Repository) ListNodes(ctx context.Context) ([]Node, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT id, name, country, city, host, proxy_port, proxy_scheme,
		       supports_pac, status, latency_ms, is_default
		FROM nodes
		ORDER BY is_default DESC, name ASC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var nodes []Node
	for rows.Next() {
		var node Node
		var supportsPAC, isDefault int
		if err := rows.Scan(
			&node.ID,
			&node.Name,
			&node.Country,
			&node.City,
			&node.Host,
			&node.ProxyPort,
			&node.ProxyScheme,
			&supportsPAC,
			&node.Status,
			&node.LatencyMS,
			&isDefault,
		); err != nil {
			return nil, err
		}
		node.SupportsPAC = supportsPAC == 1
		node.IsDefault = isDefault == 1
		nodes = append(nodes, node)
	}

	return nodes, rows.Err()
}

func (r *Repository) CreateSession(ctx context.Context, params CreateSessionParams) (*BrowserSession, error) {
	now := time.Now().UTC()
	availableJSON, err := marshalStringSlice(params.AvailableNodeIDs)
	if err != nil {
		return nil, err
	}

	session := &BrowserSession{
		ID:                     uuid.NewString(),
		AccessLinkID:           params.AccessLinkID,
		SourceType:             fallback(params.SourceType, "local_access_link"),
		SourceRef:              strings.TrimSpace(params.SourceRef),
		ExternalSubscriptionID: strings.TrimSpace(params.ExternalSubscriptionID),
		SessionTokenHash:       params.SessionTokenHash,
		SelectedNodeID:         strings.TrimSpace(params.SelectedNodeID),
		DefaultNodeID:          strings.TrimSpace(params.DefaultNodeID),
		AvailableNodeIDs:       append([]string(nil), params.AvailableNodeIDs...),
		Status:                 "active",
		ExpiresAt:              params.ExpiresAt.UTC(),
		ClientIP:               strings.TrimSpace(params.ClientIP),
		UserAgent:              strings.TrimSpace(params.UserAgent),
		CreatedAt:              now,
		UpdatedAt:              now,
	}

	_, err = r.db.ExecContext(
		ctx,
		`INSERT INTO browser_sessions (
			id, access_link_id, source_type, source_ref, external_subscription_id,
			session_token_hash, selected_node_id, default_node_id,
			available_node_ids, status, expires_at, client_ip, user_agent, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		session.ID,
		nullString(session.AccessLinkID),
		session.SourceType,
		nullString(session.SourceRef),
		nullString(session.ExternalSubscriptionID),
		session.SessionTokenHash,
		nullString(session.SelectedNodeID),
		nullString(session.DefaultNodeID),
		availableJSON,
		session.Status,
		timeString(session.ExpiresAt),
		nullString(session.ClientIP),
		nullString(session.UserAgent),
		timeString(session.CreatedAt),
		timeString(session.UpdatedAt),
	)
	if err != nil {
		return nil, err
	}

	return session, nil
}

func (r *Repository) GetSessionBundleByTokenHash(ctx context.Context, tokenHash string) (*SessionBundle, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			s.id, s.access_link_id, s.source_type, s.source_ref, s.external_subscription_id,
			s.session_token_hash, s.selected_node_id, s.default_node_id,
			s.available_node_ids, s.status, s.expires_at, s.last_seen_at, s.revoked_at,
			s.client_ip, s.user_agent, s.created_at, s.updated_at,
			a.id, a.token_hash, a.label, a.source, a.status, a.allowed_node_ids, a.default_node_id,
			a.expires_at, a.last_exchanged_at, a.revoked_at, a.created_at, a.updated_at
		FROM browser_sessions s
		LEFT JOIN access_links a ON a.id = s.access_link_id
		WHERE s.session_token_hash = ?
	`, tokenHash)

	var (
		session              BrowserSession
		sessionAccessLinkID  sql.NullString
		sessionSourceRef     sql.NullString
		sessionExternalID    sql.NullString
		sessionSelectedNode  sql.NullString
		sessionDefaultNode   sql.NullString
		sessionAvailableJSON string
		sessionExpiresAt     string
		sessionLastSeen      sql.NullString
		sessionRevoked       sql.NullString
		sessionClientIP      sql.NullString
		sessionUserAgent     sql.NullString
		sessionCreatedAt     string
		sessionUpdatedAt     string
		link                 AccessLink
		linkID               sql.NullString
		linkTokenHash        sql.NullString
		linkLabel            sql.NullString
		linkSource           sql.NullString
		linkStatus           sql.NullString
		linkAllowedJSON      sql.NullString
		linkDefaultNode      sql.NullString
		linkExpiresAt        sql.NullString
		linkLastEx           sql.NullString
		linkRevoked          sql.NullString
		linkCreatedAt        sql.NullString
		linkUpdatedAt        sql.NullString
	)

	if err := row.Scan(
		&session.ID,
		&sessionAccessLinkID,
		&session.SourceType,
		&sessionSourceRef,
		&sessionExternalID,
		&session.SessionTokenHash,
		&sessionSelectedNode,
		&sessionDefaultNode,
		&sessionAvailableJSON,
		&session.Status,
		&sessionExpiresAt,
		&sessionLastSeen,
		&sessionRevoked,
		&sessionClientIP,
		&sessionUserAgent,
		&sessionCreatedAt,
		&sessionUpdatedAt,
		&linkID,
		&linkTokenHash,
		&linkLabel,
		&linkSource,
		&linkStatus,
		&linkAllowedJSON,
		&linkDefaultNode,
		&linkExpiresAt,
		&linkLastEx,
		&linkRevoked,
		&linkCreatedAt,
		&linkUpdatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	availableNodeIDs, err := unmarshalStringSlice(sessionAvailableJSON)
	if err != nil {
		return nil, err
	}

	session.AccessLinkID = sessionAccessLinkID.String
	session.SourceRef = sessionSourceRef.String
	session.ExternalSubscriptionID = sessionExternalID.String
	session.SelectedNodeID = sessionSelectedNode.String
	session.DefaultNodeID = sessionDefaultNode.String
	session.AvailableNodeIDs = availableNodeIDs
	session.ExpiresAt = mustParseTime(sessionExpiresAt)
	session.LastSeenAt = parseNullTime(sessionLastSeen)
	session.RevokedAt = parseNullTime(sessionRevoked)
	session.ClientIP = sessionClientIP.String
	session.UserAgent = sessionUserAgent.String
	session.CreatedAt = mustParseTime(sessionCreatedAt)
	session.UpdatedAt = mustParseTime(sessionUpdatedAt)

	var accessLink *AccessLink
	if linkID.Valid {
		allowedNodeIDs, err := unmarshalStringSlice(linkAllowedJSON.String)
		if err != nil {
			return nil, err
		}
		link.ID = linkID.String
		link.TokenHash = linkTokenHash.String
		link.Label = linkLabel.String
		link.Source = linkSource.String
		link.Status = linkStatus.String
		link.DefaultNodeID = linkDefaultNode.String
		link.AllowedNodeIDs = allowedNodeIDs
		link.ExpiresAt = mustParseTime(linkExpiresAt.String)
		link.LastExchangedAt = parseNullTime(linkLastEx)
		link.RevokedAt = parseNullTime(linkRevoked)
		link.CreatedAt = mustParseTime(linkCreatedAt.String)
		link.UpdatedAt = mustParseTime(linkUpdatedAt.String)
		accessLink = &link
	}

	return &SessionBundle{Session: session, AccessLink: accessLink}, nil
}

func (r *Repository) TouchSessionSeen(ctx context.Context, id string, when time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE browser_sessions SET last_seen_at = ?, updated_at = ? WHERE id = ?`,
		timeString(when),
		timeString(when),
		id,
	)
	return err
}

func (r *Repository) RevokeSession(ctx context.Context, sessionID string, when time.Time) error {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE browser_sessions SET status = 'revoked', revoked_at = ?, updated_at = ? WHERE id = ?`,
		timeString(when),
		timeString(when),
		sessionID,
	); err != nil {
		return err
	}

	if _, err := tx.ExecContext(
		ctx,
		`UPDATE proxy_credentials SET revoked_at = ?, updated_at = ? WHERE session_id = ?`,
		timeString(when),
		timeString(when),
		sessionID,
	); err != nil {
		return err
	}

	return tx.Commit()
}

func (r *Repository) GetProxyCredentialBySessionAndNode(
	ctx context.Context,
	sessionID string,
	nodeID string,
) (*ProxyCredential, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT id, session_id, node_id, username, password_version, expires_at,
		       last_used_at, revoked_at, created_at, updated_at
		FROM proxy_credentials
		WHERE session_id = ? AND node_id = ?
	`, sessionID, nodeID)
	return scanCredential(row)
}

func (r *Repository) CreateProxyCredential(
	ctx context.Context,
	sessionID string,
	nodeID string,
	username string,
	passwordVersion int,
	expiresAt time.Time,
) (*ProxyCredential, error) {
	now := time.Now().UTC()
	credential := &ProxyCredential{
		ID:              uuid.NewString(),
		SessionID:       sessionID,
		NodeID:          nodeID,
		Username:        username,
		PasswordVersion: passwordVersion,
		ExpiresAt:       expiresAt.UTC(),
		CreatedAt:       now,
		UpdatedAt:       now,
	}

	_, err := r.db.ExecContext(
		ctx,
		`INSERT INTO proxy_credentials (
			id, session_id, node_id, username, password_version, expires_at, created_at, updated_at
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		credential.ID,
		credential.SessionID,
		credential.NodeID,
		credential.Username,
		credential.PasswordVersion,
		timeString(credential.ExpiresAt),
		timeString(credential.CreatedAt),
		timeString(credential.UpdatedAt),
	)
	if err != nil {
		return nil, err
	}

	return credential, nil
}

func (r *Repository) RotateProxyCredential(
	ctx context.Context,
	id string,
	username string,
	passwordVersion int,
	expiresAt time.Time,
) (*ProxyCredential, error) {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE proxy_credentials
		 SET username = ?, password_version = ?, expires_at = ?, revoked_at = NULL, last_used_at = NULL, updated_at = ?
		 WHERE id = ?`,
		username,
		passwordVersion,
		timeString(expiresAt),
		timeString(now),
		id,
	)
	if err != nil {
		return nil, err
	}

	row := r.db.QueryRowContext(ctx, `
		SELECT id, session_id, node_id, username, password_version, expires_at,
		       last_used_at, revoked_at, created_at, updated_at
		FROM proxy_credentials
		WHERE id = ?
	`, id)
	return scanCredential(row)
}

func (r *Repository) GetCredentialBundleByUsername(ctx context.Context, username string) (*CredentialBundle, error) {
	row := r.db.QueryRowContext(ctx, `
		SELECT
			pc.id, pc.session_id, pc.node_id, pc.username, pc.password_version, pc.expires_at,
			pc.last_used_at, pc.revoked_at, pc.created_at, pc.updated_at,
			s.id, s.access_link_id, s.source_type, s.source_ref, s.external_subscription_id,
			s.session_token_hash, s.selected_node_id, s.default_node_id,
			s.available_node_ids, s.status, s.expires_at, s.last_seen_at, s.revoked_at, s.client_ip, s.user_agent,
			s.created_at, s.updated_at,
			a.id, a.token_hash, a.label, a.source, a.status, a.allowed_node_ids, a.default_node_id,
			a.expires_at, a.last_exchanged_at, a.revoked_at, a.created_at, a.updated_at,
			n.id, n.name, n.country, n.city, n.host, n.proxy_port, n.proxy_scheme, n.supports_pac, n.status, n.latency_ms, n.is_default
		FROM proxy_credentials pc
		JOIN browser_sessions s ON s.id = pc.session_id
		LEFT JOIN access_links a ON a.id = s.access_link_id
		JOIN nodes n ON n.id = pc.node_id
		WHERE pc.username = ?
	`, username)

	var (
		bundle                 CredentialBundle
		credExpiresAt          string
		credLastUsed           sql.NullString
		credRevoked            sql.NullString
		credCreatedAt          string
		credUpdatedAt          string
		sessionAccessLinkID    sql.NullString
		sessionSourceRef       sql.NullString
		sessionExternalID      sql.NullString
		sessionSelectedNode    sql.NullString
		sessionDefaultNode     sql.NullString
		sessionAvailableJSON   string
		sessionExpiresAt       string
		sessionLastSeen        sql.NullString
		sessionRevoked         sql.NullString
		sessionClientIP        sql.NullString
		sessionUserAgent       sql.NullString
		sessionCreatedAt       string
		sessionUpdatedAt       string
		linkID                 sql.NullString
		linkTokenHash          sql.NullString
		linkLabel              sql.NullString
		linkSource             sql.NullString
		linkStatus             sql.NullString
		linkAllowedJSON        sql.NullString
		linkDefaultNode        sql.NullString
		linkExpiresAt          sql.NullString
		linkLastEx             sql.NullString
		linkRevoked            sql.NullString
		linkCreatedAt          sql.NullString
		linkUpdatedAt          sql.NullString
		nodeSupportsPAC, isDef int
	)

	err := row.Scan(
		&bundle.Credential.ID,
		&bundle.Credential.SessionID,
		&bundle.Credential.NodeID,
		&bundle.Credential.Username,
		&bundle.Credential.PasswordVersion,
		&credExpiresAt,
		&credLastUsed,
		&credRevoked,
		&credCreatedAt,
		&credUpdatedAt,
		&bundle.Session.ID,
		&sessionAccessLinkID,
		&bundle.Session.SourceType,
		&sessionSourceRef,
		&sessionExternalID,
		&bundle.Session.SessionTokenHash,
		&sessionSelectedNode,
		&sessionDefaultNode,
		&sessionAvailableJSON,
		&bundle.Session.Status,
		&sessionExpiresAt,
		&sessionLastSeen,
		&sessionRevoked,
		&sessionClientIP,
		&sessionUserAgent,
		&sessionCreatedAt,
		&sessionUpdatedAt,
		&linkID,
		&linkTokenHash,
		&linkLabel,
		&linkSource,
		&linkStatus,
		&linkAllowedJSON,
		&linkDefaultNode,
		&linkExpiresAt,
		&linkLastEx,
		&linkRevoked,
		&linkCreatedAt,
		&linkUpdatedAt,
		&bundle.Node.ID,
		&bundle.Node.Name,
		&bundle.Node.Country,
		&bundle.Node.City,
		&bundle.Node.Host,
		&bundle.Node.ProxyPort,
		&bundle.Node.ProxyScheme,
		&nodeSupportsPAC,
		&bundle.Node.Status,
		&bundle.Node.LatencyMS,
		&isDef,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}

	availableNodeIDs, err := unmarshalStringSlice(sessionAvailableJSON)
	if err != nil {
		return nil, err
	}

	bundle.Credential.ExpiresAt = mustParseTime(credExpiresAt)
	bundle.Credential.LastUsedAt = parseNullTime(credLastUsed)
	bundle.Credential.RevokedAt = parseNullTime(credRevoked)
	bundle.Credential.CreatedAt = mustParseTime(credCreatedAt)
	bundle.Credential.UpdatedAt = mustParseTime(credUpdatedAt)
	bundle.Session.AccessLinkID = sessionAccessLinkID.String
	bundle.Session.SourceRef = sessionSourceRef.String
	bundle.Session.ExternalSubscriptionID = sessionExternalID.String
	bundle.Session.SelectedNodeID = sessionSelectedNode.String
	bundle.Session.DefaultNodeID = sessionDefaultNode.String
	bundle.Session.AvailableNodeIDs = availableNodeIDs
	bundle.Session.ExpiresAt = mustParseTime(sessionExpiresAt)
	bundle.Session.LastSeenAt = parseNullTime(sessionLastSeen)
	bundle.Session.RevokedAt = parseNullTime(sessionRevoked)
	bundle.Session.ClientIP = sessionClientIP.String
	bundle.Session.UserAgent = sessionUserAgent.String
	bundle.Session.CreatedAt = mustParseTime(sessionCreatedAt)
	bundle.Session.UpdatedAt = mustParseTime(sessionUpdatedAt)
	if linkID.Valid {
		allowedNodeIDs, err := unmarshalStringSlice(linkAllowedJSON.String)
		if err != nil {
			return nil, err
		}
		accessLink := &AccessLink{
			ID:              linkID.String,
			TokenHash:       linkTokenHash.String,
			Label:           linkLabel.String,
			Source:          linkSource.String,
			Status:          linkStatus.String,
			AllowedNodeIDs:  allowedNodeIDs,
			DefaultNodeID:   linkDefaultNode.String,
			ExpiresAt:       mustParseTime(linkExpiresAt.String),
			LastExchangedAt: parseNullTime(linkLastEx),
			RevokedAt:       parseNullTime(linkRevoked),
			CreatedAt:       mustParseTime(linkCreatedAt.String),
			UpdatedAt:       mustParseTime(linkUpdatedAt.String),
		}
		bundle.AccessLink = accessLink
	}
	bundle.Node.SupportsPAC = nodeSupportsPAC == 1
	bundle.Node.IsDefault = isDef == 1

	return &bundle, nil
}

func (r *Repository) TouchProxyCredentialUsed(ctx context.Context, id string, when time.Time) error {
	_, err := r.db.ExecContext(
		ctx,
		`UPDATE proxy_credentials SET last_used_at = ?, updated_at = ? WHERE id = ?`,
		timeString(when),
		timeString(when),
		id,
	)
	return err
}

func scanCredential(row *sql.Row) (*ProxyCredential, error) {
	var (
		credential ProxyCredential
		expiresAt  string
		lastUsed   sql.NullString
		revoked    sql.NullString
		createdAt  string
		updatedAt  string
	)
	if err := row.Scan(
		&credential.ID,
		&credential.SessionID,
		&credential.NodeID,
		&credential.Username,
		&credential.PasswordVersion,
		&expiresAt,
		&lastUsed,
		&revoked,
		&createdAt,
		&updatedAt,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	credential.ExpiresAt = mustParseTime(expiresAt)
	credential.LastUsedAt = parseNullTime(lastUsed)
	credential.RevokedAt = parseNullTime(revoked)
	credential.CreatedAt = mustParseTime(createdAt)
	credential.UpdatedAt = mustParseTime(updatedAt)
	return &credential, nil
}

func timeString(value time.Time) string {
	return value.UTC().Format(time.RFC3339Nano)
}

func parseNullTime(value sql.NullString) *time.Time {
	if !value.Valid || strings.TrimSpace(value.String) == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func mustParseTime(value string) time.Time {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}
	}
	return parsed
}

func marshalStringSlice(values []string) (string, error) {
	if values == nil {
		values = []string{}
	}
	data, err := json.Marshal(values)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func unmarshalStringSlice(raw string) ([]string, error) {
	if strings.TrimSpace(raw) == "" {
		return []string{}, nil
	}
	var values []string
	if err := json.Unmarshal([]byte(raw), &values); err != nil {
		return nil, err
	}
	return values, nil
}

func boolToInt(value bool) int {
	if value {
		return 1
	}
	return 0
}

func nullString(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}
