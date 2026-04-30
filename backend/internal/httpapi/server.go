package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/config"
	"github.com/skalover32-a11y/vlf-chrome-proxy/backend/internal/service"
)

type Server struct {
	service *service.Service
	logger  *slog.Logger
	config  config.Config
}

func New(service *service.Service, logger *slog.Logger, cfg config.Config) *Server {
	return &Server{service: service, logger: logger, config: cfg}
}

func (s *Server) Handler() http.Handler {
	router := chi.NewRouter()
	router.Use(s.requestLogger)
	router.Use(s.cors)

	router.Get("/healthz", s.handleHealthz)

	router.Route("/browser", func(r chi.Router) {
		r.Post("/exchange-link", s.handleExchangeLink)
		r.With(s.requireBearerToken).Get("/session", s.handleSession)
		r.With(s.requireBearerToken).Get("/proxy-config", s.handleProxyConfig)
		r.With(s.requireBearerToken).Post("/logout", s.handleLogout)
		r.With(s.requireBearerToken).Get("/ip", s.handleIPStub)
		r.With(s.requireBearerToken).Get("/pac-config", s.handlePACConfig)
	})

	return router
}

func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleExchangeLink(w http.ResponseWriter, r *http.Request) {
	var payload struct {
		URL string `json:"url"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		writeError(w, &service.AppError{
			Code:    "invalid_json",
			Message: "Request body is invalid.",
			Status:  http.StatusBadRequest,
		})
		return
	}

	response, err := s.service.ExchangeLink(r.Context(), service.ExchangeLinkRequest{
		URL:       payload.URL,
		ClientIP:  clientIP(r),
		UserAgent: r.UserAgent(),
	})
	if err != nil {
		writeServiceError(w, err)
		return
	}

	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleSession(w http.ResponseWriter, r *http.Request) {
	response, _, err := s.service.ValidateSession(r.Context(), bearerTokenFromContext(r.Context()))
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleProxyConfig(w http.ResponseWriter, r *http.Request) {
	response, err := s.service.GetProxyConfig(
		r.Context(),
		bearerTokenFromContext(r.Context()),
		strings.TrimSpace(r.URL.Query().Get("node_id")),
		strings.TrimSpace(r.URL.Query().Get("mode")),
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	if err := s.service.Logout(r.Context(), bearerTokenFromContext(r.Context())); err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (s *Server) handleIPStub(w http.ResponseWriter, _ *http.Request) {
	writeError(w, &service.AppError{
		Code:    "not_implemented",
		Message: "/browser/ip is not implemented in this MVP.",
		Status:  http.StatusNotImplemented,
	})
}

func (s *Server) handlePACConfig(w http.ResponseWriter, r *http.Request) {
	response, err := s.service.GetPacConfig(
		r.Context(),
		bearerTokenFromContext(r.Context()),
		strings.TrimSpace(r.URL.Query().Get("node_id")),
		queryCSV(r.URL.Query().Get("bypass")),
	)
	if err != nil {
		writeServiceError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

func (s *Server) requireBearerToken(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		header := strings.TrimSpace(r.Header.Get("Authorization"))
		if !strings.HasPrefix(strings.ToLower(header), "bearer ") {
			writeError(w, &service.AppError{
				Code:    "session_missing",
				Message: "Bearer token is required.",
				Status:  http.StatusUnauthorized,
			})
			return
		}
		token := strings.TrimSpace(header[len("Bearer "):])
		ctx := context.WithValue(r.Context(), bearerTokenContextKey{}, token)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		s.logger.Info("http request", slog.String("method", r.Method), slog.String("path", r.URL.Path), slog.String("remote_addr", clientIP(r)), slog.Duration("duration", time.Since(start)))
	})
}

func (s *Server) cors(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := strings.TrimSpace(r.Header.Get("Origin"))
		if origin != "" && s.isOriginAllowed(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
			w.Header().Set("Access-Control-Allow-Headers", "Authorization, Content-Type")
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
		}
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) isOriginAllowed(origin string) bool {
	origin = strings.TrimSpace(origin)
	if origin == "" {
		return false
	}

	for _, allowed := range s.config.CORSAllowedOrigins {
		if origin == allowed {
			return true
		}
	}

	if s.config.CORSAllowChromeExtensionOrigins && strings.HasPrefix(origin, "chrome-extension://") {
		if len(s.config.AllowedChromeExtensionIDs) == 0 {
			return true
		}
		for _, extensionID := range s.config.AllowedChromeExtensionIDs {
			if origin == "chrome-extension://"+extensionID {
				return true
			}
		}
	}

	return false
}

type bearerTokenContextKey struct{}

func bearerTokenFromContext(ctx context.Context) string {
	value, _ := ctx.Value(bearerTokenContextKey{}).(string)
	return value
}

func clientIP(r *http.Request) string {
	if forwarded := strings.TrimSpace(r.Header.Get("X-Forwarded-For")); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(strings.TrimSpace(r.RemoteAddr))
	if err != nil {
		return strings.TrimSpace(r.RemoteAddr)
	}
	return host
}

func queryCSV(value string) []string {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			items = append(items, part)
		}
	}
	return items
}

func writeServiceError(w http.ResponseWriter, err error) {
	var appErr *service.AppError
	if errors.As(err, &appErr) {
		writeError(w, appErr)
		return
	}
	writeError(w, &service.AppError{
		Code:    "internal_error",
		Message: "Internal server error.",
		Status:  http.StatusInternalServerError,
	})
}

func writeError(w http.ResponseWriter, err *service.AppError) {
	writeJSON(w, err.Status, map[string]any{
		"error": map[string]any{
			"code":    err.Code,
			"message": err.Message,
		},
	})
}

func writeJSON(w http.ResponseWriter, status int, payload any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(payload)
}
