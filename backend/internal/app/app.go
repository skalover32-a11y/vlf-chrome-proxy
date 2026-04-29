package app

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"strings"

	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/config"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/logging"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/repository"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/service"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/sqliteutil"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/tokens"
)

type Runtime struct {
	Config  config.Config
	Logger  *slog.Logger
	DB      *sql.DB
	Repo    *repository.Repository
	Service *service.Service
}

func New(ctx context.Context) (*Runtime, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, err
	}

	logger := logging.New(cfg.LogLevel)
	db, err := sqliteutil.Open(cfg.SQLitePath)
	if err != nil {
		return nil, err
	}

	if err := sqliteutil.Migrate(ctx, db); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("migrate sqlite: %w", err)
	}

	repo := repository.New(db)
	if err := syncNodesFromFile(ctx, repo, cfg.NodeConfigPath); err != nil {
		_ = db.Close()
		return nil, err
	}

	tokenManager := tokens.NewManager(cfg.TokenPepper, cfg.ProxyPasswordPepper)
	svc := service.New(
		repo,
		logger,
		tokenManager,
		cfg.SessionTTL,
		cfg.ProxyCredentialTTL,
		cfg.DefaultBypassList,
		cfg.AccessLinkBaseURL,
		cfg.ProxyAllowPrivateDestinations,
	)

	return &Runtime{
		Config:  cfg,
		Logger:  logger,
		DB:      db,
		Repo:    repo,
		Service: svc,
	}, nil
}

func (r *Runtime) Close() error {
	if r.DB != nil {
		return r.DB.Close()
	}
	return nil
}

type nodeFile struct {
	Nodes []repository.Node `json:"nodes"`
}

func syncNodesFromFile(ctx context.Context, repo *repository.Repository, path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read node config %s: %w", path, err)
	}

	var cfg nodeFile
	if err := json.Unmarshal(data, &cfg); err != nil {
		return fmt.Errorf("parse node config: %w", err)
	}
	if len(cfg.Nodes) == 0 {
		return fmt.Errorf("node config %s does not contain any nodes", path)
	}

	for idx := range cfg.Nodes {
		cfg.Nodes[idx].ProxyScheme = strings.TrimSpace(cfg.Nodes[idx].ProxyScheme)
		if cfg.Nodes[idx].ProxyScheme == "" {
			cfg.Nodes[idx].ProxyScheme = "https"
		}
	}
	return repo.UpsertNodes(ctx, cfg.Nodes)
}
