package tokens

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

type Manager struct {
	tokenPepper []byte
	proxyPepper []byte
}

func NewManager(tokenPepper string, proxyPepper string) *Manager {
	return &Manager{
		tokenPepper: []byte(tokenPepper),
		proxyPepper: []byte(proxyPepper),
	}
}

func (m *Manager) NewAccessToken() (raw string, hash string, err error) {
	return m.newScopedToken("atk", "access")
}

func (m *Manager) NewSessionToken() (raw string, hash string, err error) {
	return m.newScopedToken("sess", "session")
}

func (m *Manager) HashAccessToken(raw string) string {
	return m.hash("access", raw)
}

func (m *Manager) HashSessionToken(raw string) string {
	return m.hash("session", raw)
}

func (m *Manager) NewProxyUsername() (string, error) {
	buf := make([]byte, 12)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return "browser_u_" + base64.RawURLEncoding.EncodeToString(buf), nil
}

func (m *Manager) DeriveProxyPassword(sessionID, nodeID string, version int) string {
	scope := fmt.Sprintf("proxy:%s:%s:%d", sessionID, nodeID, version)
	sum := hmac.New(sha256.New, m.proxyPepper)
	_, _ = sum.Write([]byte(scope))
	encoded := base64.RawURLEncoding.EncodeToString(sum.Sum(nil))
	if len(encoded) > 32 {
		encoded = encoded[:32]
	}
	return "browser_p_" + encoded
}

func (m *Manager) hash(scope, raw string) string {
	sum := hmac.New(sha256.New, m.tokenPepper)
	_, _ = sum.Write([]byte(scope))
	_, _ = sum.Write([]byte{':'})
	_, _ = sum.Write([]byte(raw))
	return hex.EncodeToString(sum.Sum(nil))
}

func (m *Manager) newScopedToken(prefix, scope string) (string, string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", "", err
	}
	raw := prefix + "_" + base64.RawURLEncoding.EncodeToString(buf)
	return raw, m.hash(scope, raw), nil
}
