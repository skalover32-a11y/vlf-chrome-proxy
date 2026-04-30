package remna

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Client struct {
	baseURL    string
	apiToken   string
	httpClient *http.Client
}

type Subscription struct {
	ShortUUID       string
	Username        string
	Status          string
	IsActive        bool
	ExpiresAt       *time.Time
	SubscriptionURL string
}

var (
	ErrNotFound     = errors.New("remna subscription not found")
	ErrUnauthorized = errors.New("remna api unauthorized")
	ErrUnavailable  = errors.New("remna api unavailable")
)

func NewClient(baseURL string, apiToken string, httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: 10 * time.Second}
	}
	return &Client{
		baseURL:    strings.TrimRight(strings.TrimSpace(baseURL), "/"),
		apiToken:   strings.TrimSpace(apiToken),
		httpClient: httpClient,
	}
}

func (c *Client) GetSubscriptionByShortUUID(ctx context.Context, shortUUID string) (*Subscription, error) {
	shortUUID = strings.TrimSpace(shortUUID)
	if shortUUID == "" {
		return nil, ErrNotFound
	}

	if c.apiToken != "" {
		subscription, err := c.getSubscription(ctx, "/api/subscriptions/by-short-uuid/"+url.PathEscape(shortUUID), true)
		if err == nil {
			return subscription, nil
		}
		if !errors.Is(err, ErrNotFound) && !errors.Is(err, ErrUnauthorized) {
			return nil, err
		}
	}

	return c.getSubscription(ctx, "/api/sub/"+url.PathEscape(shortUUID)+"/info", c.apiToken != "")
}

func (c *Client) getSubscription(ctx context.Context, path string, withAuth bool) (*Subscription, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+path, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json")
	if withAuth && c.apiToken != "" {
		request.Header.Set("Authorization", "Bearer "+c.apiToken)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrUnavailable, err)
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusNotFound:
		return nil, ErrNotFound
	case http.StatusUnauthorized, http.StatusForbidden:
		return nil, ErrUnauthorized
	default:
		if response.StatusCode >= 500 {
			return nil, ErrUnavailable
		}
		return nil, fmt.Errorf("remna api unexpected status: %d", response.StatusCode)
	}

	var payload subscriptionEnvelope
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return nil, fmt.Errorf("decode remna subscription response: %w", err)
	}

	subscription := payload.subscription()
	if subscription == nil || !subscription.found() {
		return nil, ErrNotFound
	}

	return subscription.toDomain(), nil
}

type subscriptionEnvelope struct {
	Response *subscriptionPayload `json:"response"`
	IsFound  *bool                `json:"isFound"`
	User     *subscriptionUser    `json:"user"`
	URL      string               `json:"subscriptionUrl"`
}

func (e subscriptionEnvelope) subscription() *subscriptionPayload {
	if e.Response != nil {
		return e.Response
	}
	if e.IsFound != nil || e.User != nil {
		return &subscriptionPayload{
			IsFound:         e.IsFound,
			User:            e.User,
			SubscriptionURL: e.URL,
		}
	}
	return nil
}

type subscriptionPayload struct {
	IsFound         *bool             `json:"isFound"`
	User            *subscriptionUser `json:"user"`
	SubscriptionURL string            `json:"subscriptionUrl"`
}

func (p subscriptionPayload) found() bool {
	if p.IsFound != nil {
		return *p.IsFound
	}
	return p.User != nil
}

func (p subscriptionPayload) toDomain() *Subscription {
	if p.User == nil {
		return &Subscription{IsActive: false, SubscriptionURL: p.SubscriptionURL}
	}

	return &Subscription{
		ShortUUID:       strings.TrimSpace(p.User.ShortUUID),
		Username:        strings.TrimSpace(p.User.Username),
		Status:          strings.TrimSpace(p.User.UserStatus),
		IsActive:        p.User.IsActive,
		ExpiresAt:       parseRemnaTime(p.User.ExpiresAt),
		SubscriptionURL: strings.TrimSpace(p.SubscriptionURL),
	}
}

type subscriptionUser struct {
	ShortUUID  string `json:"shortUuid"`
	Username   string `json:"username"`
	ExpiresAt  string `json:"expiresAt"`
	IsActive   bool   `json:"isActive"`
	UserStatus string `json:"userStatus"`
}

func parseRemnaTime(value string) *time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil
	}
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return nil
	}
	return &parsed
}
