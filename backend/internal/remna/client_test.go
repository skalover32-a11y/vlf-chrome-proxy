package remna

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSubscriptionByShortUUIDFallbackInfoEndpoint(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/subscriptions/by-short-uuid/short-1":
			w.WriteHeader(http.StatusUnauthorized)
		case "/api/sub/short-1/info":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"response":{"isFound":true,"user":{"shortUuid":"short-1","username":"user-1","expiresAt":"2027-01-01T00:00:00.000Z","isActive":true,"userStatus":"ACTIVE"},"subscriptionUrl":"https://sub.example.com/short-1"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	client := NewClient(server.URL, "token", server.Client())
	subscription, err := client.GetSubscriptionByShortUUID(context.Background(), "short-1")
	if err != nil {
		t.Fatalf("expected subscription, got error: %v", err)
	}
	if subscription.ShortUUID != "short-1" || !subscription.IsActive || subscription.Status != "ACTIVE" {
		t.Fatalf("unexpected subscription: %+v", subscription)
	}
}

func TestGetSubscriptionByShortUUIDNotFound(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"response":{"isFound":false}}`))
	}))
	defer server.Close()

	client := NewClient(server.URL, "", server.Client())
	_, err := client.GetSubscriptionByShortUUID(context.Background(), "missing")
	if err != ErrNotFound {
		t.Fatalf("expected ErrNotFound, got %v", err)
	}
}
