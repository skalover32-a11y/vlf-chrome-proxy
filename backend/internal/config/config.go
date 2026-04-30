package config

import (
	"crypto/tls"
	"errors"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/joho/godotenv"
)

type Config struct {
	AppEnv                          string
	LogLevel                        string
	HTTPListenAddr                  string
	HTTPSProxyListenAddr            string
	HTTPSProxyTLSCertPath           string
	HTTPSProxyTLSKeyPath            string
	SQLitePath                      string
	NodeConfigPath                  string
	TokenPepper                     string
	ProxyPasswordPepper             string
	SessionTTL                      time.Duration
	ProxyCredentialTTL              time.Duration
	DefaultBypassList               []string
	CORSAllowedOrigins              []string
	CORSAllowChromeExtensionOrigins bool
	AllowedChromeExtensionIDs       []string
	AccessLinkBaseURL               string
	AccessSourceMode                string
	RemnaAPIBaseURL                 string
	RemnaAPIToken                   string
	RemnaTimeout                    time.Duration
	RemnaAllowInsecureTLS           bool
	ProxyAllowPrivateDestinations   bool
	ProxyEnableIPv6                 bool
}

func Load() (Config, error) {
	_ = godotenv.Load()

	cfg := Config{
		AppEnv:                env("APP_ENV", "production"),
		LogLevel:              env("LOG_LEVEL", "info"),
		HTTPListenAddr:        env("HTTP_LISTEN_ADDR", ":8080"),
		HTTPSProxyListenAddr:  env("HTTPS_PROXY_LISTEN_ADDR", ":443"),
		HTTPSProxyTLSCertPath: env("HTTPS_PROXY_TLS_CERT_PATH", "/runtime/tls/proxy.crt"),
		HTTPSProxyTLSKeyPath:  env("HTTPS_PROXY_TLS_KEY_PATH", "/runtime/tls/proxy.key"),
		SQLitePath:            env("SQLITE_PATH", "/data/app.db"),
		NodeConfigPath:        env("NODE_CONFIG_PATH", "/runtime/nodes.json"),
		TokenPepper:           strings.TrimSpace(os.Getenv("TOKEN_PEPPER")),
		ProxyPasswordPepper: strings.TrimSpace(
			os.Getenv("PROXY_PASSWORD_PEPPER"),
		),
		SessionTTL:                      durationEnv("SESSION_TTL_HOURS", 24*time.Hour),
		ProxyCredentialTTL:              durationEnv("PROXY_CREDENTIAL_TTL_HOURS", 24*time.Hour),
		DefaultBypassList:               csv(env("DEFAULT_BYPASS_LIST", "<local>,127.0.0.1")),
		CORSAllowedOrigins:              csv(os.Getenv("CORS_ALLOWED_ORIGINS")),
		CORSAllowChromeExtensionOrigins: boolEnv("CORS_ALLOW_CHROME_EXTENSION_ORIGINS", true),
		AllowedChromeExtensionIDs:       csv(os.Getenv("ALLOWED_CHROME_EXTENSION_IDS")),
		AccessLinkBaseURL:               strings.TrimRight(env("ACCESS_LINK_BASE_URL", "https://example.com"), "/"),
		AccessSourceMode:                env("ACCESS_SOURCE_MODE", "local_only"),
		RemnaAPIBaseURL:                 strings.TrimRight(env("REMNA_API_BASE_URL", ""), "/"),
		RemnaAPIToken:                   strings.TrimSpace(os.Getenv("REMNA_API_TOKEN")),
		RemnaTimeout:                    secondsDurationEnv("REMNA_TIMEOUT_SECONDS", 10*time.Second),
		RemnaAllowInsecureTLS:           boolEnv("REMNA_ALLOW_INSECURE_TLS", false),
		ProxyAllowPrivateDestinations:   boolEnv("PROXY_ALLOW_PRIVATE_DESTINATIONS", false),
		ProxyEnableIPv6:                 boolEnv("PROXY_ENABLE_IPV6", false),
	}

	if cfg.TokenPepper == "" {
		return Config{}, errors.New("TOKEN_PEPPER is required")
	}
	if cfg.ProxyPasswordPepper == "" {
		return Config{}, errors.New("PROXY_PASSWORD_PEPPER is required")
	}
	if !validAccessSourceMode(cfg.AccessSourceMode) {
		return Config{}, errors.New("ACCESS_SOURCE_MODE must be remna_only, remna_or_local, or local_only")
	}
	if modeUsesRemna(cfg.AccessSourceMode) && cfg.RemnaAPIBaseURL == "" {
		return Config{}, errors.New("REMNA_API_BASE_URL is required when ACCESS_SOURCE_MODE uses remna")
	}

	return cfg, nil
}

func (c Config) RemnaHTTPClient() *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if c.RemnaAllowInsecureTLS {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // Explicit dev-only switch.
	}
	return &http.Client{
		Timeout:   c.RemnaTimeout,
		Transport: transport,
	}
}

func env(name, fallback string) string {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	return value
}

func boolEnv(name string, fallback bool) bool {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	parsed, err := strconv.ParseBool(value)
	if err != nil {
		return fallback
	}
	return parsed
}

func durationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}

	if strings.ContainsAny(value, "smhd") {
		parsed, err := time.ParseDuration(value)
		if err == nil {
			return parsed
		}
	}

	hours, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(hours) * time.Hour
}

func secondsDurationEnv(name string, fallback time.Duration) time.Duration {
	value := strings.TrimSpace(os.Getenv(name))
	if value == "" {
		return fallback
	}
	if strings.ContainsAny(value, "smhd") {
		parsed, err := time.ParseDuration(value)
		if err == nil {
			return parsed
		}
	}
	seconds, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return time.Duration(seconds) * time.Second
}

func validAccessSourceMode(mode string) bool {
	return mode == "remna_only" || mode == "remna_or_local" || mode == "local_only"
}

func modeUsesRemna(mode string) bool {
	return mode == "remna_only" || mode == "remna_or_local"
}

func csv(value string) []string {
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
